package services

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"
)

func TestTUNDNSDiagnosticFailureDoesNotPreventConfiguration(t *testing.T) {
	for _, force := range []bool{false, true} {
		calls := 0
		result, diagnostic, err := prepareTUNDNS(context.Background(), AdapterView{Name: "Ethernet", Address: "192.0.2.10", DNSServers: []string{"192.168.1.1"}}, force,
			func(context.Context, string, string) (dnsResolveResult, error) {
				calls++
				return dnsResolveResult{}, errors.New("test domains blocked by unknown software")
			}, func(context.Context) (tunDNSConfiguration, error) {
				return tunDNSConfiguration{Policy: "off", LegacyServers: []string{"223.5.5.5"}}, nil
			})
		if err != nil || result.Server != "192.168.1.1:53" || result.Adapter != "Ethernet" || result.Transport != "udp" {
			t.Fatalf("force=%t result=%+v diagnostic=%v error=%v", force, result, diagnostic, err)
		}
		if force && (calls != 0 || diagnostic != nil) || !force && (calls == 0 || diagnostic == nil) {
			t.Fatalf("force=%t queries=%d diagnostic=%v", force, calls, diagnostic)
		}
	}
}

func TestForcedTUNDNSPreservesEncryptedPolicyAndRejectsMissingConfiguration(t *testing.T) {
	config := tunDNSConfiguration{Policy: "google"}
	config.DoHEndpoints = append(config.DoHEndpoints, tunDoHEndpoint{IP: "8.8.8.8", Host: "dns.google", Path: "/dns-query"})
	result, diagnostic, err := prepareTUNDNS(context.Background(), AdapterView{Name: "Ethernet", Address: "192.0.2.10"}, true, nil,
		func(context.Context) (tunDNSConfiguration, error) { return config, nil })
	if err != nil || diagnostic != nil || result.Transport != "doh" || result.Server != "dns.google@8.8.8.8:443" {
		t.Fatalf("encrypted policy lost: %+v %v %v", result, diagnostic, err)
	}
	if _, err := buildDNSUpstreamForPolicy(AdapterView{Name: "Ethernet", Address: "192.168.1.2"}, result, "google"); err != nil {
		t.Fatal(err)
	}
	config.DoHEndpoints = nil
	if _, _, err := prepareTUNDNS(context.Background(), AdapterView{}, true, nil,
		func(context.Context) (tunDNSConfiguration, error) { return config, nil }); err == nil {
		t.Fatal("force accepted an unusable DNS configuration")
	}
}

func TestTUNDNSKeepsSuccessfullyProbedUpstream(t *testing.T) {
	want := dnsResolveResult{Adapter: "Ethernet", Transport: "tcp", Server: "192.168.1.1:53"}
	got, diagnostic, err := prepareTUNDNS(context.Background(), AdapterView{Name: "Ethernet", Address: "192.0.2.10"}, false,
		func(context.Context, string, string) (dnsResolveResult, error) { return want, nil },
		func(context.Context) (tunDNSConfiguration, error) {
			t.Fatal("unexpected fallback")
			return tunDNSConfiguration{}, nil
		})
	if err != nil || diagnostic != nil || got.Server != want.Server || got.Transport != want.Transport {
		t.Fatalf("%+v %v %v", got, diagnostic, err)
	}
}

func TestForcedPreflightDoesNotVetoEnvironmentAndInvalidatesCache(t *testing.T) {
	s := testTunService(t, tunPlatformSnapshot{PrivilegeBrokerAvailable: true, DefaultRouteAliases: []string{"Unknown VPN"}, RouteScanError: "denied"})
	snapshot, err := s.Preflight([]string{"ethernet"})
	if err != nil || !snapshot.Ready || !hasTunIssue(snapshot, "foreign_tun") {
		t.Fatalf("normal startup should allow route overlap: %+v %v", snapshot, err)
	}
	s.settings.mu.Lock()
	s.settings.settings.ForceTUNBypass = true
	s.settings.mu.Unlock()
	adapters, _ := s.listAdapters()
	if _, reused := s.consumeRecentPreflight(adapters[:1]); reused {
		t.Fatal("force setting change reused normal preflight")
	}
	snapshot, _ = s.Preflight([]string{"ethernet"})
	if !snapshot.Ready || !hasTunIssue(snapshot, "force_start") {
		t.Fatalf("forced start blocked: %+v", snapshot)
	}
	for _, issue := range snapshot.Issues {
		if issue.Level != "info" {
			t.Fatalf("forced environment raised gate: %+v", issue)
		}
	}
	s.resolveEngine = func() (string, error) { return "", errors.New("missing executable") }
	snapshot, _ = s.Preflight([]string{"ethernet"})
	if snapshot.Ready || !hasTunIssue(snapshot, "engine_missing") {
		t.Fatal("force hid missing core")
	}
}

func TestForcedAddressInspectionUsesKnownAllocationsWhenScanFails(t *testing.T) {
	for _, ipv6 := range []bool{false, true} {
		prefix := netip.MustParsePrefix("172.19.0.0/24")
		if ipv6 {
			prefix = netip.MustParsePrefix("fdfe:dcba:9876::/64")
		}
		inspect := func(bool) ([]netip.Prefix, error) {
			return []netip.Prefix{prefix}, errors.New("foreign driver inspection failed")
		}
		if _, err := inspectedTUNAddress(ipv6, false, inspect); err == nil {
			t.Fatal("normal inspection failure hidden")
		}
		address, err := inspectedTUNAddress(ipv6, true, inspect)
		if err != nil || prefix.Overlaps(netip.MustParsePrefix(address)) {
			t.Fatalf("forced address=%s error=%v", address, err)
		}
	}
}

func TestRepeatedConnectivityFailuresRemainAdvisoryAndRecover(t *testing.T) {
	s := &EngineService{}
	for i := 0; i < 5; i++ {
		if notice := s.recordTUNConnectivityOutcome(errors.New("blocked test site")); !strings.Contains(notice, "实际访问") {
			t.Fatal("missing actionable advisory", notice)
		}
	}
	if notice := s.recordTUNConnectivityOutcome(nil); notice != "" {
		t.Fatal("recovered probe kept old warning")
	}
}
