package proxy

import (
	"bytes"
	"math"
	"reflect"
	"testing"
	"time"
)

func TestUDPReplyFeedsOnlyUDPAndReleasesItsLease(t *testing.T) {
	address, _, stopEcho := startUDPEchoServer(t)
	defer stopEcho()
	server := newTUNPoolTestServer(t)
	endpoints, err := server.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer stopServer(t, server)
	control, relay := startUDPAssociation(t, endpoints.Channels[ChannelAggregation], 0)
	defer control.Close()
	client := listenUDPClient(t)
	defer client.Close()
	payload := bytes.Repeat([]byte{0xa5}, 8192)
	for range 10 {
		sendSOCKSUDP(t, client, relay, address, payload)
		if !bytes.Equal(readSOCKSUDP(t, client), payload) {
			t.Fatal("UDP reply corrupted")
		}
	}
	waitForUDPFlowTelemetry(t, server, 81920, 81920, time.Second)
	server.udpPerformance.mu.Lock()
	var download uint64
	for _, link := range server.udpPerformance.links {
		download += link.bytes.Load()
	}
	server.udpPerformance.mu.Unlock()
	if download != 81920 {
		t.Fatal("UDP delivery not recorded", download)
	}
	server.performance.mu.Lock()
	tcpLinks := len(server.performance.links)
	server.performance.mu.Unlock()
	if tcpLinks != 0 {
		t.Fatal("UDP reserved TCP capacity")
	}
	_ = control.Close()
	waitForUDPFlows(t, server, 0, time.Second)
	server.udpPerformance.mu.Lock()
	defer server.udpPerformance.mu.Unlock()
	for _, link := range server.udpPerformance.links {
		if link.load != 0 || len(link.leases) != 0 {
			t.Fatal("closed UDP association retained leases")
		}
	}
}

func TestUDPAdaptiveLearnsSeparatelyFromTCPAndFallsBackWhenIdle(t *testing.T) {
	now := time.Now()
	adapters := []Adapter{{Name: "fast", SourceIP: "127.0.0.1"}, {Name: "slow", SourceIP: "127.0.0.2"}}
	s := newScheduler(adapters, false)
	s.setStrategy(StrategyAdaptive)
	s.performance, s.udpPerformance = newPerformanceTable(), newUDPPerformanceTable()
	s.performance.now = func() time.Time { return now }
	s.udpPerformance.now = func() time.Time { return now }
	var udpLeases, tcpLeases []*performanceLease
	for _, adapter := range adapters {
		_, u := s.udpPerformance.acquire(adapters, true, adapter, adapters)
		_, tcp := s.performance.acquire(adapters, true, adapter, adapters)
		u.attach()
		tcp.attach()
		udpLeases = append(udpLeases, u)
		tcpLeases = append(tcpLeases, tcp)
	}
	for range 30 {
		// Real download accounting feeds one multiplexed flow per UDP link.
		for i, lease := range udpLeases {
			bytes := uint64(1024 * 1024 / (1 + 7*i))
			lease.bytes.Add(bytes)
			lease.link.bytes.Add(bytes)
		}
		for range 4 {
			_, lease, ok := s.acquireUDP(nil, "192.0.2.1:443")
			if !ok {
				t.Fatal("no UDP candidate")
			}
			lease.finish()
		}
		now = now.Add(time.Second)
		before := s.udpPerformance.allocation.shares[0]
		s.udpPerformance.sample()
		s.performance.sample()
		if math.Abs(before-s.udpPerformance.allocation.shares[0]) > 0.050001 {
			t.Fatal("UDP allocation changed too quickly")
		}
	}
	status := s.performanceSnapshot()
	if status.UDPStrategy != StrategyAdaptive || status.UDPState != "adapting" || status.UDPLinks[0].AllocationShare <= 0.5 || status.UDPLinks[1].AllocationShare < 0.25 {
		t.Fatal("missing bounded UDP learning", status)
	}
	if status.EffectiveTCPStrategy != StrategyRoundRobin || status.Links[0].Samples != 0 || status.Links[0].DownloadBPS != 0 {
		t.Fatal("UDP feedback contaminated TCP", status)
	}
	// Keep-alives/idle time must not train a capacity estimate or retain load.
	now = now.Add(4 * time.Second)
	s.udpPerformance.sample()
	status = s.performanceSnapshot()
	if status.UDPStrategy != StrategyRoundRobin || status.UDPState != "insufficient-demand" || status.UDPLinks[0].Load != 0 {
		t.Fatal("idle UDP did not fall back", status)
	}
	for _, lease := range append(udpLeases, tcpLeases...) {
		lease.finish()
	}
	for _, link := range s.udpPerformance.links {
		if link.load != 0 || len(link.leases) != 0 {
			t.Fatal("UDP lease leaked")
		}
	}
}

func TestUDPAdaptivePreservesConstrainedPoolAndResetsOnRebinding(t *testing.T) {
	adapters := []Adapter{{Name: "a", SourceIP: "127.0.0.1"}, {Name: "b", SourceIP: "127.0.0.2"}}
	s := newScheduler(adapters, false)
	s.setStrategy(StrategyAdaptive)
	s.udpPerformance = newUDPPerformanceTable()
	p := s.udpPerformance
	p.allocation.reset(adapters, time.Now())
	p.allocation.state, p.allocation.shares = "adapting", []float64{0.7, 0.3}
	for range 10 {
		adapter, lease, ok := s.acquireUDP(map[string]struct{}{"a": {}}, "192.0.2.1:443")
		if !ok || adapter.Name != "b" {
			t.Fatal("bypassed exclusion", adapter)
		}
		lease.finish()
	}
	if p.allocation.state != "adapting" || p.allocation.allocations != 0 || !reflect.DeepEqual(p.allocation.shares, []float64{0.7, 0.3}) {
		t.Fatal("constrained retry erased or trained the pool")
	}
	adapters[0].SourceIP = "127.0.0.3"
	s.update(SchedulingConfig{Strategy: StrategyAdaptive, Adapters: adapters})
	_, lease, _ := s.acquireUDP(nil, "192.0.2.1:443")
	lease.finish()
	if p.allocation.state != "warming-up" || !p.allocation.matches(adapters) {
		t.Fatal("rebound radio retained old allocation")
	}
}

func TestUDPAdaptiveDoesNotOverrideLegacyOrLearnWithoutReplies(t *testing.T) {
	adapters := []Adapter{{Name: "a", SourceIP: "127.0.0.1", Weight: 3}, {Name: "b", SourceIP: "127.0.0.2", Weight: 1}}
	for _, weighted := range []bool{false, true} {
		baseline, observed := newScheduler(adapters, weighted), newScheduler(adapters, weighted)
		observed.udpPerformance = newUDPPerformanceTable()
		for range 20 {
			want, _ := baseline.Select(nil)
			got, lease, _ := observed.acquireUDP(nil, "192.0.2.1:443")
			lease.finish()
			if got.Name != want.Name {
				t.Fatal("changed legacy UDP selection")
			}
		}
	}
	now := time.Now()
	p := newUDPPerformanceTable()
	p.now = func() time.Time { return now }
	p.allocation.reset(adapters, now.Add(-time.Minute))
	for range 20 {
		for range 8 {
			_, lease := p.acquire(adapters, true, adapters[0], adapters)
			lease.finish() // Sending or dial success alone provides no feedback.
		}
		now = now.Add(time.Second)
		p.sample()
	}
	if p.allocation.state != "insufficient-demand" || p.allocation.shares[0] != 0.5 {
		t.Fatal("learned UDP throughput without replies")
	}
}
