package proxy

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestHTTPForwardDoesNotHalfCloseBeforeResponse(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, stream, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		_, err = stream.ReadByte()
		if timeout, ok := err.(net.Error); !ok || !timeout.Timeout() {
			t.Errorf("upstream received data or FIN before its response: %v", err)
			return
		}
		_ = conn.SetReadDeadline(time.Time{})
		_, _ = fmt.Fprint(stream, "HTTP/1.1 206 Partial Content\r\nContent-Length: 5\r\nConnection: close\r\n\r\nchunk")
		_ = stream.Flush()
	}))
	defer origin.Close()
	proxyURL, _ := url.Parse("http://" + startHTTPForwardTestProxy(t))
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
	defer transport.CloseIdleConnections()
	response, err := (&http.Client{Transport: transport, Timeout: 3 * time.Second}).Get(origin.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != http.StatusPartialContent || string(body) != "chunk" {
		t.Fatalf("status=%d body=%q error=%v", response.StatusCode, body, err)
	}
}
