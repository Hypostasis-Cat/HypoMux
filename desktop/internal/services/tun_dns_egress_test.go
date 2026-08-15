package services

import (
	"errors"
	"testing"
)

func dnsEgressAdapters() []AdapterView {
	return []AdapterView{
		{ID: "Ethernet", Name: "Ethernet", Metric: 25, Selected: true, Operational: true},
		{ID: "Wi-Fi", Name: "Wi-Fi", Metric: 50, Selected: true, Operational: true},
	}
}

func TestAutomaticDNSEgressFollowsIPv4DefaultRule(t *testing.T) {
	settings := DefaultSettings()
	decision, err := resolveTUNDNSEgress(settings, dnsEgressAdapters(), []RoutingRule{
		{MatchType: MatchIP, Value: "0.0.0.0/0", Outbound: "nic_Wi-Fi"},
	}, "Ethernet", nil)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Adapter.ID != "Wi-Fi" || decision.Source != "ipv4_default_rule" {
		t.Fatalf("DNS egress did not follow the default rule: %#v", decision)
	}
}

func TestAutomaticDNSEgressPrefersIPv4WhenDefaultsConflict(t *testing.T) {
	settings := DefaultSettings()
	decision, err := resolveTUNDNSEgress(settings, dnsEgressAdapters(), []RoutingRule{
		{MatchType: MatchIP, Value: "0.0.0.0/0", Outbound: "nic_Wi-Fi"},
		{MatchType: MatchIP, Value: "::/0", Outbound: "nic_Ethernet"},
	}, "Ethernet", nil)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Adapter.ID != "Wi-Fi" || !decision.Ambiguous {
		t.Fatalf("conflicting defaults were not resolved explicitly: %#v", decision)
	}
}

func TestAutomaticDNSEgressUsesSystemDefaultBeforeMetric(t *testing.T) {
	settings := DefaultSettings()
	decision, err := resolveTUNDNSEgress(settings, dnsEgressAdapters(), nil, "Wi-Fi", nil)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Adapter.ID != "Wi-Fi" || decision.Source != "system_default_route" {
		t.Fatalf("DNS egress ignored the system default route: %#v", decision)
	}
}

func TestAutomaticDNSEgressFallsBackToLowestMetric(t *testing.T) {
	settings := DefaultSettings()
	decision, err := resolveTUNDNSEgress(
		settings, dnsEgressAdapters(), nil, "", errors.New("route table unavailable"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Adapter.ID != "Ethernet" || decision.Source != "metric_fallback" || !decision.Ambiguous {
		t.Fatalf("metric fallback was not deterministic: %#v", decision)
	}
}

func TestExplicitDNSEgressRequiresEnabledAdapter(t *testing.T) {
	settings := DefaultSettings()
	settings.DNSEgressMode = DNSEgressAdapter
	settings.DNSAdapterID = "Wi-Fi"
	decision, err := resolveTUNDNSEgress(settings, dnsEgressAdapters(), nil, "Ethernet", nil)
	if err != nil || decision.Adapter.ID != "Wi-Fi" || decision.Source != "explicit_adapter" {
		t.Fatalf("explicit adapter was not honored: decision=%#v err=%v", decision, err)
	}

	settings.DNSAdapterID = "Cellular"
	if _, err := resolveTUNDNSEgress(settings, dnsEgressAdapters(), nil, "Ethernet", nil); err == nil {
		t.Fatal("expected an unavailable explicit adapter to fail")
	}
}

func TestSystemDNSEgressRequiresSelectedDefaultAdapter(t *testing.T) {
	settings := DefaultSettings()
	settings.DNSEgressMode = DNSEgressSystem
	if _, err := resolveTUNDNSEgress(settings, dnsEgressAdapters(), nil, "Cellular", nil); err == nil {
		t.Fatal("expected an unselected system default adapter to fail")
	}
}
