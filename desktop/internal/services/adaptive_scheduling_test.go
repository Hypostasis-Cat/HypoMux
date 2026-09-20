package services

import (
	"github.com/Hypostasis-Cat/HypoMux/desktop/internal/engineclient"
	"testing"
)

func TestAdaptiveSchedulingPersistenceAndLegacySelection(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	settings := NewSettingsService()
	adapters := NewAdapterService(settings)
	s := &EngineService{settings: settings, adapters: adapters, client: engineclient.New(), lifecycleGate: make(chan struct{}, 1)}
	if _, err := s.SaveScheduling("proxy", "adaptive-throughput", nil); err != nil {
		t.Fatal(err)
	}
	loaded := NewSettingsService().Get()
	if loaded.Strategy != "adaptive-throughput" || loaded.Weighted {
		t.Fatal("strategy not persisted", loaded.Strategy)
	}
	if _, err := s.SaveScheduling("proxy", "unknown", nil); err == nil {
		t.Fatal("accepted unknown strategy")
	}
	if effectiveSchedulingStrategy(settings.Get()) != "adaptive-throughput" {
		t.Fatal("invalid save changed settings")
	}
	if _, err := s.saveRuntimeSelection("proxy", false, nil); err != nil {
		t.Fatal(err)
	}
	if effectiveSchedulingStrategy(settings.Get()) != "round-robin" {
		t.Fatal("legacy false unexpectedly enables adaptive")
	}
}

func TestAdaptiveModePersistencePreservesStrategy(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	settings := NewSettingsService()
	for _, mode := range []string{"proxy", "tun"} {
		value, err := settings.updateHomeStrategy(mode, false, nil, nil, "adaptive-throughput")
		if err != nil || value.Strategy != "adaptive-throughput" || value.Weighted {
			t.Fatal(value, err)
		}
	}
}
