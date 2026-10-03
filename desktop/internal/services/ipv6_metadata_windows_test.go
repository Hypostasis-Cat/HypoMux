//go:build windows

package services

import (
	"context"
	"encoding/json"
	"golang.org/x/sys/windows"
	"net"
	"os"
	"syscall"
	"testing"
	"unsafe"
)

func TestIPv6PreferredSourceSkipsTentativeDeprecatedAndExpired(t *testing.T) {
	var first *windows.IpAdapterUnicastAddress
	for _, fixture := range []struct {
		address  string
		state    int32
		lifetime uint32
	}{
		{"2001:db8::1", windows.IpDadStateDeprecated, 100}, {"2001:db8::2", windows.IpDadStateTentative, 100},
		{"2001:db8::3", windows.IpDadStatePreferred, 0}, {"fe80::1", windows.IpDadStatePreferred, 100},
		{"fd00::1", windows.IpDadStatePreferred, 100}, {"2001:db8::5", windows.IpDadStatePreferred, 100},
	} {
		raw := new(syscall.RawSockaddrAny)
		socket := (*windows.RawSockaddrInet6)(unsafe.Pointer(raw))
		socket.Family = windows.AF_INET6
		copy(socket.Addr[:], net.ParseIP(fixture.address).To16())
		first = &windows.IpAdapterUnicastAddress{Next: first, DadState: fixture.state, ValidLifetime: fixture.lifetime, PreferredLifetime: fixture.lifetime, Address: windows.SocketAddress{Sockaddr: raw, SockaddrLength: int32(unsafe.Sizeof(windows.RawSockaddrInet6{}))}}
	}
	if got := preferredIPv6Source(first); got != "2001:db8::5" {
		t.Fatalf("preferred=%s", got)
	}
	first.Next = nil
	first.DadState = windows.IpDadStateDeprecated
	if got := preferredIPv6Source(first); got != "" {
		t.Fatalf("deprecated source=%s", got)
	}
}

func TestIPv6OnlyGeneratedTUNConfigsPassBundledCheck(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	endpoints := map[string]string{"nic_ethernet": "127.0.0.1:19101", "nic_wifi": "127.0.0.1:19102", "aggregation": "127.0.0.1:19103"}
	adapter := AdapterView{Name: "v6", SourceIPv6: "2001:db8::1", IPv6IfIndex: 7}
	for _, policy := range []string{"auto", "off", "system", "google", "dnspod"} {
		t.Run(policy, func(t *testing.T) {
			dnsResult := dnsResolveResult{Transport: "udp", Server: "[2001:db8::53]:53"}
			if policy == "google" || policy == "dnspod" {
				dnsResult.Transport = "doh"
				dnsResult.Server = "dns.google@[2001:4860:4860::8888]:443"
				if policy == "dnspod" {
					dnsResult.Server = "doh.pub@[2001:db8::53]:443"
				}
			}
			executable, path, _, err := writeSingBoxConfigWithOptions(endpoints, adapter, dnsResult, nil, compatibilityPlan{}, true, tunConfigOptions{DNSPolicy: policy, IPv6Available: true, IPv4Unavailable: true, ConfigName: policy + ".json"})
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var cfg struct {
				Route struct {
					Rules []map[string]any `json:"rules"`
				} `json:"route"`
			}
			if err = json.Unmarshal(data, &cfg); err != nil {
				t.Fatal(err)
			}
			if policy != "system" {
				found := false
				for _, rule := range cfg.Route.Rules {
					if rule["action"] == "resolve" {
						found = true
						if rule["strategy"] != "prefer_ipv6" {
							t.Fatal("IPv6-only TUN preferred IPv4")
						}
					}
				}
				if !found {
					t.Fatal("missing domain resolution")
				}
			}
			checkSingBoxConfig(t, executable, path)
		})
	}
}

func TestDesktopIPv6ICMPRealLoopback(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3e9)
	defer cancel()
	result := probeICMPv6(ctx, "::1", "::1")
	if result.Sent == 0 || result.Received != result.Sent {
		t.Fatalf("IPv6 ICMP=%+v", result)
	}
}

func TestReadOnlyWindowsDefaultDNSEgress(t *testing.T) {
	if os.Getenv("HYPOMUX_RUN_TUN_PREFLIGHT_TEST") != "1" {
		t.Skip("set HYPOMUX_RUN_TUN_PREFLIGHT_TEST=1 for read-only native default-route inspection")
	}
	name, err := systemDefaultDNSAdapterID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := net.InterfaceByName(name); err != nil {
		t.Fatalf("default route alias does not map to a real adapter: %s %v", name, err)
	}
	if isHypoMuxManagedAdapter(name) {
		t.Fatal("own TUN became default DNS egress")
	}
	t.Logf("native default DNS egress: %s", name)
}
