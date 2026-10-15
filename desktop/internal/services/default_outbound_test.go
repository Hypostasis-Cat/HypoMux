package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultOutboundSettings(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	s := NewSettingsService()
	if s.Get().DefaultOutbound != "aggregation" {
		t.Fatal("new installs must default to aggregation")
	}
	legacyUpdate := s.Get()
	for _, outbound := range []string{"direct", "reject", "nic_adapter-one", "aggregation", " direct "} {
		saved, err := s.UpdateFields(AppSettings{DefaultOutbound: outbound}, []string{"default_outbound"})
		if err != nil {
			t.Fatal(err)
		}
		if saved.DefaultOutbound != strings.TrimSpace(outbound) || NewSettingsService().Get().DefaultOutbound != saved.DefaultOutbound {
			t.Fatalf("outbound did not survive reload: %+v", saved)
		}
	}
	legacyUpdate.DefaultOutbound = ""
	if saved, err := s.Update(legacyUpdate); err != nil || saved.DefaultOutbound != "direct" {
		t.Fatalf("old binding erased preference: %+v %v", saved, err)
	}
	before, err := os.ReadFile(s.path)
	if err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{"unknown", "system-direct", "nic_", "nic_a\x00b", "nic_" + strings.Repeat("a", 261)} {
		if _, err := s.UpdateFields(AppSettings{DefaultOutbound: invalid}, []string{"default_outbound"}); err == nil {
			t.Fatalf("invalid outbound accepted: %q", invalid)
		}
	}
	after, _ := os.ReadFile(s.path)
	if string(before) != string(after) || s.Get().DefaultOutbound != "direct" {
		t.Fatal("rejected settings changed saved state")
	}
}

func TestDefaultOutboundLegacySettings(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	path := filepath.Join(settingsDirectory(), "settings.json")
	if err := os.WriteFile(path, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	s := NewSettingsService()
	if s.StartupError() != nil || s.Get().DefaultOutbound != "aggregation" {
		t.Fatal("old settings must keep aggregation")
	}
	backup := filepath.Join(settingsDirectory(), "backup.json")
	if err := os.WriteFile(backup, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	s.migration = ConfigMigrationStatus{Applied: true, BackupPath: backup}
	if restored, err := s.RollbackLegacyMigration(); err != nil || restored.DefaultOutbound != "aggregation" {
		t.Fatalf("legacy rollback lost default: %+v %v", restored, err)
	}
	if err := os.WriteFile(path, []byte(`{"default_outbound":"unknown"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if NewSettingsService().StartupError() == nil {
		t.Fatal("invalid saved outbound must not silently change traffic routing")
	}
}

func TestDefaultOutboundRequiresSelectedOperationalAdapter(t *testing.T) {
	adapters := []AdapterView{
		{ID: "ready", Selected: true, Operational: true},
		{ID: "disabled", Operational: true},
		{ID: "offline", Selected: true},
	}
	for _, value := range []string{"", "aggregation", "direct", "reject", "nic_ready"} {
		if err := validateDefaultOutbound(value, adapters); err != nil {
			t.Errorf("%s: %v", value, err)
		}
	}
	for _, value := range []string{"nic_disabled", "nic_offline", "nic_missing", "invalid"} {
		if err := validateDefaultOutbound(value, adapters); err == nil {
			t.Errorf("unavailable outbound accepted: %s", value)
		}
	}
}

func TestDefaultOutboundPreflight(t *testing.T) {
	s := testTunService(t, tunPlatformSnapshot{PrivilegeBrokerAvailable: true, WFPReady: true})
	if _, err := s.settings.UpdateFields(AppSettings{DefaultOutbound: "nic_wlan"}, []string{"default_outbound"}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := s.Preflight([]string{"ethernet"})
	if err != nil || snapshot.Ready || !hasTunIssue(snapshot, "default_outbound_unavailable") {
		t.Fatalf("missing default adapter was not blocked: %+v %v", snapshot, err)
	}
	if _, err := s.settings.UpdateFields(AppSettings{DefaultOutbound: "direct"}, []string{"default_outbound"}); err != nil {
		t.Fatal(err)
	}
	adapters, _ := s.listAdapters()
	if _, reused := s.consumeRecentPreflight(adapters[:1]); reused {
		t.Fatal("preflight with an old default exit was reused")
	}
	if snapshot, err := s.Preflight([]string{"ethernet"}); err != nil || !snapshot.Ready {
		t.Fatalf("direct default should pass preflight: %+v %v", snapshot, err)
	}
}
