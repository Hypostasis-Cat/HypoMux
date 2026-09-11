package dns

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type dohTestDial struct {
	binding          Binding
	network, address string
}

type dohFixture struct {
	resolver *Resolver
	endpoint Endpoint
	server   *httptest.Server
	root     context.Context
	cancel   context.CancelFunc
	mu       sync.Mutex
	dials    []dohTestDial
	closed   atomic.Int64
}

type dohTrackedConn struct {
	net.Conn
	once   sync.Once
	closed *atomic.Int64
}

func (c *dohTrackedConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(func() { c.closed.Add(1) })
	return err
}

func newDoHFixture(t *testing.T, http2 bool, handler http.HandlerFunc) *dohFixture {
	t.Helper()
	f := &dohFixture{endpoint: Endpoint{IP: "192.0.2.53", Host: "example.com", Path: "/dns-query"}}
	f.server = httptest.NewUnstartedServer(handler)
	f.server.EnableHTTP2 = http2
	f.server.StartTLS()
	t.Cleanup(f.server.Close)
	f.root, f.cancel = context.WithCancel(context.Background())
	t.Cleanup(f.cancel)
	var err error
	f.resolver, err = New(f.root, Config{Policy: PolicyAliDNS, QueryTimeout: time.Second}, func(ctx context.Context, network, address string, binding Binding) (net.Conn, error) {
		f.mu.Lock()
		f.dials = append(f.dials, dohTestDial{binding: binding, network: network, address: address})
		f.mu.Unlock()
		// A local test server stands in for the explicitly supplied public IP.
		connection, err := (&net.Dialer{}).DialContext(ctx, "tcp4", f.server.Listener.Addr().String())
		if err != nil {
			return nil, err
		}
		return &dohTrackedConn{Conn: connection, closed: &f.closed}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	f.trust(t, loopbackBinding, f.endpoint)
	return f
}

func (f *dohFixture) trust(t *testing.T, binding Binding, endpoint Endpoint) {
	t.Helper()
	transport, err := f.resolver.doHTransport(binding, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(f.server.Certificate())
	transport.TLSClientConfig.RootCAs = roots
}

func (f *dohFixture) dialCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.dials)
}

func serveDoHAnswer(t *testing.T, w http.ResponseWriter, req *http.Request) {
	t.Helper()
	if req.Method != http.MethodPost || req.Header.Get("Content-Type") != "application/dns-message" || req.Header.Get("Accept") != "application/dns-message" {
		t.Errorf("unexpected DoH request: %s %#v", req.Method, req.Header)
	}
	packet, err := io.ReadAll(req.Body)
	if err != nil {
		t.Error(err)
		return
	}
	answer, err := answerForQueryRaw(packet, dnsTypeA, "192.0.2.44", 60)
	if err != nil {
		t.Error(err)
		return
	}
	w.Header().Set("Content-Type", "application/dns-message")
	w.Write(answer)
}

func TestDoHReusesHTTPConnections(t *testing.T) {
	for _, h2 := range []bool{false, true} {
		t.Run(fmt.Sprintf("http2=%v", h2), func(t *testing.T) {
			var requests atomic.Int64
			f := newDoHFixture(t, h2, func(w http.ResponseWriter, req *http.Request) {
				requests.Add(1)
				if req.Host != "example.com" || req.TLS.ServerName != "example.com" || (req.ProtoMajor == 2) != h2 {
					t.Errorf("Host=%s SNI=%s protocol=%s", req.Host, req.TLS.ServerName, req.Proto)
				}
				serveDoHAnswer(t, w, req)
			})
			for i := 0; i < 5; i++ {
				ctx, cancel := context.WithTimeout(f.root, time.Second)
				result, ttl, err := f.resolver.queryDoH(ctx, fmt.Sprintf("d%d.example", i), dnsTypeA, loopbackBinding, f.endpoint)
				cancel() // A finished request must not poison a reusable socket.
				if err != nil || result.Address != "192.0.2.44" || ttl != time.Minute {
					t.Fatalf("result=%#v ttl=%s err=%v", result, ttl, err)
				}
			}
			if requests.Load() != 5 || f.dialCount() != 1 {
				t.Fatalf("requests=%d dials=%d", requests.Load(), f.dialCount())
			}
		})
	}
}

func TestDoHPoolsAreIsolatedByBindingAndEndpoint(t *testing.T) {
	f := newDoHFixture(t, true, func(w http.ResponseWriter, req *http.Request) { serveDoHAnswer(t, w, req) })
	otherName, otherIP, otherIndex := loopbackBinding, loopbackBinding, loopbackBinding
	otherName.Name = "other"
	otherIP.SourceIP = "127.0.0.2"
	otherIndex.IfIndex++
	otherEndpoint := f.endpoint
	otherEndpoint.IP = "192.0.2.54"
	otherPath := f.endpoint
	otherPath.Path = "/other-query"
	cases := []struct {
		binding  Binding
		endpoint Endpoint
	}{
		{loopbackBinding, f.endpoint}, {otherName, f.endpoint}, {otherIP, f.endpoint},
		{otherIndex, f.endpoint}, {loopbackBinding, otherEndpoint}, {loopbackBinding, otherPath},
	}
	for i, item := range cases {
		if i != 0 {
			f.trust(t, item.binding, item.endpoint)
		}
		for j := 0; j < 2; j++ {
			if _, _, err := f.resolver.queryDoH(f.root, "isolation.example", dnsTypeA, item.binding, item.endpoint); err != nil {
				t.Fatal(err)
			}
		}
	}
	if f.dialCount() != len(cases) {
		t.Fatalf("dials=%d, want %d", f.dialCount(), len(cases))
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, call := range f.dials {
		want := cases[i]
		if bindingKey(call.binding) != bindingKey(want.binding) || call.network != "tcp4" || call.address != net.JoinHostPort(want.endpoint.IP, "443") {
			t.Fatalf("dial %d lost explicit binding/endpoint: %#v", i, call)
		}
	}
}

func TestDoHHTTP2MultiplexingAndRequestCancellation(t *testing.T) {
	const concurrent = 5
	entered := make(chan struct{}, concurrent)
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseAll := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseAll()
	var requests atomic.Int64
	f := newDoHFixture(t, true, func(w http.ResponseWriter, req *http.Request) {
		if requests.Add(1) > 1 {
			entered <- struct{}{}
			select {
			case <-release:
			case <-req.Context().Done():
				return
			}
		}
		serveDoHAnswer(t, w, req)
	})
	if _, _, err := f.resolver.queryDoH(f.root, "warm.example", dnsTypeA, loopbackBinding, f.endpoint); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(f.root)
	defer cancel()
	results := make(chan error, concurrent)
	for i := 0; i < concurrent; i++ {
		requestCtx := f.root
		if i == 0 {
			requestCtx = ctx
		}
		go func(i int, requestCtx context.Context) {
			_, _, err := f.resolver.queryDoH(requestCtx, fmt.Sprintf("parallel%d.example", i), dnsTypeA, loopbackBinding, f.endpoint)
			results <- err
		}(i, requestCtx)
	}
	for i := 0; i < concurrent; i++ {
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			t.Fatal("HTTP/2 requests did not execute concurrently")
		}
	}
	cancel()
	select {
	case err := <-results:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled stream returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled stream did not stop")
	}
	releaseAll()
	for i := 1; i < concurrent; i++ {
		select {
		case err := <-results:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("other stream interrupted or stalled")
		}
	}
	if f.dialCount() != 1 {
		t.Fatalf("multiplexed queries used %d connections", f.dialCount())
	}
}

func TestDoHRejectsInvalidResponsesAndRedirects(t *testing.T) {
	for _, kind := range []string{"redirect", "status", "content-type", "oversized", "dns-id"} {
		t.Run(kind, func(t *testing.T) {
			var requests atomic.Int64
			f := newDoHFixture(t, true, func(w http.ResponseWriter, req *http.Request) {
				requests.Add(1)
				switch kind {
				case "redirect":
					w.Header().Set("Location", "https://unconfigured.example/dns-query")
					w.WriteHeader(http.StatusFound)
				case "status":
					w.WriteHeader(http.StatusServiceUnavailable)
				case "content-type":
					w.Header().Set("Content-Type", "text/html")
					w.Write([]byte("not DNS"))
				case "oversized":
					w.Header().Set("Content-Type", "application/dns-message")
					w.Write(make([]byte, maxDNSMessageBytes+1))
				case "dns-id":
					packet, _ := io.ReadAll(req.Body)
					answer, err := answerForQueryRaw(packet, dnsTypeA, "192.0.2.1", 60)
					if err != nil {
						t.Error(err)
						return
					}
					answer[0] ^= 0xff
					w.Header().Set("Content-Type", "application/dns-message")
					w.Write(answer)
				}
			})
			if _, _, err := f.resolver.queryDoH(f.root, "invalid.example", dnsTypeA, loopbackBinding, f.endpoint); err == nil {
				t.Fatal("invalid response accepted")
			}
			if requests.Load() != 1 || f.dialCount() != 1 {
				t.Fatal("unexpected redirect/retry")
			}
		})
	}
}

func TestDoHVerifiesCertificateAndHostname(t *testing.T) {
	for _, wrongHost := range []bool{false, true} {
		t.Run(fmt.Sprintf("wrongHost=%v", wrongHost), func(t *testing.T) {
			var requests atomic.Int64
			f := newDoHFixture(t, false, func(w http.ResponseWriter, req *http.Request) { requests.Add(1); serveDoHAnswer(t, w, req) })
			ep := f.endpoint
			if wrongHost {
				ep.Host = "wrong.example"
				f.trust(t, loopbackBinding, ep)
			} else {
				transport, _ := f.resolver.doHTransport(loopbackBinding, ep)
				transport.TLSClientConfig.RootCAs = x509.NewCertPool()
			}
			if _, _, err := f.resolver.queryDoH(f.root, "cert.example", dnsTypeA, loopbackBinding, ep); err == nil {
				t.Fatal("invalid certificate accepted")
			}
			if requests.Load() != 0 {
				t.Fatal("sent DNS request before certificate validation")
			}
		})
	}
}

func TestDoHPoolLimitAndRootCleanup(t *testing.T) {
	f := newDoHFixture(t, false, func(w http.ResponseWriter, req *http.Request) { serveDoHAnswer(t, w, req) })
	if _, _, err := f.resolver.queryDoH(f.root, "warm.example", dnsTypeA, loopbackBinding, f.endpoint); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxDoHTransports+1; i++ {
		binding := loopbackBinding
		binding.Name = fmt.Sprintf("adapter-%d", i)
		if _, err := f.resolver.doHTransport(binding, f.endpoint); err != nil {
			t.Fatal(err)
		}
	}
	f.resolver.dohMu.Lock()
	size := len(f.resolver.dohTransports)
	_, oldRetained := f.resolver.dohTransports[dohPoolKey{bindingKey(loopbackBinding), f.endpoint}]
	f.resolver.dohMu.Unlock()
	if size != maxDoHTransports || oldRetained {
		t.Fatalf("pool limit: size=%d, retained oldest=%v", size, oldRetained)
	}
	f.cancel()
	if _, err := f.resolver.doHTransport(loopbackBinding, f.endpoint); !errors.Is(err, context.Canceled) {
		t.Fatalf("pool accepted request after stop: %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		f.resolver.dohMu.Lock()
		closed := f.resolver.dohClosed && len(f.resolver.dohTransports) == 0
		f.resolver.dohMu.Unlock()
		if closed && f.closed.Load() == int64(f.dialCount()) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("root cancellation retained transport pools or sockets")
}

func TestDoHRootCancellationStopsTLSHandshake(t *testing.T) {
	root, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	closed := make(chan struct{})
	r, err := New(root, Config{Policy: PolicyAliDNS, QueryTimeout: 5 * time.Second}, func(ctx context.Context, _, _ string, _ Binding) (net.Conn, error) {
		client, server := net.Pipe()
		go func() {
			defer server.Close()
			defer close(closed)
			buffer := make([]byte, 4096)
			if _, err := server.Read(buffer); err == nil {
				close(started)
				io.Copy(io.Discard, server)
			}
		}()
		return client, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := r.Resolve(root, Query{Domain: "stall.example", Binding: loopbackBinding})
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("TLS handshake did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("lookup error=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("lookup did not cancel")
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("TLS socket retained after shutdown")
	}
}

func TestDoHResponseBodyTimeout(t *testing.T) {
	f := newDoHFixture(t, true, func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/dns-message")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-req.Context().Done()
	})
	ctx, cancel := context.WithTimeout(f.root, 100*time.Millisecond)
	defer cancel()
	_, _, err := f.resolver.queryDoH(ctx, "body.example", dnsTypeA, loopbackBinding, f.endpoint)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("body timeout error=%v", err)
	}
}

func TestDoHRootCancellationClosesIdleSocket(t *testing.T) {
	for _, h2 := range []bool{false, true} {
		t.Run(fmt.Sprintf("http2=%v", h2), func(t *testing.T) {
			f := newDoHFixture(t, h2, func(w http.ResponseWriter, req *http.Request) { serveDoHAnswer(t, w, req) })
			if _, _, err := f.resolver.queryDoH(f.root, "idle.example", dnsTypeA, loopbackBinding, f.endpoint); err != nil {
				t.Fatal(err)
			}
			if f.closed.Load() != 0 {
				t.Fatal("socket not retained for reuse")
			}
			f.cancel()
			deadline := time.Now().Add(time.Second)
			for time.Now().Before(deadline) {
				if f.closed.Load() == 1 {
					return
				}
				time.Sleep(time.Millisecond)
			}
			t.Fatal("idle socket survived root cancellation")
		})
	}
}

func TestDoHHTTP1ConcurrentQueries(t *testing.T) {
	const concurrent = 8
	entered := make(chan struct{}, concurrent)
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseAll := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseAll()
	f := newDoHFixture(t, false, func(w http.ResponseWriter, req *http.Request) {
		entered <- struct{}{}
		select {
		case <-release:
			serveDoHAnswer(t, w, req)
		case <-req.Context().Done():
		}
	})
	results := make(chan error, concurrent)
	for i := 0; i < concurrent; i++ {
		go func(i int) {
			_, _, err := f.resolver.queryDoH(f.root, fmt.Sprintf("http1-%d.example", i), dnsTypeA, loopbackBinding, f.endpoint)
			results <- err
		}(i)
	}
	for i := 0; i < concurrent; i++ {
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			t.Fatal("HTTP/1.1 cold queries were serialized")
		}
	}
	releaseAll()
	for i := 0; i < concurrent; i++ {
		select {
		case err := <-results:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("concurrent request stalled")
		}
	}
}
