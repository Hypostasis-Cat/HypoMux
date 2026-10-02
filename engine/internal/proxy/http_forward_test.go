package proxy

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func startHTTPForwardTestProxy(t *testing.T) string {
	t.Helper()
	s, err := New(Config{Adapters: []Adapter{{Name: "loopback", SourceIP: "127.0.0.1"}}})
	if err != nil {
		t.Fatal(err)
	}
	endpoints, err := s.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stopServer(t, s) })
	return endpoints.HTTP
}

func TestHTTPForwardDoesNotLeakRequestsAcrossOrigins(t *testing.T) {
	var aRequests atomic.Int32
	originA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		aRequests.Add(1)
		if r.Header.Get("Authorization") != "" || r.URL.Path != "/a" {
			t.Error("origin A received origin B's request")
		}
		_, _ = io.WriteString(w, "origin-A")
	}))
	defer originA.Close()
	originB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer origin-B-test-secret" {
			t.Error("origin B lost its credential")
		}
		if r.Header.Get("Proxy-Authorization") != "" {
			t.Error("proxy credential leaked upstream")
		}
		_, _ = io.WriteString(w, "origin-B")
	}))
	defer originB.Close()
	proxyURL, _ := url.Parse("http://" + startHTTPForwardTestProxy(t))
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	for _, target := range []string{originA.URL + "/a", originB.URL + "/b", originB.URL + "/b"} {
		request, _ := http.NewRequest(http.MethodGet, target, nil)
		if strings.HasPrefix(target, originB.URL) {
			request.Header.Set("Authorization", "Bearer origin-B-test-secret")
		}
		request.Header.Set("Proxy-Authorization", "proxy-test-secret")
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		want := "origin-B"
		if strings.HasPrefix(target, originA.URL) {
			want = "origin-A"
		}
		if string(body) != want || !response.Close {
			t.Fatalf("got body=%q close=%t", body, response.Close)
		}
	}
	if aRequests.Load() != 1 {
		t.Fatalf("origin A received %d requests", aRequests.Load())
	}
}

func TestHTTPForwardStopsAtFirstPipelinedRequest(t *testing.T) {
	var count atomic.Int32
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		if r.URL.Path != "/first" {
			t.Error("pipelined request leaked upstream")
		}
		_, _ = io.WriteString(w, "first-only")
	}))
	defer origin.Close()
	client, err := net.Dial("tcp", startHTTPForwardTestProxy(t))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	_ = client.SetDeadline(time.Now().Add(3 * time.Second))
	_, _ = fmt.Fprintf(client, "GET %s/first HTTP/1.1\r\nHost: ignored.example\r\n\r\nGET %s/second HTTP/1.1\r\nHost: other.example\r\nAuthorization: secret\r\n\r\n", origin.URL, origin.URL)
	reader := bufio.NewReader(client)
	response, err := http.ReadResponse(reader, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ReadByte(); err != io.EOF {
		t.Fatalf("expected closed connection, got %v", err)
	}
	if count.Load() != 1 {
		t.Fatalf("upstream received %d requests", count.Load())
	}
}

func TestHTTPForwardBodiesAndHopHeaders(t *testing.T) {
	for _, chunked := range []bool{false, true} {
		t.Run(fmt.Sprintf("chunked=%t", chunked), func(t *testing.T) {
			payload := strings.Repeat("upload", 20_000)
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil || string(body) != payload {
					t.Errorf("body length=%d error=%v", len(body), err)
				}
				if r.Header.Get("X-Hop") != "" || r.Header.Get("Proxy-Authorization") != "" {
					t.Error("hop header leaked")
				}
				if chunked && r.Trailer.Get("X-Trailer") != "complete" {
					t.Error("request trailer lost")
				}
				conn, output, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				defer conn.Close()
				_, _ = fmt.Fprint(output, "HTTP/1.1 200 OK\r\nConnection: X-Response-Hop\r\nX-Response-Hop: private\r\n")
				if chunked {
					_, _ = fmt.Fprintf(output, "Transfer-Encoding: chunked\r\nTrailer: X-Response-Trailer\r\n\r\n%x\r\n%s\r\n0\r\nX-Response-Trailer: complete\r\n\r\n", len(body), body)
				} else {
					_, _ = fmt.Fprintf(output, "Content-Length: %d\r\n\r\n%s", len(body), body)
				}
				_ = output.Flush()
			}))
			defer origin.Close()
			proxyURL, _ := url.Parse("http://" + startHTTPForwardTestProxy(t))
			transport := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
			defer transport.CloseIdleConnections()
			request, _ := http.NewRequest(http.MethodPost, origin.URL, strings.NewReader(payload))
			request.Header.Set("Connection", "X-Hop")
			request.Header.Set("X-Hop", "private")
			request.Header.Set("Proxy-Authorization", "private")
			if chunked {
				request.ContentLength = -1
				request.Trailer = http.Header{"X-Trailer": {"complete"}}
			}
			response, err := (&http.Client{Transport: transport, Timeout: 3 * time.Second}).Do(request)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if err != nil || string(body) != payload {
				t.Fatalf("response length=%d error=%v", len(body), err)
			}
			if response.Header.Get("X-Response-Hop") != "" {
				t.Fatal("response hop header leaked")
			}
			if chunked && response.Trailer.Get("X-Response-Trailer") != "complete" {
				t.Fatal("response trailer lost")
			}
		})
	}
}

