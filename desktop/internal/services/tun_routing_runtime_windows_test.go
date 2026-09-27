//go:build windows

package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/net/proxy"
)

// Run the bundled router with loopback-only SOCKS endpoints.
// No TUN interface, system proxy, external DNS or destination is used.
func TestBundledSingBoxRoutingAndHotReload(t *testing.T) {
	cases := []struct {
		name         string
		compat       bool
		rules        []RoutingRule
		target, want string
	}{
		{"ip-domain-plain", false, []RoutingRule{{MatchType: MatchIP, Value: "203.0.113.0/24", Outbound: "nic_wifi", Priority: 2}}, "audit.example:443", "nic_wifi"},
		{"ip-domain-compat", true, []RoutingRule{{MatchType: MatchIP, Value: "203.0.113.0/24", Outbound: "nic_wifi", Priority: 2}}, "audit.example:443", "nic_wifi"},
		{"ip-literal-compat", true, []RoutingRule{{MatchType: MatchIP, Value: "203.0.113.0/24", Outbound: "nic_wifi", Priority: 2}}, "203.0.113.7:443", "nic_wifi"},
		{"subdomain-specific", false, []RoutingRule{{MatchType: MatchDomain, Value: "example.com", Outbound: "nic_wifi"}, {MatchType: MatchDomain, Value: "www.example.com", Outbound: "nic_ethernet"}}, "www.example.com:443", "nic_ethernet"},
	}
	orders := [][]string{{MatchProcess, MatchDomain, MatchIP}, {MatchProcess, MatchIP, MatchDomain}, {MatchDomain, MatchProcess, MatchIP}, {MatchDomain, MatchIP, MatchProcess}, {MatchIP, MatchProcess, MatchDomain}, {MatchIP, MatchDomain, MatchProcess}}
	destinations := map[string]string{MatchProcess: "nic_ethernet", MatchDomain: "direct", MatchIP: "nic_wifi"}
	for i, order := range orders {
		for _, compat := range []bool{false, true} {
			rules := rulesWithMatchOrder([]RoutingRule{{MatchType: MatchProcess, Value: filepath.Base(os.Args[0]), Outbound: "nic_ethernet"}, {MatchType: MatchDomain, Value: "audit.example", Outbound: "direct"}, {MatchType: MatchIP, Value: "203.0.113.0/24", Outbound: "nic_wifi"}}, order)
			cases = append(cases, struct {
				name         string
				compat       bool
				rules        []RoutingRule
				target, want string
			}{fmt.Sprintf("order-%d-compat-%v", i, compat), compat, rules, "audit.example:443", destinations[order[0]]})
		}
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			directory := t.TempDir()
			t.Setenv("HYPOMUX_DATA_DIR", directory)
			compat := compatibilityPlan{}
			if tc.compat {
				compat.ProcessNames = []string{filepath.Base(os.Args[0])}
			}
			executable, path, _, err := writeSingBoxConfigWithOptions(map[string]string{"nic_ethernet": "127.0.0.1:19101", "nic_wifi": "127.0.0.1:19102", "aggregation": "127.0.0.1:19103"}, AdapterView{}, dnsResolveResult{Transport: "udp", Server: "1.1.1.1"}, tc.rules, compat, true, tunConfigOptions{DNSPolicy: "auto"})
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var config map[string]any
			if err = json.Unmarshal(data, &config); err != nil {
				t.Fatal(err)
			}
			listener, err := net.Listen("tcp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			address := listener.Addr().String()
			port := listener.Addr().(*net.TCPAddr).Port
			listener.Close()
			config["inbounds"] = []any{map[string]any{"type": "socks", "tag": "audit", "listen": "127.0.0.1", "listen_port": port}}
			config["dns"] = map[string]any{"servers": []any{map[string]any{"type": "hosts", "tag": "dns-local", "predefined": map[string]any{"audit.example": []string{"203.0.113.7"}, "www.example.com": []string{"203.0.113.7"}}}}, "final": "dns-local"}
			delete(config, "experimental")
			route := config["route"].(map[string]any)
			route["rules"] = route["rules"].([]any)[3:] // Remove sniff and self-process bypass for this local test client.
			observed := make(chan string, 16)
			for _, raw := range config["outbounds"].([]any) {
				entry := raw.(map[string]any)
				tag := entry["tag"].(string)
				endpoint, e := net.Listen("tcp4", "127.0.0.1:0")
				if e != nil {
					t.Fatal(e)
				}
				defer endpoint.Close()
				for k := range entry {
					delete(entry, k)
				}
				entry["type"] = "socks"
				entry["tag"] = tag
				entry["server"] = "127.0.0.1"
				entry["server_port"] = endpoint.Addr().(*net.TCPAddr).Port
				entry["version"] = "5"
				go func() {
					for {
						c, e := endpoint.Accept()
						if e != nil {
							return
						}
						observed <- tag
						c.Close()
					}
				}()
			}
			logPath := filepath.Join(directory, "router.log")
			config["log"] = map[string]any{"level": "debug", "output": logPath, "disabled": false}
			data, err = json.MarshalIndent(config, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, executable, "run", "-c", path)
			if err = command.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() {
				cancel()
				_ = command.Wait()
				if t.Failed() {
					logs, _ := os.ReadFile(logPath)
					t.Log(string(logs))
				}
			}()
			ready := false
			for i := 0; i < 100; i++ {
				c, e := net.DialTimeout("tcp", address, 50*time.Millisecond)
				if e == nil {
					c.Close()
					ready = true
					break
				}
				time.Sleep(25 * time.Millisecond)
			}
			if !ready {
				t.Fatal("router did not start")
			}
			dialer, err := proxy.SOCKS5("tcp", address, nil, &net.Dialer{Timeout: 3 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			assertRoute := func(want string, reloading bool) {
				t.Helper()
				deadline := time.Now().Add(3 * time.Second)
				for {
					conn, dialErr := dialer.(proxy.ContextDialer).DialContext(ctx, "tcp", tc.target)
					if conn != nil {
						conn.Close()
					}
					select {
					case got := <-observed:
						if got == want {
							return
						}
						if !reloading || !time.Now().Before(deadline) {
							t.Fatalf("target=%s selected=%s want=%s (dial=%v)", tc.target, got, want, dialErr)
						}
					case <-time.After(time.Second):
						t.Fatalf("no outbound: %v", dialErr)
					}
					// File watching is asynchronous: wait for the new generation
					// to affect a fresh connection, without assuming a fixed delay.
					time.Sleep(25 * time.Millisecond)
				}
			}
			assertRoute(tc.want, false)
			if tc.name == "subdomain-specific" {
				childDisabled := append([]RoutingRule(nil), tc.rules...)
				childDisabled[1].Disabled = true
				if err := refreshSingBoxRuleSets(childDisabled, nil); err != nil {
					t.Fatal(err)
				}
				assertRoute("nic_wifi", true)
				if err := refreshSingBoxRuleSets(tc.rules, nil); err != nil {
					t.Fatal(err)
				}
				assertRoute("nic_ethernet", true)
			}
			disabled := append([]RoutingRule(nil), tc.rules...)
			for i := range disabled {
				disabled[i].Disabled = true
			}
			if err := refreshSingBoxRuleSets(disabled, nil); err != nil {
				t.Fatal(err)
			}
			wantDisabled := "aggregation"
			if tc.compat {
				wantDisabled = "system-direct"
			}
			assertRoute(wantDisabled, true)
		})
	}
}
