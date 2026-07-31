package services

import "testing"

func TestRoutingServicePersistsCanonicalRules(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	settings := NewSettingsService()
	adapters := NewAdapterService(settings)
	service := NewRoutingRuleService(settings, adapters, nil)
	saved, err := service.Save([]RoutingRule{
		{MatchType: "domain", Value: "*.Example.COM.", Outbound: "direct"},
		{MatchType: "process_name", Value: "steam.exe", Outbound: "aggregation"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Rules) != 2 || saved.Rules[0].MatchType != MatchProcess ||
		saved.Rules[1].Value != "example.com" {
		t.Fatalf("unexpected saved rules: %#v", saved.Rules)
	}

	reloaded := NewSettingsService()
	reloadedService := NewRoutingRuleService(reloaded, NewAdapterService(reloaded), nil)
	snapshot, err := reloadedService.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Rules) != 2 || snapshot.Rules[0].Value != "steam.exe" {
		t.Fatalf("persistence did not round-trip: %#v", snapshot.Rules)
	}
}