func TestHTTPForwardContinueAndEarlyUploadRejection(t *testing.T) {
	for _, reject := range []bool{false, true} {
		t.Run(fmt.Sprintf("reject=%t", reject), func(t *testing.T) {
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if reject {
					w.Header().Set("Connection", "close")
					w.WriteHeader(http.StatusRequestEntityTooLarge)
					return
				}
				body, _ := io.ReadAll(r.Body)
				_, _ = w.Write(body)
			}))
			defer origin.Close()
			client, err := net.Dial("tcp", startHTTPForwardTestProxy(t))
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			_ = client.SetDeadline(time.Now().Add(3 * time.Second))
			_, _ = fmt.Fprintf(client, "POST %s/ HTTP/1.1\r\nHost: ignored\r\nContent-Length: 5\r\nExpect: 100-continue\r\n\r\n", origin.URL)
			reader := bufio.NewReader(client)
			response, err := http.ReadResponse(reader, nil)
			if err != nil {
				t.Fatal(err)
			}
			if reject {
				if response.StatusCode != 413 {
					t.Fatal(response.Status)
				}
			} else {
				if response.StatusCode != 100 {
					t.Fatal(response.Status)
				}
				_ = response.Body.Close()
				_, _ = io.WriteString(client, "hello")
				response, err = http.ReadResponse(reader, nil)
				if err != nil {
					t.Fatal(err)
				}
			}
			body, err := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			if !reject && string(body) != "hello" {
				t.Fatalf("body=%q", body)
			}
			if _, err := reader.ReadByte(); err != io.EOF {
				t.Fatalf("connection did not close: %v", err)
			}
		})
	}
}

func TestHTTPForwardHEADDoesNotReadResponseBody(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Header().Set("Content-Length", "1000000") }))
	defer origin.Close()
	proxyURL, _ := url.Parse("http://" + startHTTPForwardTestProxy(t))
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
	defer transport.CloseIdleConnections()
	request, _ := http.NewRequest(http.MethodHead, origin.URL, nil)
	response, err := (&http.Client{Transport: transport, Timeout: 3 * time.Second}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || len(body) != 0 || response.ContentLength != 1000000 {
		t.Fatalf("HEAD response: %d %v", len(body), err)
	}
}

