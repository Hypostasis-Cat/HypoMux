package services

import "testing"

func TestFreshInstallDefaultsExitOnClose(t *testing.T) {
	settings := DefaultSettings()
	if settings.CloseToTray {
		t.Fatal("fresh installs must exit directly when the main window closes")
	}
	if settings.AutoStartEngine {
		t.Fatal("fresh installs must not start acceleration automatically")
	}
}

func TestAutoStartEngineRequiresLaunchAtStartup(t *testing.T) {
	settings := DefaultSettings()
	settings.AutoStartEngine = true
	if err := validateSettings(settings); err == nil {
		t.Fatal("automatic acceleration must require launch at startup")
	}
	settings.Autostart = true
	if err := validateSettings(settings); err != nil {
		t.Fatalf("valid automatic acceleration settings were rejected: %v", err)
	}
}

func TestAutoStartEnginePreferencePersists(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	service := NewSettingsService()
	next := DefaultSettings()
	next.Autostart = true
	next.AutoStartEngine = true
	if _, err := service.Update(next); err != nil {
		t.Fatal(err)
	}
	reloaded := NewSettingsService()
	if !reloaded.settings.Autostart || !reloaded.settings.AutoStartEngine {
		t.Fatalf("automatic acceleration preference did not persist: %#v", reloaded.settings)
	}
}

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
