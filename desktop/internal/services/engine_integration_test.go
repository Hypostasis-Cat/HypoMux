package services

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRealProxyStartStopAndNetworkRestore(t *testing.T) {
	if os.Getenv("HYPOMUX_RUN_NETWORK_TEST") != "1" {
		t.Skip("set HYPOMUX_RUN_NETWORK_TEST=1 for the explicit Windows network lifecycle check")
	}
	enginePath := filepath.Clean(filepath.Join("..", "..", "..", "..", "hypomux-engine.exe"))
	if _, err := os.Stat(enginePath); err != nil {
		t.Skip("real hypomux-engine.exe is not available")
	}
	t.Setenv("HYPOMUX_ENGINE_PATH", enginePath)
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	settings := NewSettingsService()
	settings.mu.Lock()
	settings.settings.SOCKSPort = 18080
	settings.settings.HTTPPort = 18081
	if err := settings.saveLocked(); err != nil {
		settings.mu.Unlock()
		t.Fatal(err)
	}
	settings.mu.Unlock()

	adapters := NewAdapterService(settings)
	available, err := adapters.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(available) == 0 {
		t.Skip("no active IPv4 adapter is available")
	}
	available[0].Selected = true
	if _, err := adapters.SaveSelection("proxy", true, available); err != nil {
		t.Fatal(err)
	}
	engine := NewEngineService(settings, adapters)
	started := false
	defer func() {
		if started {
			_, _ = engine.Stop()
		}
		engine.Shutdown()
	}()
	snapshot, err := engine.Start("proxy")
	if err != nil {
		t.Fatal(err)
	}
	started = true
	if snapshot.Phase != "running" {
		t.Fatalf("start phase = %q", snapshot.Phase)
	}
	stopped, err := engine.Stop()
	started = false
	if err != nil {
		t.Fatal(err)
	}
	if stopped.Phase != "stopped" {
		t.Fatalf("stop phase = %q", stopped.Phase)
	}
	if _, err := os.Stat(proxyMarkerPath()); !os.IsNotExist(err) {
		t.Fatalf("proxy ownership marker remains after stop: %v", err)
	}
}
