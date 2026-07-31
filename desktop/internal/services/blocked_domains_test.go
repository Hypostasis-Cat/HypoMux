package services

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBlockedDomainServiceReadsLegacyListAndPersistsRemovals(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("HYPOMUX_DATA_DIR", dataDir)
	settings := NewSettingsService()
	current := settings.Get()
	current.BlockedDomainBypass = true
	current.BlockedDomainExpiry = true
	if _, err := settings.Update(current); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(dataDir, "blocked_domains.json"),
		[]byte(`{"WLAN":["Example.COM.","cdn.example.com"]}`),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	service := NewBlockedDomainService(settings)
	service.now = func() time.Time { return time.Now() }
	snapshot, err := service.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Entries) != 2 || snapshot.Entries[0].Adapter != "WLAN" {
		t.Fatalf("legacy entries not loaded: %#v", snapshot)
	}
	if err := service.Remove("WLAN", "EXAMPLE.COM."); err != nil {
		t.Fatal(err)
	}
	reloaded := NewBlockedDomainService(settings)
	after, err := reloaded.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Entries) != 1 || after.Entries[0].Domain != "cdn.example.com" {
		t.Fatalf("removal did not persist: %#v", after)
	}
}
