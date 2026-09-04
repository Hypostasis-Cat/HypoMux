//go:build windows

package services

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestTUNStacksAndPersistentCacheConfig(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	endpoints := map[string]string{
		"nic_ethernet": "127.0.0.1:19101",
		"nic_wifi":     "127.0.0.1:19102",
		"aggregation":  "127.0.0.1:19103",
	}
	wantCache := filepath.Join(settingsDirectory(), "cache", "sing-box.db")
	for _, stack := range []string{"", "system", "mixed", "gvisor"} {
		for _, policy := range []string{"auto", "off", "system"} {
			for _, ipv6 := range []bool{true, false} {
				executable, path, _, err := writeSingBoxConfigWithOptions(
					endpoints, AdapterView{}, dnsResolveResult{Transport: "udp", Server: "1.1.1.1"},
					nil, compatibilityPlan{}, false,
					tunConfigOptions{Stack: stack, DNSPolicy: policy, IPv6Available: ipv6, ConfigName: "test.json"},
				)
				if err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				var config struct {
					Inbounds     []map[string]any `json:"inbounds"`
					Experimental struct {
						Cache struct {
							Enabled     bool   `json:"enabled"`
							Path        string `json:"path"`
							StoreFakeIP bool   `json:"store_fakeip"`
						} `json:"cache_file"`
					} `json:"experimental"`
				}
				if err := json.Unmarshal(data, &config); err != nil {
					t.Fatal(err)
				}
				wantStack, _ := normalizeTunStack(stack)
				if config.Inbounds[0]["stack"] != wantStack {
					t.Fatalf("stack = %v, want %s", config.Inbounds[0]["stack"], wantStack)
				}
				cache := config.Experimental.Cache
				if !cache.Enabled || cache.Path != wantCache || !filepath.IsAbs(cache.Path) || cache.StoreFakeIP != (policy == "auto") {
					t.Fatalf("unexpected cache for %s/%s/ipv6=%v: %#v", stack, policy, ipv6, cache)
				}
				if _, exists := config.Inbounds[0]["endpoint_independent_nat"]; exists {
					t.Fatal("stack selection must not change NAT defaults")
				}
				checkSingBoxConfig(t, executable, path)
			}
		}
	}
	if _, _, _, err := writeSingBoxConfigWithOptions(endpoints, AdapterView{}, dnsResolveResult{}, nil, compatibilityPlan{}, false, tunConfigOptions{Stack: "invalid"}); err == nil {
		t.Fatal("config generation must reject an invalid stack")
	}
}

// Exercise the bundled binary without creating a TUN adapter or changing the
// host's DNS/routes. Kill mirrors the engine's current sidecar stop behavior.
func TestBundledSingBoxRestoresFakeIPAfterRestart(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	executable, path, _, err := writeSingBoxConfigWithOptions(
		map[string]string{"nic_ethernet": "127.0.0.1:19101", "nic_wifi": "127.0.0.1:19102", "aggregation": "127.0.0.1:19103"},
		AdapterView{}, dnsResolveResult{Transport: "udp", Server: "1.1.1.1"}, nil, compatibilityPlan{}, false,
		tunConfigOptions{DNSPolicy: "auto"},
	)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	listener, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	endpoint := listener.LocalAddr().String()
	_, portText, _ := net.SplitHostPort(endpoint)
	port, _ := strconv.Atoi(portText)
	_ = listener.Close()
	config["inbounds"] = []any{map[string]any{
		"type": "direct", "listen": "127.0.0.1", "listen_port": port, "network": "udp",
	}}
	config["outbounds"] = []any{map[string]any{"type": "direct", "tag": "direct"}}
	config["route"] = map[string]any{"rules": []any{map[string]any{"action": "hijack-dns"}}, "final": "direct", "default_domain_resolver": "dns-local"}
	delete(config["experimental"].(map[string]any), "clash_api")
	data, err = json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	resolver := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "udp4", endpoint)
	}}
	lookup := func(domain string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		ips, err := resolver.LookupIP(ctx, "ip4", domain)
		if err != nil || len(ips) != 1 {
			t.Fatalf("lookup %s: %v, %v", domain, ips, err)
		}
		if ipv4 := ips[0].To4(); ipv4 == nil || ipv4[0] != 198 || ipv4[1] < 18 || ipv4[1] > 19 {
			t.Fatalf("expected FakeIP, got %v", ips)
		}
		return ips[0].String()
	}
	start := func() func() {
		t.Helper()
		command := exec.Command(executable, "run", "--disable-color", "-c", path)
		var output bytes.Buffer
		command.Stdout = &output
		command.Stderr = &output
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		stopped := false
		stop := func() {
			if !stopped {
				stopped = true
				_ = command.Process.Kill()
				_ = command.Wait()
			}
		}
		t.Cleanup(stop)
		// Readiness queries use a separate name, keeping tested allocation order
		// independent of the UDP listener's startup timing.
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			_, err := resolver.LookupIP(ctx, "ip4", "ready.cache-test.example")
			cancel()
			if err == nil {
				return stop
			}
		}
		stop()
		t.Fatalf("test DNS listener did not become ready: %s", output.String())
		return stop
	}
	stop := start()
	first := lookup("first.cache-test.example")
	second := lookup("second.cache-test.example")
	if first == second {
		t.Fatal("distinct domains must have distinct FakeIPs")
	}
	// Allow sing-box's asynchronous cache writes to settle before terminating.
	time.Sleep(2 * time.Second)
	stop()
	start()
	// Reverse the allocation order so a fresh, empty pool cannot pass by luck.
	if got := lookup("second.cache-test.example"); got != second {
		t.Fatalf("second mapping lost across restart: got %s, want %s", got, second)
	}
	if got := lookup("first.cache-test.example"); got != first {
		t.Fatalf("first mapping lost across restart: got %s, want %s", got, first)
	}
}
