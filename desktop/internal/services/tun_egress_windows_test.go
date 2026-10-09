//go:build windows

package services

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	xproxy "golang.org/x/net/proxy"
)

func TestTunSystemDirectBindingIsIndependentOfDNSAndAggregation(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	physical := AdapterView{Name: "Ethernet", Address: "192.0.2.10", SourceIPv6: "2001:db8::10"}
	for _, policy := range []string{"auto", "system"} {
		t.Run(policy, func(t *testing.T) {
			executable, path, _, err := writeSingBoxConfigWithOptions(
				map[string]string{"nic_ethernet": "127.0.0.1:19101", "nic_wifi": "127.0.0.1:19102", "aggregation": "127.0.0.1:19103"},
				AdapterView{Name: "WLAN", Address: "192.0.2.20"},
				dnsResolveResult{Transport: "udp", Server: "1.1.1.1"}, nil, compatibilityPlan{}, true,
				tunConfigOptions{DNSPolicy: policy, SystemDirectAdapter: &physical},
			)
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var config struct {
				Outbounds []map[string]any `json:"outbounds"`
				DNS       struct {
					Servers []map[string]any `json:"servers"`
				} `json:"dns"`
			}
			if err := json.Unmarshal(data, &config); err != nil {
				t.Fatal(err)
			}
			found := false
			for _, outbound := range config.Outbounds {
				if outbound["tag"] == "system-direct" {
					found = true
					if outbound["bind_interface"] != physical.Name || outbound["inet4_bind_address"] != physical.Address || outbound["inet6_bind_address"] != physical.SourceIPv6 {
						t.Fatalf("self-process bypass lost physical binding: %#v", outbound)
					}
				} else if outbound["type"] == "socks" && (outbound["bind_interface"] != nil || outbound["inet4_bind_address"] != nil || outbound["inet6_bind_address"] != nil) {
					t.Fatalf("physical binding leaked into loopback SOCKS channel: %#v", outbound)
				}
			}
			if !found {
				t.Fatal("missing self-process bypass")
			}
			if policy == "auto" && config.DNS.Servers[0]["bind_interface"] != "WLAN" {
				t.Fatal("custom DNS egress was replaced by system-direct egress")
			}
			checkSingBoxConfig(t, executable, path)
		})
	}
}

// Exercise the actual bundled core, without creating a TUN or changing routes.
// An unusable default interface simulates a stale/wrong automatic uplink. The
// explicit physical binding must still carry an HTTP response over real sockets.
func TestSystemDirectUsesPinnedInterfaceInsteadOfDefault(t *testing.T) {
	interfaces, err := net.Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	physical := AdapterView{}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}
		addresses, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, address := range addresses {
			ip, _, err := net.ParseCIDR(address.String())
			if err == nil && ip.To4() != nil && !ip.IsLinkLocalUnicast() {
				physical = AdapterView{Name: iface.Name, Address: ip.String()}
				break
			}
		}
		if physical.Name != "" {
			break
		}
	}
	if physical.Name == "" {
		t.Skip("no active non-loopback IPv4 interface for local socket binding test")
	}
	executable, err := resolveRuntimeAsset("sing-box.exe")
	if err != nil {
		t.Fatal(err)
	}
	origin := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "pinned-egress-ok")
	}))
	origin.Listener.Close()
	origin.Listener, err = net.Listen("tcp4", net.JoinHostPort(physical.Address, "0"))
	if err != nil {
		t.Fatal(err)
	}
	origin.Start()
	defer origin.Close()
	for _, pinned := range []bool{false, true} {
		name := "unbound-control"
		if pinned {
			name = "pinned-egress"
		}
		t.Run(name, func(t *testing.T) {
			listener, err := net.Listen("tcp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			endpoint := listener.Addr().String()
			port := listener.Addr().(*net.TCPAddr).Port
			listener.Close()
			outbound := map[string]any{"type": "direct", "tag": "system-direct"}
			if pinned {
				outbound = boundSystemDirectOutbound(physical)
			}
			config := map[string]any{
				"inbounds":  []any{map[string]any{"type": "socks", "listen": "127.0.0.1", "listen_port": port}},
				"outbounds": []any{outbound},
				"route":     map[string]any{"default_interface": "HypoMux-nonexistent-uplink", "final": "system-direct"},
			}
			data, err := json.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "egress.json")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, executable, "run", "-c", path)
			configureBackgroundCommand(command)
			var output bytes.Buffer
			command.Stdout, command.Stderr = &output, &output
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			stopped := false
			stop := func() {
				if !stopped {
					cancel()
					_ = command.Wait()
					stopped = true
				}
			}
			defer stop()
			for {
				connection, dialErr := net.DialTimeout("tcp4", endpoint, 100*time.Millisecond)
				if dialErr == nil {
					connection.Close()
					break
				}
				if ctx.Err() != nil {
					stop()
					t.Fatalf("core did not start: %s", output.String())
				}
				time.Sleep(50 * time.Millisecond)
			}
			dialer, err := xproxy.SOCKS5("tcp", endpoint, nil, &net.Dialer{Timeout: time.Second})
			if err != nil {
				t.Fatal(err)
			}
			transport := &http.Transport{DialContext: dialer.(xproxy.ContextDialer).DialContext}
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport, Timeout: time.Second}
			var lastErr error
			for ctx.Err() == nil {
				response, requestErr := client.Get(origin.URL)
				lastErr = requestErr
				if !pinned {
					if response != nil {
						response.Body.Close()
					}
					stop()
					if requestErr == nil || !strings.Contains(output.String(), "no such network interface") {
						t.Fatalf("unbound control did not fail on the incorrect default interface: %v %s", requestErr, output.String())
					}
					return
				}
				if requestErr == nil {
					body, readErr := io.ReadAll(response.Body)
					response.Body.Close()
					if readErr != nil || strings.TrimSpace(string(body)) != "pinned-egress-ok" {
						t.Fatalf("response: %s %v", body, readErr)
					}
					return
				}
				time.Sleep(50 * time.Millisecond)
			}
			t.Fatalf("bound core did not forward HTTP: %v", lastErr)
		})
	}
}