func TestHTTPForwardFailedDialDoesNotDrainUnsentUpload(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	target := listener.Addr().String()
	_ = listener.Close()
	client, err := net.Dial("tcp", startHTTPForwardTestProxy(t))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	_ = client.SetDeadline(time.Now().Add(3 * time.Second))
	_, _ = fmt.Fprintf(client, "POST http://%s/ HTTP/1.1\r\nHost: ignored\r\nContent-Length: 1000000\r\n\r\n", target)
	reader := bufio.NewReader(client)
	response, err := http.ReadResponse(reader, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadGateway {
		t.Fatal(response.Status)
	}
	if _, err := reader.ReadByte(); err != io.EOF {
		t.Fatalf("failed dial kept waiting for an unsent upload: %v", err)
	}
}

func TestHTTPForwardCancelsBlockedTransfers(t *testing.T) {
	for _, rejectUpload := range []bool{true, false} {
		t.Run(fmt.Sprintf("reject-upload=%t", rejectUpload), func(t *testing.T) {
			s, err := New(Config{Adapters: []Adapter{{Name: "loopback", SourceIP: "127.0.0.1"}}})
			if err != nil {
				t.Fatal(err)
			}
			client, relayClient := net.Pipe()
			upstream, origin := net.Pipe()
			defer client.Close()
			defer relayClient.Close()
			defer upstream.Close()
			defer origin.Close()
			_ = client.SetDeadline(time.Now().Add(3 * time.Second))
			request, _ := http.NewRequest(http.MethodGet, "http://example.test/", nil)
			if rejectUpload {
				request, _ = http.NewRequest(http.MethodPost, "http://example.test/", strings.NewReader(strings.Repeat("x", 1024*1024)))
			}
			prepareHTTPForwardRequest(request)
			session := s.registry.Begin("http", "", relayClient)
			defer s.registry.Finish(session)
			// Keep the origin open without consuming any more upload or sending
			// the remaining response. A correct relay must cancel its own I/O.
			releaseOrigin := make(chan struct{})
			defer close(releaseOrigin)
			go func() {
				if _, err := readHTTPHeader(bufio.NewReader(origin)); err != nil {
					return
				}
				if rejectUpload {
					_, _ = io.WriteString(origin, "HTTP/1.1 413 Content Too Large\r\nContent-Length: 0\r\n\r\n")
				} else {
					_, _ = io.WriteString(origin, "HTTP/1.1 200 OK\r\nContent-Length: 1000000\r\n\r\n"+strings.Repeat("x", 64*1024))
				}
				<-releaseOrigin
			}()
			done := make(chan struct{})
			go func() {
				s.relayHTTP(request, bufio.NewReader(relayClient), relayClient, upstream, session)
				close(done)
			}()
			response, err := http.ReadResponse(bufio.NewReader(client), request)
			if err != nil {
				t.Fatal(err)
			}
			if rejectUpload && response.StatusCode != http.StatusRequestEntityTooLarge {
				t.Fatal(response.Status)
			}
			_ = client.Close()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("relay waited for an origin that stopped transferring")
			}
		})
	}
}

func TestHTTPForwardProtocolUpgradeRemainsDuplex(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !httpUpgrade(r.Header) || r.Header.Get("Proxy-Authorization") != "" {
			t.Error("upgrade request changed or leaked proxy credentials")
		}
		connection, output, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer connection.Close()
		_, _ = io.WriteString(output, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: test-protocol\r\n\r\nready")
		_ = output.Flush()
		_, _ = io.Copy(connection, output)
	}))
	defer origin.Close()
	client, err := net.Dial("tcp", startHTTPForwardTestProxy(t))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	_ = client.SetDeadline(time.Now().Add(3 * time.Second))
	_, _ = fmt.Fprintf(client, "GET %s HTTP/1.1\r\nHost: ignored\r\nConnection: Upgrade\r\nUpgrade: test-protocol\r\nProxy-Authorization: private\r\n\r\n", origin.URL)
	reader := bufio.NewReader(client)
	response, err := http.ReadResponse(reader, nil)
	if err != nil || response.StatusCode != 101 {
		t.Fatalf("upgrade failed: %v %v", response, err)
	}
	ready := make([]byte, 5)
	if _, err := io.ReadFull(reader, ready); err != nil || string(ready) != "ready" {
		t.Fatalf("upstream read-ahead lost: %q %v", ready, err)
	}
	_, _ = io.WriteString(client, "ping")
	pong := make([]byte, 4)
	if _, err := io.ReadFull(reader, pong); err != nil || string(pong) != "ping" {
		t.Fatalf("upgrade was not duplex: %q %v", pong, err)
	}
}

type httpCountingReader struct {
	io.Reader
	bytes int
}

func (r *httpCountingReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.bytes += n
	return n, err
}

func TestHTTPHeaderLimitBoundsReads(t *testing.T) {
	for _, suffix := range []string{"", "\r\n\r\n"} {
		source := &httpCountingReader{Reader: strings.NewReader(strings.Repeat("x", 4*maxHTTPHeaderBytes) + suffix)}
		if _, err := readHTTPHeader(bufio.NewReaderSize(source, 4096)); err == nil {
			t.Fatal("oversize line accepted")
		}
		if source.bytes > maxHTTPHeaderBytes+4096 {
			t.Fatalf("read %d bytes before rejection", source.bytes)
		}
	}
	for _, size := range []int{maxHTTPHeaderBytes, maxHTTPHeaderBytes + 1} {
		header := append(bytes.Repeat([]byte{'x'}, size-4), []byte("\r\n\r\n")...)
		got, err := readHTTPHeader(bufio.NewReaderSize(bytes.NewReader(header), 1024))
		if size == maxHTTPHeaderBytes && (err != nil || !bytes.Equal(got, header)) {
			t.Fatalf("boundary rejected: %v", err)
		}
		if size > maxHTTPHeaderBytes && err == nil {
			t.Fatal("over-limit header accepted")
		}
	}
}
