package dns

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type dotFixture struct {
	resolver           *Resolver
	endpoint           Endpoint
	roots              *x509.CertPool
	cancel             context.CancelFunc
	mu                 sync.Mutex
	dials              []dohTestDial
	closed             atomic.Int64
	queries            atomic.Int64
	closeAfterResponse atomic.Bool
}

func newDoTFixture(t *testing.T, ipv6 bool, respond func([]byte) []byte) *dotFixture {
	t.Helper()
	certificateServer := httptest.NewTLSServer(http.NotFoundHandler())
	certificate := certificateServer.TLS.Certificates[0]
	roots := x509.NewCertPool()
	roots.AddCert(certificateServer.Certificate())
	certificateServer.Close()
	// Map either requested IP family to a local TLS peer. The resolver's
	// requested network, endpoint and source binding are asserted separately.
	network, local, ip := "tcp4", "127.0.0.1:0", "192.0.2.53"
	if ipv6 {
		ip = "2001:db8::53"
	}
	listener, err := tls.Listen(network, local, &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	f := &dotFixture{endpoint: Endpoint{IP: ip, Host: "example.com", Port: 8853}, roots: roots}
	root, cancel := context.WithCancel(context.Background())
	f.cancel = cancel
	t.Cleanup(cancel)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
				for {
					var length uint16
					if binary.Read(conn, binary.BigEndian, &length) != nil {
						return
					}
					packet := make([]byte, int(length))
					if _, err := io.ReadFull(conn, packet); err != nil || len(packet) < 12 {
						return
					}
					f.queries.Add(1)
					if conn.(*tls.Conn).ConnectionState().ServerName != "example.com" {
						t.Error("DoT lost TLS server name")
					}
					var answer []byte
					if respond != nil {
						answer = respond(packet)
						if answer == nil {
							return
						}
					} else {
						kind := binary.BigEndian.Uint16(packet[len(packet)-4 : len(packet)-2])
						address := "192.0.2.44"
						if kind == dnsTypeAAAA {
							address = "2001:db8::44"
						}
						var answerErr error
						answer, answerErr = answerForQueryRaw(packet, kind, address, 60)
						if answerErr != nil {
							t.Error(answerErr)
							return
						}
					}
					if err := binary.Write(conn, binary.BigEndian, uint16(len(answer))); err != nil {
						return
					}
					if _, err := conn.Write(answer); err != nil {
						return
					}
					if f.closeAfterResponse.Load() {
						return
					}
				}
			}()
		}
	}()
	f.resolver, err = New(root, Config{Policy: PolicyDoT, DoTServers: []string{"tls://example.com:8853"}, QueryTimeout: time.Second}, func(ctx context.Context, gotNetwork, address string, binding Binding) (net.Conn, error) {
		f.mu.Lock()
		f.dials = append(f.dials, dohTestDial{binding, gotNetwork, address})
		f.mu.Unlock()
		if strings.HasPrefix(gotNetwork, "udp") {
			client, server := net.Pipe()
			go func() {
				defer server.Close()
				packet := make([]byte, 4096)
				n, err := server.Read(packet)
				if err != nil || n < 12 {
					return
				}
				kind := binary.BigEndian.Uint16(packet[n-4 : n-2])
				answer, err := answerForQueryRaw(packet[:n], kind, f.endpoint.IP, 60)
				if err != nil {
					return
				}
				_, _ = server.Write(answer)
			}()
			return client, nil
		}
		connection, err := (&net.Dialer{}).DialContext(ctx, network, listener.Addr().String())
		if err != nil {
			return nil, err
		}
		return &dohTrackedConn{Conn: connection, closed: &f.closed}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *dotFixture) trust(t *testing.T, binding Binding, endpoint Endpoint) {
	t.Helper()
	pool, err := f.resolver.doTPool(binding, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	pool.tlsConfig.RootCAs = f.roots
}

func TestDoTConfiguration(t *testing.T) {
	config, err := NormalizeConfig(Config{Policy: PolicyDoT, DoTServers: []string{" tls://DNS.Example.com ", "tls://dns.example.com:853", "tls://[2001:db8::53]:8853"}})
	if err != nil {
		t.Fatal(err)
	}
	endpoints := ConfigDoTEndpoints(config)
	if len(endpoints) != 2 || endpoints[0].Host != "dns.example.com" || endpoints[0].Port != 853 || endpoints[1].IP != "2001:db8::53" || endpoints[1].Port != 8853 || len(ConfigEndpoints(config)) != 0 {
		t.Fatalf("invalid normalized DoT configuration: %+v", config)
	}
	for _, value := range []string{"", "https://dns.example", "tls://dns.example/", "tls://dns.example?", "tls://dns.example#", "tls://user@dns.example", "tls://dns.example:0", "tls://dns.example:65536", "tls://dns.example:", "tls://0.0.0.0", "tls://[fe80::1]", "tls://127.1", "tls://☃.example"} {
		if _, err := NormalizeConfig(Config{Policy: PolicyDoT, DoTServers: []string{value}}); err == nil {
			t.Errorf("accepted %q", value)
		}
	}
	if _, err := NormalizeConfig(Config{Policy: PolicyDoT}); err == nil {
		t.Fatal("accepted an empty DoT policy")
	}
}

func TestDoTBootstrapsReusesConnectionsAndIsolatesBindings(t *testing.T) {
	for _, ipv6 := range []bool{false, true} {
		t.Run(fmt.Sprint(ipv6), func(t *testing.T) {
			f := newDoTFixture(t, ipv6, nil)
			binding, kind, network := loopbackBinding, RecordA, "tcp4"
			if ipv6 {
				binding.SourceIP = ""
				binding.SourceIPv6 = "::1"
				binding.IPv6IfIndex = 2
				kind = RecordAAAA
				network = "tcp6"
			}
			f.trust(t, binding, f.endpoint)
			for _, domain := range []string{"first.example", "second.example"} {
				answer, err := f.resolver.Resolve(context.Background(), Query{Domain: domain, Binding: binding, RecordType: kind})
				if err != nil || answer.Transport != "dot" || answer.Server != "example.com@"+net.JoinHostPort(f.endpoint.IP, "8853") {
					t.Fatalf("result=%+v err=%v", answer, err)
				}
			}
			cached, err := f.resolver.Resolve(context.Background(), Query{Domain: "first.example", Binding: binding, RecordType: kind})
			if err != nil || !cached.Cached || f.queries.Load() != 2 {
				t.Fatalf("cache=%+v err=%v queries=%d", cached, err, f.queries.Load())
			}
			other := binding
			other.Name = "other-adapter"
			other.IfIndex++
			other.IPv6IfIndex++
			f.trust(t, other, f.endpoint)
			answer, err := f.resolver.Resolve(context.Background(), Query{Domain: "first.example", Binding: other, RecordType: kind})
			if err != nil || answer.Cached {
				t.Fatalf("binding cache leaked: %+v %v", answer, err)
			}
			f.mu.Lock()
			calls := append([]dohTestDial(nil), f.dials...)
			f.mu.Unlock()
			var encrypted, bootstrap int
			for _, call := range calls {
				if strings.HasPrefix(call.network, "udp") {
					bootstrap++
				} else {
					encrypted++
					if call.network != network || call.address != net.JoinHostPort(f.endpoint.IP, "8853") {
						t.Fatalf("incorrect dial: %+v", call)
					}
				}
			}
			if encrypted != 2 || bootstrap != 2 {
				t.Fatalf("connections=%d bootstrap=%d; want one of each per binding", encrypted, bootstrap)
			}
			if f.resolver.Status().DoTSuccesses != 3 || len(f.resolver.Status().DoTEndpoints) != 1 {
				t.Fatal("missing DoT status")
			}
			f.cancel()
			deadline := time.Now().Add(time.Second)
			for f.closed.Load() < 2 && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			if f.closed.Load() != 2 {
				t.Fatal("idle connections leaked after shutdown")
			}
		})
	}
}

func TestDoTRejectsUntrustedAndWrongHostnameCertificates(t *testing.T) {
	for _, wrongHost := range []bool{false, true} {
		f := newDoTFixture(t, false, nil)
		endpoint := f.endpoint
		if wrongHost {
			endpoint.Host = "wrong.example"
			f.trust(t, loopbackBinding, endpoint)
		}
		_, _, err := f.resolver.queryDoT(f.resolver.root, "secure.example", dnsTypeA, loopbackBinding, endpoint)
		if err == nil {
			t.Fatal("accepted an invalid server certificate")
		}
		var hostnameError x509.HostnameError
		var authorityError x509.UnknownAuthorityError
		if wrongHost && !errors.As(err, &hostnameError) || !wrongHost && !errors.As(err, &authorityError) {
			t.Fatalf("expected certificate rejection, got %v", err)
		}
		if f.queries.Load() != 0 {
			t.Fatal("sent a DNS query before certificate verification")
		}
	}
}

func TestDoTFailureNeverDowngradesOrRequestsRestart(t *testing.T) {
	var plaintext, restart atomic.Int64
	r, err := New(context.Background(), Config{Policy: PolicyDoT, DoTServers: []string{"tls://192.0.2.53"}, FailureThreshold: 1}, func(ctx context.Context, network, address string, binding Binding) (net.Conn, error) {
		if strings.HasPrefix(network, "udp") || strings.HasSuffix(address, ":53") {
			plaintext.Add(1)
		}
		return nil, errors.New("DoT unavailable")
	})
	if err != nil {
		t.Fatal(err)
	}
	r.SetFallbackHandler(func(FallbackEvent) { restart.Add(1) })
	for i := 0; i < 3; i++ {
		if _, err := r.Resolve(context.Background(), Query{Domain: "private.example", Binding: loopbackBinding}); err == nil {
			t.Fatal("unexpected success")
		}
	}
	if plaintext.Load() != 0 || restart.Load() != 0 || r.Status().DoTFailures != 3 {
		t.Fatal("DoT failed open or lost failure telemetry")
	}
}

func TestDoTRejectsMalformedResponses(t *testing.T) {
	for _, mode := range []string{"length", "id", "truncated"} {
		t.Run(mode, func(t *testing.T) {
			f := newDoTFixture(t, false, func(packet []byte) []byte {
				if mode == "length" {
					return []byte{1, 2}
				}
				answer, err := answerForQueryRaw(packet, dnsTypeA, "192.0.2.44", 60)
				if err != nil {
					t.Error(err)
					return nil
				}
				if mode == "id" {
					answer[0] ^= 0xff
				} else {
					answer[2] |= 2
				}
				return answer
			})
			f.trust(t, loopbackBinding, f.endpoint)
			if _, _, err := f.resolver.queryDoT(f.resolver.root, "bad.example", dnsTypeA, loopbackBinding, f.endpoint); err == nil {
				t.Fatal("accepted malformed DoT response")
			}
		})
	}
}

func TestDoTCancellationClosesStalledRead(t *testing.T) {
	started := make(chan struct{})
	// Keep a verified TLS peer open after reading a query, but do not reply.
	block := make(chan struct{})
	defer close(block)
	f := newDoTFixture(t, false, func([]byte) []byte { close(started); <-block; return nil })
	f.trust(t, loopbackBinding, f.endpoint)
	ctx, cancel := context.WithCancel(f.resolver.root)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, _, err := f.resolver.queryDoT(ctx, "cancel.example", dnsTypeA, loopbackBinding, f.endpoint)
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("query never reached peer")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("canceled query succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation did not interrupt read")
	}
}

func TestDoTRetriesServerClosedIdleConnection(t *testing.T) {
	f := newDoTFixture(t, false, nil)
	f.closeAfterResponse.Store(true)
	f.trust(t, loopbackBinding, f.endpoint)
	for _, domain := range []string{"first.example", "second.example"} {
		ctx, cancel := context.WithTimeout(f.resolver.root, time.Second)
		_, _, err := f.resolver.queryDoT(ctx, domain, dnsTypeA, loopbackBinding, f.endpoint)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.dials) != 2 || f.queries.Load() != 2 {
		t.Fatalf("dials=%d queries=%d", len(f.dials), f.queries.Load())
	}
}

func TestDoTConcurrentQueriesRespectPoolLimitAndCancellation(t *testing.T) {
	started := make(chan struct{}, 4)
	block := make(chan struct{})
	defer close(block)
	f := newDoTFixture(t, false, func(packet []byte) []byte {
		started <- struct{}{}
		<-block
		answer, _ := answerForQueryRaw(packet, dnsTypeA, "192.0.2.44", 60)
		return answer
	})
	f.trust(t, loopbackBinding, f.endpoint)
	ctx, cancel := context.WithCancel(f.resolver.root)
	defer cancel()
	done := make(chan error, 3)
	for i := 0; i < 3; i++ {
		go func() {
			_, _, err := f.resolver.queryDoT(ctx, "parallel.example", dnsTypeA, loopbackBinding, f.endpoint)
			done <- err
		}()
	}
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("pool serialized independent queries")
		}
	}
	select {
	case <-started:
		t.Fatal("pool exceeded two active queries")
	case <-time.After(20 * time.Millisecond):
	}
	cancel()
	for i := 0; i < 3; i++ {
		select {
		case err := <-done:
			if err == nil {
				t.Fatal("canceled query succeeded")
			}
		case <-time.After(time.Second):
			t.Fatal("active or queued query ignored cancellation")
		}
	}
	if f.closed.Load() != 2 {
		t.Fatal("active TLS sockets leaked")
	}
}

func TestDoTLaterEndpointBatchReceivesTimeoutBudget(t *testing.T) {
	f := newDoTFixture(t, false, nil)
	f.trust(t, loopbackBinding, f.endpoint)
	config, err := NormalizeConfig(Config{Policy: PolicyDoT, DoTServers: []string{"tls://192.0.2.1", "tls://192.0.2.2", "tls://example.com:8853"}, QueryTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	f.resolver.config = config
	originalDial := f.resolver.dial
	var firstBatch atomic.Int64
	f.resolver.dial = func(ctx context.Context, network, address string, binding Binding) (net.Conn, error) {
		if address == "192.0.2.1:853" || address == "192.0.2.2:853" {
			firstBatch.Add(1)
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return originalDial(ctx, network, address, binding)
	}
	result, err := f.resolver.Resolve(context.Background(), Query{Domain: "budget.example", Binding: loopbackBinding})
	if err != nil || result.Transport != "dot" || firstBatch.Load() != 2 {
		t.Fatalf("later DoT endpoint starved: %+v %v dials=%d", result, err, firstBatch.Load())
	}
}
