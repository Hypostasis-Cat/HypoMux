package services

import (
	"context"
	"net"
	"net/netip"
	"strings"
	"testing"
)

func TestIPv6OnlyTUNDNSAndSourceBinding(t *testing.T) {
	adapter := AdapterView{Name: "v6", SourceIPv6: "2001:db8::1", IPv6IfIndex: 7, DNSServers: []string{"fe80::1%7"}}
	got, err := configuredTUNDNS(tunDNSConfiguration{Policy: "auto", LegacyServers: []string{"223.5.5.5"}}, adapter)
	if err != nil || got.Server != "[fe80::1%7]:53" || got.Transport != "udp" {
		t.Fatalf("DNS64 upstream=%+v error=%v", got, err)
	}
	upstream, err := buildDNSUpstream(adapter, got)
	if err != nil || upstream["inet6_bind_address"] != adapter.SourceIPv6 || upstream["bind_interface"] != adapter.Name {
		t.Fatalf("lost source binding=%+v error=%v", upstream, err)
	}
	if _, exists := upstream["inet4_bind_address"]; exists {
		t.Fatal("IPv6-only DNS added an IPv4 binding")
	}
}

func TestIPv6OnlyDiagnosticsUseIPv6SourceAndMetadata(t *testing.T) {
	probe := &fakeDiagnosticProbe{icmp: icmpProbeResult{Sent: 1, Received: 1, Status: "available"}, tcpOK: true}
	service := newTestDiagnostics(t, probe)
	adapter := AdapterView{ID: "v6", Name: "v6", SourceIPv6: "2001:db8::1", IPv6IfIndex: 7, IPv6Gateway: "fe80::1%7", IPv6Metric: 25, Operational: true}
	service.listAdapters = func() ([]AdapterView, error) { return []AdapterView{adapter}, nil }
	result := service.runAdapter(context.Background(), adapter)
	if result.Address != adapter.SourceIPv6 || result.TargetIP != diagnosticTargetIPv6 || result.Checks[1].Detail != adapter.IPv6Gateway || result.Checks[2].Key != "dns" {
		t.Fatalf("IPv4 assumptions remain: %+v", result)
	}
	snapshot, err := service.Run([]string{"v6"})
	if err != nil || len(snapshot.Results) != 1 || snapshot.TargetIP != diagnosticTargetIPv6 {
		t.Fatalf("IPv6-only adapter was skipped: %+v %v", snapshot, err)
	}
}

func TestIPv6SettingsDNSAddress(t *testing.T) {
	settings := DefaultSettings()
	settings.DNSServer = "2001:4860:4860::8888"
	if err := validateSettings(settings); err != nil {
		t.Fatal(err)
	}
}

func TestIPv6OnlyForcedDNSPodUsesBoundProviderBootstrap(t *testing.T) {
	adapter := AdapterView{Name: "v6", SourceIPv6: "2001:db8::1", IPv6IfIndex: 7}
	result, diagnosticErr, err := prepareTUNDNS(context.Background(), adapter, true, func(_ context.Context, _ string, name string) (dnsResolveResult, error) {
		if name != adapter.Name {
			t.Fatal("provider bootstrap lost selected adapter")
		}
		return dnsResolveResult{Adapter: name, Transport: "doh", Server: "doh.pub@[2001:db8::53]:443"}, nil
	}, func(context.Context) (tunDNSConfiguration, error) { return tunDNSConfiguration{Policy: "dnspod"}, nil })
	if err != nil || diagnosticErr != nil || result.Server != "doh.pub@[2001:db8::53]:443" {
		t.Fatalf("bootstrap=%+v error=%v diagnostic=%v", result, err, diagnosticErr)
	}
}

func TestIPv4TUNFallbackNoticeSurvivesRuntimeAndConnectivityReasons(t *testing.T) {
	for _, reason := range []string{"tun listeners ready", "提示：外部联网探测未通过。"} {
		got := withTUNFallbackNotice(reason, true)
		if !strings.HasPrefix(got, "提示：") || !strings.Contains(got, "仅接管 IPv4") || !strings.Contains(got, reason) {
			t.Fatal("fallback was invisible to the frontend", got)
		}
	}
	if got := withTUNFallbackNotice("stopped", false); got != "stopped" {
		t.Fatal(got)
	}
}

func TestIPv6DefaultDNSUsesRouteAliasAndExcludesOwnTUN(t *testing.T) {
	routes := []networkRoute{
		{Prefix: netip.MustParsePrefix("::/0"), InterfaceIndex: 91, Alias: "HypoMux-Tun", Connected: true, MetadataKnown: true, Metric: 1},
		{Prefix: netip.MustParsePrefix("::/0"), InterfaceIndex: 92, Alias: "IPv6-only uplink", Connected: true, MetadataKnown: true, Metric: 5},
		{Prefix: netip.MustParsePrefix("::/0"), InterfaceIndex: 7, Alias: "backup", Connected: true, MetadataKnown: true, Metric: 50},
	}
	if name, ok := defaultDNSRouteAdapter(routes); !ok || name != "IPv6-only uplink" {
		t.Fatal("IPv6 route was resolved through an IPv4 index", name, ok)
	}
	if _, ok := defaultDNSRouteAdapter(routes[:1]); ok {
		t.Fatal("own TUN became its DNS uplink")
	}
}

func TestIPv6ConnectivityExcludesFakeIPRangeAndKeepsRealULA(t *testing.T) {
	for _, address := range []string{"fc00::1", "fc00:3fff::1", "198.18.0.1"} {
		if !isConnectivityFakeIP(net.ParseIP(address)) {
			t.Fatal("FakeIP became a real connectivity target", address)
		}
	}
	for _, address := range []string{"fc00:4000::1", "fd00::1", "2001:db8::1"} {
		if isConnectivityFakeIP(net.ParseIP(address)) {
			t.Fatal("real IPv6 target was filtered", address)
		}
	}
}
