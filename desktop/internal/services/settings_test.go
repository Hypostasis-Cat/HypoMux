package services

import (
	"os"
	"path/filepath"
	"testing"
)

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

func TestSettingsUpdateKeepsMemoryUnchangedWhenDiskCommitFails(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	service := NewSettingsService()
	before := service.Get()
	occupiedPath := filepath.Join(t.TempDir(), "settings.json")
	if err := os.Mkdir(occupiedPath, 0o755); err != nil {
		t.Fatal(err)
	}
	service.path = occupiedPath
	next := cloneSettings(before)
	next.Language = "en"

	if _, err := service.Update(next); err == nil {
		t.Fatal("disk commit failure was ignored")
	}
	if after := service.Get(); after.Language != before.Language {
		t.Fatalf("memory changed after failed commit: before=%q after=%q", before.Language, after.Language)
	}
}

func TestSettingsLoadFailureBlocksOverwrite(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("HYPOMUX_DATA_DIR", directory)
	path := filepath.Join(directory, "settings.json")
	corrupt := []byte(`{"mode":`)
	if err := os.WriteFile(path, corrupt, 0o600); err != nil {
		t.Fatal(err)
	}
	service := NewSettingsService()
	if service.StartupError() == nil {
		t.Fatal("corrupt settings were silently accepted")
	}
	next := service.Get()
	next.Language = "en"
	if _, err := service.Update(next); err == nil {
		t.Fatal("update overwrote settings after load failure")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(corrupt) {
		t.Fatalf("corrupt source file was changed: %q", after)
	}
}

func TestSetAutostartRollsBackSystemStateWhenDiskCommitFails(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	service := NewSettingsService()
	occupiedPath := filepath.Join(t.TempDir(), "settings.json")
	if err := os.Mkdir(occupiedPath, 0o755); err != nil {
		t.Fatal(err)
	}
	service.path = occupiedPath
	systemEnabled := false
	var changes []bool
	service.autostartEnabled = func() (bool, error) { return systemEnabled, nil }
	service.setAutostart = func(enabled bool) error {
		systemEnabled = enabled
		changes = append(changes, enabled)
		return nil
	}

	if _, err := service.SetAutostart(true); err == nil {
		t.Fatal("disk commit failure was ignored")
	}
	if systemEnabled || len(changes) != 2 || !changes[0] || changes[1] {
		t.Fatalf("autostart was not compensated: enabled=%t changes=%v", systemEnabled, changes)
	}
	if service.settings.Autostart {
		t.Fatal("memory changed after compensated autostart failure")
	}
}
