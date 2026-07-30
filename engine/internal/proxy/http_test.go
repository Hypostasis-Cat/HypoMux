package proxy

import (
	"strings"
	"testing"
)

func TestParseConnectAndAbsoluteHTTPRequests(t *testing.T) {
	connect, err := parseProxyRequest([]byte(
		"CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n",
	))
	if err != nil {
		t.Fatalf("parse CONNECT: %v", err)
	}
	if !connect.connect || connect.host != "example.com" || connect.port != 443 {
		t.Fatalf("CONNECT = %#v", connect)
	}

	request, err := parseProxyRequest([]byte(
		"GET http://example.com:8080/path?q=1 HTTP/1.1\r\n" +
			"Host: example.com:8080\r\n" +
			"Proxy-Connection: keep-alive\r\n" +
			"X-Test: yes\r\n\r\n",
	))
	if err != nil {
		t.Fatalf("parse absolute request: %v", err)
	}
	if request.connect || request.host != "example.com" || request.port != 8080 {
		t.Fatalf("request = %#v", request)
	}
	header := string(request.forwardHeader)
	if !strings.HasPrefix(header, "GET /path?q=1 HTTP/1.1\r\n") {
		t.Fatalf("origin request line missing: %q", header)
	}
	if strings.Contains(strings.ToLower(header), "proxy-connection") {
		t.Fatalf("proxy-only header leaked: %q", header)
	}
	if !strings.Contains(header, "X-Test: yes\r\n") {
		t.Fatalf("ordinary header missing: %q", header)
	}
}

func TestSplitHostPortRejectsInvalidPort(t *testing.T) {
	if _, _, err := splitHostPort("example.com:bad", 80); err == nil {
		t.Fatal("invalid explicit port unexpectedly accepted")
	}
	if host, port, err := splitHostPort("[::1]", 443); err != nil || host != "::1" || port != 443 {
		t.Fatalf("bracketed IPv6 = %q, %d, %v", host, port, err)
	}
}
