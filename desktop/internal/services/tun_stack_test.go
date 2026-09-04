package services

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestTUNStackSettingsRoundTrip(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	service := NewSettingsService()
	if service.settings.TUNStack != "system" {
		t.Fatal("fresh installs must retain the system stack")
	}
	for _, stack := range []string{"system", "mixed", "gvisor", " MIXED ", ""} {
		next := DefaultSettings()
		next.TUNStack = stack
		saved, err := service.Update(next)
		if err != nil {
			t.Fatal(err)
		}
		want, _ := normalizeTunStack(stack)
		if saved.TUNStack != want || NewSettingsService().settings.TUNStack != want {
			t.Fatalf("stack %q was not normalized and persisted as %q", stack, want)
		}
	}
	before, _ := os.ReadFile(service.path)
	next := DefaultSettings()
	next.TUNStack = "invalid"
	if _, err := service.Update(next); err == nil {
		t.Fatal("invalid stack must be rejected")
	}
	after, _ := os.ReadFile(service.path)
	if string(before) != string(after) {
		t.Fatal("invalid stack modified the saved settings")
	}
}

func TestTUNStackLegacySettingsAndRollback(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	fields := map[string]any{}
	data, _ := json.Marshal(DefaultSettings())
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	delete(fields, "tun_stack")
	legacy, _ := json.Marshal(fields)
	path := filepath.Join(settingsDirectory(), "settings.json")
	if err := os.WriteFile(path, legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	service := NewSettingsService()
	if service.StartupError() != nil || service.settings.TUNStack != "system" {
		t.Fatal("settings without tun_stack must load as system")
	}
	backup := filepath.Join(settingsDirectory(), "backup.json")
	if err := os.WriteFile(backup, legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	service.migration = ConfigMigrationStatus{Applied: true, BackupPath: backup}
	rolledBack, err := service.RollbackLegacyMigration()
	if err != nil || rolledBack.TUNStack != "system" {
		t.Fatalf("legacy rollback = %#v, %v", rolledBack, err)
	}
}
