package proxy

import (
	"testing"
	"time"
)

func TestNormalizeConfigValidatesMigrationBoundary(t *testing.T) {
	config, err := normalizeConfig(Config{
		SOCKSPort: 0,
		HTTPPort:  0,
		Weighted:  true,
		Adapters: []Adapter{
			{Name: "Ethernet", SourceIP: "127.0.0.1", Weight: 3},
		},
	})
	if err != nil {
		t.Fatalf("normalizeConfig() failed: %v", err)
	}
	if config.ListenHost != DefaultListenHost {
		t.Fatalf("listen host = %q", config.ListenHost)
	}
	if config.ConnectTimeout != 6*time.Second {
		t.Fatalf("connect timeout = %v", config.ConnectTimeout)
	}

	_, err = normalizeConfig(Config{
		ListenHost: "0.0.0.0",
		Adapters:   []Adapter{{Name: "Ethernet", SourceIP: "127.0.0.1"}},
	})
	if err == nil {
		t.Fatal("non-loopback listener unexpectedly accepted")
	}

	_, err = normalizeConfig(Config{
		SOCKSPort: 10800,
		HTTPPort:  10800,
		Adapters:  []Adapter{{Name: "Ethernet", SourceIP: "127.0.0.1"}},
	})
	if err == nil {
		t.Fatal("duplicate listener ports unexpectedly accepted")
	}
}

func TestSchedulerRoundRobinWeightedAndExclusion(t *testing.T) {
	adapters := []Adapter{
		{Name: "a", SourceIP: "127.0.0.1", Weight: 2},
		{Name: "b", SourceIP: "127.0.0.2", Weight: 1},
	}
	roundRobin := newScheduler(adapters, false)
	first, _ := roundRobin.Select(nil)
	second, _ := roundRobin.Select(nil)
	if first.Name != "a" || second.Name != "b" {
		t.Fatalf("round robin = %s, %s", first.Name, second.Name)
	}
	onlyB, ok := roundRobin.Select(map[string]struct{}{"a": {}})
	if !ok || onlyB.Name != "b" {
		t.Fatalf("excluded selection = %#v, %v", onlyB, ok)
	}

	weighted := newScheduler(adapters, true)
	counts := map[string]int{}
	for range 6 {
		selected, _ := weighted.Select(nil)
		counts[selected.Name]++
	}
	if counts["a"] != 4 || counts["b"] != 2 {
		t.Fatalf("weighted distribution = %#v", counts)
	}
}
