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

func TestNormalizeConfigAcceptsOptionalIPv6AndRejectsUnsafeSources(t *testing.T) {
	config, err := normalizeConfig(Config{
		Adapters: []Adapter{{
			Name:        "dual-stack",
			SourceIP:    "127.0.0.1",
			IfIndex:     11,
			SourceIPv6:  "2001:0db8::1",
			IPv6IfIndex: 12,
		}},
	})
	if err != nil {
		t.Fatalf("normalizeConfig() failed: %v", err)
	}
	adapter := config.Adapters[0]
	if adapter.SourceIPv6 != "2001:db8::1" || adapter.IPv6IfIndex != 12 {
		t.Fatalf("normalized IPv6 adapter = %#v", adapter)
	}

	for _, source := range []string{"192.0.2.1", "::", "ff02::1", "fe80::1"} {
		_, err := normalizeConfig(Config{
			Adapters: []Adapter{{
				Name:       "invalid",
				SourceIP:   "127.0.0.1",
				SourceIPv6: source,
			}},
		})
		if err == nil {
			t.Errorf("unsafe IPv6 source %q unexpectedly accepted", source)
		}
	}
}

func TestNormalizeConfigValidatesTUNChannelBoundary(t *testing.T) {
	config, err := normalizeConfig(Config{
		Adapters: []Adapter{
			{Name: "Ethernet", SourceIP: "127.0.0.1"},
			{Name: "Wi-Fi", SourceIP: "127.0.0.2"},
		},
		Channels: []Channel{
			{Name: "nic_ethernet", Port: 2001, AdapterNames: []string{"Ethernet"}},
			{Name: "nic_wifi", Port: 2002, AdapterNames: []string{"Wi-Fi"}},
			{
				Name:         "aggregation",
				Port:         2003,
				AdapterNames: []string{"Ethernet", "Wi-Fi"},
			},
		},
	})
	if err != nil {
		t.Fatalf("normalizeConfig() failed: %v", err)
	}
	if got := len(adaptersForChannel(config.Adapters, config.Channels[1])); got != 1 {
		t.Fatalf("Wi-Fi channel adapter count = %d", got)
	}

	invalid := []Config{
		{
			SOCKSPort: 10800,
			Adapters:  config.Adapters,
			Channels:  config.Channels,
		},
		{
			Adapters: config.Adapters,
			Channels: []Channel{
				{Name: "aggregation", Port: 2001, AdapterNames: []string{"missing"}},
			},
		},
		{
			Adapters: config.Adapters,
			Channels: []Channel{
				{Name: "one", Port: 2001, AdapterNames: []string{"Ethernet"}},
				{Name: "two", Port: 2001, AdapterNames: []string{"Wi-Fi"}},
			},
		},
		{
			Adapters: config.Adapters,
			Channels: []Channel{
				{Name: "empty", Port: 2001},
			},
		},
	}
	for index, candidate := range invalid {
		if _, err := normalizeConfig(candidate); err == nil {
			t.Errorf("invalid TUN channel configuration %d accepted", index)
		}
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
