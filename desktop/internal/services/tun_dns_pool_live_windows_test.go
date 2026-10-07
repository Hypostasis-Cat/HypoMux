//go:build windows

package services

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
	"golang.org/x/net/proxy"
)

// Exercise the actual bundled resolver on loopback without creating a TUN,
// changing routes, or contacting public DNS services.
func TestMultipleTUNDNSLiveFallback(t *testing.T) {
	for _, scenario := range []string{"second-doh", "second-dns", "strict-failure", "fakeip-route"} {
		t.Run(scenario, func(t *testing.T) {
			var plainQueries atomic.Int64
			legacy, err := net.ListenPacket("udp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { legacy.Close() })
			go func() {
				buf := make([]byte, 4096)
				for {
					n, remote, err := legacy.ReadFrom(buf)
					if err != nil {
						return
					}
					plainQueries.Add(1)
					answer := poolDNSAnswer(buf[:n])
					if answer != nil {
						_, _ = legacy.WriteTo(answer, remote)
					}
				}
			}()
			var customRequests atomic.Int64
			doh := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.RequestURI() != "/custom/query" {
					t.Errorf("DoH URL changed: %s", r.URL.RequestURI())
					http.Error(w, "wrong URL", 400)
					return
				}
				if r.Host != "example.com:"+strconv.Itoa(dohPort(r)) {
					t.Errorf("DoH Host changed: %s", r.Host)
				}
				customRequests.Add(1)
				if scenario != "second-doh" && scenario != "fakeip-route" {
					http.Error(w, "unavailable", 503)
					return
				}
				query, err := io.ReadAll(r.Body)
				if err != nil {
					http.Error(w, "bad query", 400)
					return
				}
				w.Header().Set("Content-Type", "application/dns-message")
				_, _ = w.Write(poolDNSAnswer(query))
			}))
			t.Cleanup(doh.Close)
			_, tlsPort, _ := net.SplitHostPort(doh.Listener.Addr().String())
			pool := []dnsResolveResult{
				{Transport: "doh", Server: "example.com@127.0.0.1:1", DoHPath: "/custom/query"},
				{Transport: "doh", Server: "example.com@127.0.0.1:" + tlsPort, DoHPath: "/custom/query"},
			}
			policy := "custom"
			if scenario == "second-dns" {
				policy = "auto"
				pool = append(pool, dnsResolveResult{Transport: "udp", Server: "127.0.0.1:1"}, dnsResolveResult{Transport: "udp", Server: legacy.LocalAddr().String()})
			}
			dns, err := buildTUNDNSPool(AdapterView{Address: "127.0.0.1"}, tunConfigOptions{DNSPolicy: policy, DNSUpstreams: pool})
			if err != nil {
				t.Fatal(err)
			}
			for _, raw := range dns["servers"].([]any) {
				server := raw.(map[string]any)
				if tls, ok := server["tls"].(map[string]any); ok {
					tls["certificate"] = []string{string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: doh.Certificate().Raw}))}
				}
			}
			if scenario == "fakeip-route" {
				dns["servers"] = append(dns["servers"].([]any), map[string]any{"type": "fakeip", "tag": "dns-fakeip", "inet4_range": "198.18.0.0/15", "inet6_range": "fc00::/18"})
				dns["rules"] = append([]any{map[string]any{"protocol": "dns", "query_type": []string{"A", "AAAA"}, "server": "dns-fakeip"}}, dns["rules"].([]any)...)
			}
			dnsPort, socksPort := poolTestPort(t), poolTestPort(t)
			config := map[string]any{
				"log": map[string]any{"level": "warn"}, "dns": dns,
				"inbounds":  []any{map[string]any{"type": "direct", "tag": "test-dns", "listen": "127.0.0.1", "listen_port": dnsPort}, map[string]any{"type": "socks", "tag": "test-socks", "listen": "127.0.0.1", "listen_port": socksPort}},
				"outbounds": []any{map[string]any{"type": "direct", "tag": "test-direct"}},
				"route":     map[string]any{"default_domain_resolver": dns["final"], "rules": []any{map[string]any{"inbound": "test-dns", "action": "sniff", "timeout": "50ms"}, map[string]any{"inbound": "test-dns", "action": "hijack-dns"}, map[string]any{"action": "resolve", "strategy": "ipv4_only"}}},
			}
			executable, err := resolveRuntimeAsset("sing-box.exe")
			if err != nil {
				t.Fatal(err)
			}
			directory := t.TempDir()
			path := filepath.Join(directory, "pool.json")
			data, _ := json.Marshal(config)
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			checkSingBoxConfig(t, executable, path)
			logs, err := os.Create(filepath.Join(directory, "singbox.log"))
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(executable, "run", "--disable-color", "-c", path)
			cmd.Stdout = logs
			cmd.Stderr = logs
			if err := cmd.Start(); err != nil {
				logs.Close()
				t.Fatal(err)
			}
			t.Cleanup(func() {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				_ = logs.Close()
				if t.Failed() {
					output, _ := os.ReadFile(logs.Name())
					t.Logf("sing-box: %s", output)
				}
			})
			address := net.JoinHostPort("127.0.0.1", strconv.Itoa(dnsPort))
			deadline := time.Now().Add(3 * time.Second)
			for {
				conn, err := net.DialTimeout("tcp4", address, 50*time.Millisecond)
				if err == nil {
					conn.Close()
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("sing-box did not listen")
				}
				time.Sleep(10 * time.Millisecond)
			}
			conn, err := net.Dial("udp4", address)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
			query := dnsmessage.Message{Header: dnsmessage.Header{ID: 42, RecursionDesired: true}, Questions: []dnsmessage.Question{{Name: dnsmessage.MustNewName("target.example."), Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}}}
			wire, _ := query.Pack()
			if _, err := conn.Write(wire); err != nil {
				t.Fatal(err)
			}
			buf := make([]byte, 4096)
			n, err := conn.Read(buf)
			if err != nil {
				t.Fatal(err)
			}
			var answer dnsmessage.Message
			if err := answer.Unpack(buf[:n]); err != nil {
				t.Fatal(err)
			}
			if scenario == "strict-failure" {
				if answer.RCode != dnsmessage.RCodeServerFailure || plainQueries.Load() != 0 {
					t.Fatalf("strict policy downgraded: code=%d plain=%d", answer.RCode, plainQueries.Load())
				}
				return
			}
			if answer.RCode != dnsmessage.RCodeSuccess || len(answer.Answers) != 1 {
				t.Fatalf("fallback answer: %+v", answer)
			}
			if scenario == "second-doh" && (customRequests.Load() == 0 || plainQueries.Load() != 0) {
				t.Fatal("healthy DoH was not selected")
			}
			if scenario == "fakeip-route" {
				ip, ok := answer.Answers[0].Body.(*dnsmessage.AResource)
				if !ok || ip.A[0] != 198 || (ip.A[1] != 18 && ip.A[1] != 19) || customRequests.Load() != 0 || plainQueries.Load() != 0 {
					t.Fatalf("intercepted DNS did not use FakeIP: %+v", answer)
				}
			}
			if scenario == "second-dns" && plainQueries.Load() == 0 {
				t.Fatal("second DNS was not used")
			}
			// Route resolution must use the pool too, rather than the first failed
			// upstream. This checks the same resolve action used before CIDR rules.
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("pool works")) }))
			defer target.Close()
			_, port, _ := net.SplitHostPort(target.Listener.Addr().String())
			dialer, err := proxy.SOCKS5("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(socksPort)), nil, &net.Dialer{Timeout: 5 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
				return dialer.(proxy.ContextDialer).DialContext(ctx, network, address)
			}}}
			response, err := client.Get("http://route.example:" + port + "/")
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(response.Body)
			response.Body.Close()
			client.CloseIdleConnections()
			if string(body) != "pool works" {
				t.Fatalf("route lookup result: %s", body)
			}
		})
	}
}

func dohPort(r *http.Request) int {
	_, port, _ := net.SplitHostPort(r.Context().Value(http.LocalAddrContextKey).(net.Addr).String())
	value, _ := strconv.Atoi(port)
	return value
}

func poolDNSAnswer(wire []byte) []byte {
	var query dnsmessage.Message
	if query.Unpack(wire) != nil || len(query.Questions) != 1 {
		return nil
	}
	question := query.Questions[0]
	answer := dnsmessage.Message{Header: dnsmessage.Header{ID: query.ID, Response: true, RecursionAvailable: true}, Questions: query.Questions}
	if question.Type == dnsmessage.TypeA {
		answer.Answers = []dnsmessage.Resource{{Header: dnsmessage.ResourceHeader{Name: question.Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: 30}, Body: &dnsmessage.AResource{A: [4]byte{127, 0, 0, 1}}}}
	}
	result, _ := answer.Pack()
	return result
}

func poolTestPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}
