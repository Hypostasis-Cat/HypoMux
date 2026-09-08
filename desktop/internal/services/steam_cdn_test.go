package services

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Hypostasis-Cat/HypoMux/desktop/internal/engineclient"
)

func TestSteamCDNPreferenceDefaultsOffAndPersistsWithoutStartingCore(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	settings := NewSettingsService()
	if settings.Get().SteamCDNEnabled {
		t.Fatal("must default off")
	}
	service := &EngineService{settings: settings, client: engineclient.New(), lifecycleGate: make(chan struct{}, 1)}
	for _, enabled := range []bool{true, false} {
		value, err := service.SetSteamCDNEnabled(enabled)
		if err != nil || value.SteamCDNEnabled != enabled {
			t.Fatal(value, err)
		}
		if NewSettingsService().Get().SteamCDNEnabled != enabled {
			t.Fatal("preference did not persist")
		}
		if service.client.Hello().ProtocolVersion != 0 {
			t.Fatal("settings must not launch privileged Core")
		}
	}
}

func TestSteamCDNFailedPersistenceKeepsPreference(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HYPOMUX_DATA_DIR", dir)
	settings := NewSettingsService()
	blocker := filepath.Join(dir, "file")
	if err := os.WriteFile(blocker, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	settings.path = filepath.Join(blocker, "settings.json")
	service := &EngineService{settings: settings, client: engineclient.New(), lifecycleGate: make(chan struct{}, 1)}
	if _, err := service.SetSteamCDNEnabled(true); err == nil {
		t.Fatal("expected persistence failure")
	}
	if settings.Get().SteamCDNEnabled {
		t.Fatal("failed save changed preference")
	}
}
