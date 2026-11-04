package services

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// startRuleSetConnectProxy tunnels every CONNECT to upstream, whatever host was
// asked for, and records the requested targets.
func startRuleSetConnectProxy(t *testing.T, upstream string) (*url.URL, func() []string) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	var mu sync.Mutex
	targets := []string{}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				request, err := http.ReadRequest(bufio.NewReader(conn))
				if err != nil || request.Method != http.MethodConnect {
					return
				}
				mu.Lock()
				targets = append(targets, request.Host)
				mu.Unlock()
				remote, err := net.Dial("tcp", upstream)
				if err != nil {
					return
				}
				defer remote.Close()
				if _, err := io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
					return
				}
				go func() { _, _ = io.Copy(remote, conn) }()
				_, _ = io.Copy(conn, remote)
			}()
		}
	}()
	proxy := &url.URL{Scheme: "http", Host: listener.Addr().String()}
	return proxy, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), targets...)
	}
}

func insecureRuleSetProxyTransport(proxy *url.URL) *http.Transport {
	transport := ruleSetTransport(proxy)
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	return transport
}

func TestRuleSetFetchTunnelsThroughLoopbackProxy(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("payload:\n  - example.com\n"))
	}))
	defer server.Close()
	proxy, targets := startRuleSetConnectProxy(t, server.Listener.Addr().String())

	// The name does not resolve here at all: the proxy must be the one to resolve it.
	set := RuleSet{ID: "one", Name: "List", URL: "https://rules.hypomux-test.example/list.yaml"}
	result, err := fetchRuleSetOnce(context.Background(), set, insecureRuleSetProxyTransport(proxy))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(result.Body), "example.com") {
		t.Fatalf("body = %q", result.Body)
	}
	if got := targets(); len(got) != 1 || got[0] != "rules.hypomux-test.example:443" {
		t.Fatalf("CONNECT targets = %v", got)
	}
}

func TestRuleSetProxyRefusesPrivateTargetsAndRedirects(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Redirect(w, &http.Request{}, "https://192.168.1.1/list.yaml", http.StatusFound)
	}))
	defer server.Close()
	proxy, targets := startRuleSetConnectProxy(t, server.Listener.Addr().String())

	for _, target := range []string{"https://127.0.0.1/a", "https://[::1]/a", "https://10.0.0.8/a", "https://localhost/a", "https://nas.local/a"} {
		_, err := fetchRuleSetOnce(context.Background(), RuleSet{URL: target}, insecureRuleSetProxyTransport(proxy))
		if !errors.Is(err, errRuleSetBlockedTarget) {
			t.Fatalf("%s: err = %v", target, err)
		}
	}
	if got := targets(); len(got) != 0 {
		t.Fatalf("private targets reached the proxy: %v", got)
	}
	_, err := fetchRuleSetOnce(context.Background(), RuleSet{URL: "https://rules.hypomux-test.example/a"}, insecureRuleSetProxyTransport(proxy))
	if !errors.Is(err, errRuleSetBlockedTarget) {
		t.Fatalf("redirect to the LAN: err = %v", err)
	}
}

func TestGuardRuleSetProxyTarget(t *testing.T) {
	for _, host := range []string{"127.0.0.1", "::1", "0.0.0.0", "10.1.2.3", "192.168.1.1", "169.254.1.1", "fe80::1%eth0", "LOCALHOST", "localhost.", "a.localhost", "nas.local", "intranet", "2130706433", "127.1", "0x7f.1", "0x7f000001"} {
		if err := guardRuleSetProxyTarget(host); err == nil {
			t.Fatalf("private host %q accepted", host)
		}
	}
	for _, host := range []string{"raw.githubusercontent.com", "cdn.jsdelivr.net", "93.184.216.34", "2606:2800:220:1::1", "1password.com", "rules.example.com."} {
		if err := guardRuleSetProxyTarget(host); err != nil {
			t.Fatalf("public host %q rejected: %v", host, err)
		}
	}
}

func TestFetchRuleSetFallback(t *testing.T) {
	proxy := &url.URL{Scheme: "http", Host: "127.0.0.1:7897"}
	connectFailure := &ruleSetConnectError{err: errors.New("connection refused")}
	serverFailure := errors.New("订阅服务器返回状态码 404")
	blocked := &ruleSetConnectError{err: errRuleSetBlockedTarget}

	for _, test := range []struct {
		name         string
		proxyErr     error
		directErr    error
		wantRoutes   []string
		wantErr      string
		wantNoErrors bool
	}{
		{name: "proxy works", wantRoutes: []string{"proxy"}, wantNoErrors: true},
		{name: "dead proxy falls back", proxyErr: connectFailure, wantRoutes: []string{"proxy", "direct"}, wantNoErrors: true},
		{name: "both fail", proxyErr: connectFailure, directErr: connectFailure, wantRoutes: []string{"proxy", "direct"}, wantErr: "直连也失败"},
		{name: "server answer is final", proxyErr: serverFailure, wantRoutes: []string{"proxy"}, wantErr: "404"},
		{name: "blocked target is final", proxyErr: blocked, wantRoutes: []string{"proxy"}, wantErr: "内网"},
	} {
		t.Run(test.name, func(t *testing.T) {
			routes := []string{}
			_, err := fetchRuleSetWithFallback(context.Background(), RuleSet{}, proxy, func(_ context.Context, _ RuleSet, via *url.URL) (ruleSetFetchResult, error) {
				if via != nil {
					routes = append(routes, "proxy")
					return ruleSetFetchResult{}, test.proxyErr
				}
				routes = append(routes, "direct")
				return ruleSetFetchResult{}, test.directErr
			})
			if strings.Join(routes, ",") != strings.Join(test.wantRoutes, ",") {
				t.Fatalf("routes = %v, want %v", routes, test.wantRoutes)
			}
			if test.wantNoErrors != (err == nil) || (err != nil && !strings.Contains(err.Error(), test.wantErr)) {
				t.Fatalf("err = %v, want containing %q", err, test.wantErr)
			}
		})
	}

	_, err := fetchRuleSetWithFallback(context.Background(), RuleSet{}, nil, func(context.Context, RuleSet, *url.URL) (ruleSetFetchResult, error) {
		return ruleSetFetchResult{}, connectFailure
	})
	if err == nil || !strings.Contains(err.Error(), "未检测到可用代理") {
		t.Fatalf("direct failure does not explain the missing proxy: %v", err)
	}
}

func TestParseSystemProxyServer(t *testing.T) {
	for _, test := range []struct{ server, want string }{
		{"127.0.0.1:7897", "http://127.0.0.1:7897"},
		{"127.0.0.1:10808", "http://127.0.0.1:10808"},
		{"http=127.0.0.1:7890;https=127.0.0.1:7891;socks=127.0.0.1:7892", "http://127.0.0.1:7891"},
		{"http=127.0.0.1:7890", "http://127.0.0.1:7890"},
		{"socks=127.0.0.1:1080", "socks5://127.0.0.1:1080"},
		{"socks5://127.0.0.1:1080", "socks5://127.0.0.1:1080"},
		{"http://proxy.corp.example:8080", "http://proxy.corp.example:8080"},
		{"ftp=127.0.0.1:21", ""},
		{"", ""},
		{"not a proxy", ""},
		{"127.0.0.1", ""},
	} {
		proxy := parseSystemProxyServer(test.server)
		got := ""
		if proxy != nil {
			got = proxy.String()
		}
		if got != test.want {
			t.Fatalf("parseSystemProxyServer(%q) = %q, want %q", test.server, got, test.want)
		}
	}
}
