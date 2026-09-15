package proxy

import (
	"reflect"
	"sync"
	"testing"
)

func TestLiveSchedulingRetainsConnectionsAndListeners(t *testing.T) {
	for _, channel := range []string{"", ChannelAggregation} {
		t.Run("channel="+channel, func(t *testing.T) {
			first := Adapter{Name: "first", SourceIP: "127.0.0.1", Weight: 1}
			second := Adapter{Name: "second", SourceIP: "127.0.0.2", Weight: 3}
			config := Config{Adapters: []Adapter{first}}
			if channel != "" {
				config.Channels = []Channel{{Name: ChannelEthernet, AdapterNames: []string{first.Name}}, {Name: ChannelWiFi, AdapterNames: []string{first.Name}}, {Name: channel, AdapterNames: []string{first.Name}}, {Name: "nic_first", AdapterNames: []string{first.Name}}}
			}
			s, err := New(config)
			if err != nil {
				t.Fatal(err)
			}
			endpoints, err := s.Start()
			if err != nil {
				t.Fatal(err)
			}
			defer stopServer(t, s)
			address, stop := startEchoServer(t)
			defer stop()
			endpoint := endpoints.SOCKS
			if channel != "" {
				endpoint = endpoints.Channels[channel]
			}
			old := dialSOCKSIPv4(t, endpoint, address, 1)
			defer old.Close()
			assertEcho(t, old, []byte("before"))
			previous, err := s.UpdateScheduling(SchedulingConfig{Weighted: true, Adapters: []Adapter{second}})
			if err != nil {
				t.Fatal(err)
			}
			if previous.Weighted || previous.Adapters[0].Name != first.Name {
				t.Fatal(previous)
			}
			assertEcho(t, old, []byte("after"))
			fresh := dialSOCKSIPv4(t, endpoint, address, 1)
			defer fresh.Close()
			assertEcho(t, fresh, []byte("new"))
			snapshot := s.Snapshot(true)
			counts := map[string]int{}
			for _, connection := range snapshot.Connections {
				counts[connection.Adapter]++
			}
			if counts[first.Name] != 1 || counts[second.Name] != 1 {
				t.Fatal(counts)
			}
			if !reflect.DeepEqual(endpoints, s.Endpoints()) || !s.Running() {
				t.Fatal("listeners changed")
			}
			if channel != "" {
				a, ok := s.schedulers["nic_first"].Select(nil)
				if !ok || a.Name != first.Name {
					t.Fatal("explicit NIC route changed", a)
				}
			}
			if _, err := s.UpdateScheduling(previous); err != nil {
				t.Fatal(err)
			}
			a, ok := s.scheduler.Select(nil)
			if !ok || a.Name != first.Name {
				t.Fatal("rollback failed", a)
			}
		})
	}
}

func TestLiveSchedulingValidationAndWeightDistribution(t *testing.T) {
	a := Adapter{Name: "a", SourceIP: "127.0.0.1", Weight: 1}
	b := Adapter{Name: "b", SourceIP: "127.0.0.2", Weight: 3}
	s, err := New(Config{Adapters: []Adapter{a, b}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Start(); err != nil {
		t.Fatal(err)
	}
	defer stopServer(t, s)
	for _, config := range []SchedulingConfig{{}, {Adapters: []Adapter{a, a}}, {Adapters: []Adapter{{Name: "bad", SourceIP: "invalid", Weight: 1}}}, {Adapters: []Adapter{{Name: "bad", SourceIP: "127.0.0.3", Weight: 101}}}} {
		before := s.scheduler.snapshot()
		if _, err = s.UpdateScheduling(config); err == nil {
			t.Fatal("accepted invalid configuration", config)
		}
		if !reflect.DeepEqual(before, s.scheduler.snapshot()) {
			t.Fatal("failed update mutated scheduler")
		}
	}
	for _, weighted := range []bool{true, false} {
		if _, err = s.UpdateScheduling(SchedulingConfig{Weighted: weighted, Adapters: []Adapter{a, b}}); err != nil {
			t.Fatal(err)
		}
		counts := map[string]int{}
		for range 40 {
			chosen, _ := s.scheduler.Select(nil)
			counts[chosen.Name]++
		}
		want := 20
		if weighted {
			want = 30
		}
		if counts["b"] != want {
			t.Fatal(weighted, counts)
		}
	}
}

func TestLiveSchedulingConcurrentSelectionAndTelemetry(t *testing.T) {
	a := Adapter{Name: "a", SourceIP: "127.0.0.1", Weight: 1}
	b := Adapter{Name: "b", SourceIP: "127.0.0.2", Weight: 2}
	s, err := New(Config{Adapters: []Adapter{a}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Start(); err != nil {
		t.Fatal(err)
	}
	defer stopServer(t, s)
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				s.scheduler.Select(nil)
				s.scheduler.snapshot()
				s.Snapshot(true)
				s.shouldTuneTCP("")
			}
		}()
	}
	for range 100 {
		if _, err = s.UpdateScheduling(SchedulingConfig{Adapters: []Adapter{b}}); err != nil {
			t.Fatal(err)
		}
		if _, err = s.UpdateScheduling(SchedulingConfig{Adapters: []Adapter{a}}); err != nil {
			t.Fatal(err)
		}
	}
	wg.Wait()
}

func TestLiveSchedulingRetainsUDPFlowsAndUpdatesNewTargets(t *testing.T) {
	s := newTUNPoolTestServer(t)
	endpoints, err := s.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer stopServer(t, s)
	first, _, stopFirst := startUDPEchoServer(t)
	defer stopFirst()
	second, _, stopSecond := startUDPEchoServer(t)
	defer stopSecond()
	control, relay := startUDPAssociation(t, endpoints.Channels[ChannelAggregation], 0)
	defer control.Close()
	client := listenUDPClient(t)
	defer client.Close()
	sendSOCKSUDP(t, client, relay, first, []byte("before"))
	if string(readSOCKSUDP(t, client)) != "before" {
		t.Fatal("initial flow failed")
	}
	_, err = s.UpdateScheduling(SchedulingConfig{Adapters: []Adapter{{Name: "new", SourceIP: "127.0.0.3", Weight: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	sendSOCKSUDP(t, client, relay, first, []byte("retained"))
	if string(readSOCKSUDP(t, client)) != "retained" {
		t.Fatal("existing flow interrupted")
	}
	sendSOCKSUDP(t, client, relay, second, []byte("new"))
	if string(readSOCKSUDP(t, client)) != "new" {
		t.Fatal("new flow failed")
	}
	bindings := map[string]string{}
	for _, flow := range s.Snapshot(true).Connections {
		if flow.Protocol == "socks5_udp" {
			bindings[flow.Target] = flow.Adapter
		}
	}
	if bindings[first] != "wired" || bindings[second] != "new" {
		t.Fatal(bindings)
	}
}
