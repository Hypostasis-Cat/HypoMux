//go:build windows

package services

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestHotspotPreferencesEncryptedRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HYPOMUX_DATA_DIR", dir)
	s := &EngineService{}
	config, err := s.HotspotPreferences()
	if err != nil || config.SSID != "HypoMux" || config.Password != "" {
		t.Fatalf("defaults: %v %v", config.SSID, err)
	}
	want := HotspotConfig{SSID: "Test hotspot", Password: "test-secret-123", Band: "5"}
	if err := saveHotspotPreferences(want); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "hotspot.dat"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(want.Password)) {
		t.Fatal("plaintext password persisted")
	}
	got, err := s.HotspotPreferences()
	if err != nil || got != want {
		t.Fatal("encrypted preferences did not round trip", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hotspot.dat"), []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.HotspotPreferences(); err == nil {
		t.Fatal("corrupt preferences accepted")
	}
}
