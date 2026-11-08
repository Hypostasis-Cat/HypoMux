//go:build windows

package services

import (
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// sing-box can skip an upstream answer that arrives while a match_response
// rule is being checked. With a dead first DoH and a warm second one this used
// to turn roughly one query in two hundred into SERVFAIL.
func TestTUNDNSPoolKeepsAnswerFromFastUpstream(t *testing.T) {
	doh := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/dns-message")
		_, _ = w.Write(poolDNSAnswer(query))
	}))
	defer doh.Close()
	_, tlsPort, _ := net.SplitHostPort(doh.Listener.Addr().String())
	pool := []dnsResolveResult{
		{Transport: "doh", Server: "example.com@127.0.0.1:1", DoHPath: "/custom/query"},
		{Transport: "doh", Server: "example.com@127.0.0.1:" + tlsPort, DoHPath: "/custom/query"},
	}
	dns, err := buildTUNDNSPool(AdapterView{Address: "127.0.0.1"}, tunConfigOptions{DNSPolicy: "custom", DNSUpstreams: pool})
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range dns["servers"].([]any) {
		if tls, ok := raw.(map[string]any)["tls"].(map[string]any); ok {
			tls["certificate"] = []string{string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: doh.Certificate().Raw}))}
		}
	}
	dns["disable_cache"] = true
	dnsPort := poolTestPort(t)
	config := map[string]any{
		"log": map[string]any{"level": "fatal"}, "dns": dns,
		"inbounds":  []any{map[string]any{"type": "direct", "tag": "test-dns", "listen": "127.0.0.1", "listen_port": dnsPort}},
		"outbounds": []any{map[string]any{"type": "direct", "tag": "test-direct"}},
		"route":     map[string]any{"default_domain_resolver": dns["final"], "rules": []any{map[string]any{"inbound": "test-dns", "action": "hijack-dns"}}},
	}
	executable, err := resolveRuntimeAsset("sing-box.exe")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "pool.json")
	data, _ := json.Marshal(config)
	_ = os.WriteFile(path, data, 0600)
	cmd := exec.Command(executable, "run", "-c", path)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(dnsPort))
	deadline := time.Now().Add(3 * time.Second)
	for {
		probe, err := net.DialTimeout("tcp4", address, 50*time.Millisecond)
		if err == nil {
			probe.Close()
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
	fails := 0
	const total = 1000
	buf := make([]byte, 4096)
	for i := range total {
		query := dnsmessage.Message{Header: dnsmessage.Header{ID: uint16(i), RecursionDesired: true}, Questions: []dnsmessage.Question{{Name: dnsmessage.MustNewName(fmt.Sprintf("n%d.example.", i)), Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}}}
		wire, _ := query.Pack()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		_, _ = conn.Write(wire)
		n, err := conn.Read(buf)
		if err != nil {
			fails++
			continue
		}
		var answer dnsmessage.Message
		if answer.Unpack(buf[:n]) != nil || answer.RCode != dnsmessage.RCodeSuccess {
			fails++
		}
	}
	if fails > 0 {
		t.Fatalf("%d of %d queries failed although the second DoH was healthy", fails, total)
	}
}
