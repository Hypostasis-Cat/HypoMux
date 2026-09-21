package proxy

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Hypostasis-Cat/HypoMux/engine/internal/diagnostic"
)

func latencyFixture() (*latencyTable, []Adapter, *time.Time) {
	p := newLatencyTable()
	now := time.Now()
	p.now = func() time.Time { return now }
	return p, []Adapter{{Name: "a", SourceIP: "127.0.0.1", Weight: 1}, {Name: "b", SourceIP: "127.0.0.2", Weight: 1}}, &now
}

func samples(p *latencyTable, a Adapter, target string, rtt, count int) {
	for range count {
		r := diagnostic.Result{Sent: 1}
		if rtt >= 0 {
			r.Received, r.AvgLatencyMS = 1, rtt
		}
		p.record(a, target, r)
	}
}

func TestLatencyTargetOverridesReferenceAndHoldDown(t *testing.T) {
	p, a, now := latencyFixture()
	target := "203.0.113.1"
	samples(p, a[0], latencyReferences[0], 10, 3)
	samples(p, a[1], latencyReferences[0], 90, 3)
	if got := p.selectAdapter(a, target); got.Name != "a" {
		t.Fatal(got)
	}
	samples(p, a[0], target, 80, 3)
	samples(p, a[1], target, 20, 3)
	if got := p.selectAdapter(a, target); got.Name != "b" {
		t.Fatal(got)
	}
	// Even a large instantaneous win cannot churn the incumbent during hold-down.
	samples(p, a[0], target, 1, 40)
	if got := p.selectAdapter(a, target); got.Name != "b" {
		t.Fatal("hold-down bypassed", got)
	}
	*now = now.Add(11 * time.Second)
	samples(p, a[0], target, 1, 3)
	samples(p, a[1], target, 20, 3)
	if got := p.selectAdapter(a, target); got.Name != "a" {
		t.Fatal("did not switch after sustained win", got)
	}
}

func TestLatencyFailureEvidenceAndStaleSamples(t *testing.T) {
	p, a, now := latencyFixture()
	target := "203.0.113.2"
	samples(p, a[0], target, -1, 4)
	samples(p, a[1], target, 15, 4)
	if p.failedAgainst(a[0], a[1], target) {
		t.Fatal("never-responsive target caused migration")
	}
	samples(p, a[0], target, 10, 3)
	samples(p, a[0], target, -1, 3)
	if !p.failedAgainst(a[0], a[1], target) {
		t.Fatal("comparative outage ignored")
	}
	if got := p.selectAdapter(a, target); got.Name != "b" {
		t.Fatal("failed path selected", got)
	}
	*now = now.Add(latencyFreshness + time.Second)
	if p.failedAgainst(a[0], a[1], target) {
		t.Fatal("stale data caused migration")
	}
	if got := p.selectAdapter(a, target); got.Name != "a" {
		t.Fatal("stale scores used", got)
	}
}

func TestLatencyAllBlockedAndBindingIsolation(t *testing.T) {
	p, a, _ := latencyFixture()
	for _, target := range latencyReferences {
		for _, adapter := range a {
			samples(p, adapter, target, -1, 3)
		}
	}
	for range 5 {
		if got := p.selectAdapter(a, "203.0.113.5"); got.Name != "a" {
			t.Fatal(got)
		}
	}
	if p.failedAgainst(a[0], a[1], "203.0.113.5") {
		t.Fatal("blocked ICMP caused failover")
	}
	samples(p, a[1], latencyReferences[0], 5, 3)
	a[1].SourceIP = "127.0.0.3"
	if got := p.selectAdapter(a, "203.0.113.5"); got.Name != "a" {
		t.Fatal("rebound NIC inherited measurement")
	}
}

func TestLatencyPenalizesLossAndJitter(t *testing.T) {
	t.Run("loss", func(t *testing.T) {
		p, a, _ := latencyFixture()
		target := "203.0.113.9"
		samples(p, a[0], target, 5, 5)
		samples(p, a[0], target, -1, 1)
		samples(p, a[1], target, 40, 5)
		if got := p.selectAdapter(a, target); got.Name != "b" {
			t.Fatal("preferred low RTT with high loss", got)
		}
	})
	t.Run("jitter", func(t *testing.T) {
		p, a, _ := latencyFixture()
		target := "203.0.113.9"
		for range 6 {
			samples(p, a[0], target, 5, 1)
			samples(p, a[0], target, 100, 1)
		}
		samples(p, a[1], target, 65, 5)
		if got := p.selectAdapter(a, target); got.Name != "b" {
			t.Fatal("ignored jitter", got)
		}
	})
}

func TestLatencySchedulerRespectsExclusionsAndIPv6(t *testing.T) {
	p, a, _ := latencyFixture()
	s := newScheduler(a, false)
	s.strategy, s.latency = StrategyLatency, p
	samples(p, a[0], latencyReferences[0], 90, 3)
	samples(p, a[1], latencyReferences[0], 10, 3)
	if got, _ := s.selectForTarget(nil, "203.0.113.5:7777"); got.Name != "b" {
		t.Fatal(got)
	}
	if got, _ := s.selectForTarget(map[string]struct{}{"b": {}}, "203.0.113.5:7777"); got.Name != "a" {
		t.Fatal(got)
	}
	if _, ok := s.selectForTarget(map[string]struct{}{"a": {}, "b": {}}, "203.0.113.5:7777"); ok {
		t.Fatal("ignored exclusions")
	}
	if got := p.selectAdapter(a, "2001:db8::1"); got.Name != "a" {
		t.Fatal("used IPv4 RTT for IPv6")
	}
	if got := p.selectAdapter(a, "[2001:db8::1]:7777"); got.Name != "a" {
		t.Fatal("used IPv4 RTT for UDP IPv6")
	}
}

func TestLatencyProbeLifecycleBoundedAndPruned(t *testing.T) {
	p, a, now := latencyFixture()
	var calls atomic.Int64
	p.probe = func(ctx context.Context, c diagnostic.Config) diagnostic.Result {
		calls.Add(1)
		if c.SourceIP == "" || c.Count != 1 || c.Timeout > time.Second {
			t.Error("unbound or unbounded probe", c)
		}
		return diagnostic.Result{Sent: 1, Received: 1, AvgLatencyMS: 20}
	}
	for i := 1; i <= 50; i++ {
		p.watch(net.IPv4(203, 0, 113, byte(i)).String())
	}
	if len(p.targets) != latencyTargetLimit {
		t.Fatal("target cap exceeded")
	}
	p.round(context.Background(), a)
	if calls.Load() != int64((latencyTargetLimit+2)*len(a)) {
		t.Fatal(calls.Load())
	}
	*now = now.Add(latencyTargetTTL + time.Second)
	p.round(context.Background(), a[:1])
	if len(p.targets) != 0 || len(p.values) != 2 {
		t.Fatal("stale bindings/targets retained")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	before := calls.Load()
	p.round(ctx, a)
	if calls.Load() != before {
		t.Fatal("probed after shutdown")
	}
}

func TestLatencyUDPRebuildsSilentFlowAndKeepsControl(t *testing.T) {
	echoAddress, _, stopEcho := startUDPEchoServer(t)
	defer stopEcho()
	s := newTUNPoolTestServer(t)
	s.scheduler.strategy = StrategyLatency
	p := s.scheduler.latency
	p.probe = func(context.Context, diagnostic.Config) diagnostic.Result { return diagnostic.Result{} }
	endpoints, err := s.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer stopServer(t, s)
	control, relay := startUDPAssociation(t, endpoints.Channels[ChannelAggregation], 0)
	defer control.Close()
	client := listenUDPClient(t)
	defer client.Close()
	a := s.scheduler.snapshot().Adapters
	host := latencyHost(echoAddress)
	samples(p, a[0], host, 10, 3)
	samples(p, a[1], host, 30, 3)
	sendSOCKSUDP(t, client, relay, echoAddress, []byte("before"))
	if string(readSOCKSUDP(t, client)) != "before" {
		t.Fatal("initial echo")
	}
	samples(p, a[0], host, -1, 3)
	sendSOCKSUDP(t, client, relay, echoAddress, []byte("still-responsive"))
	if string(readSOCKSUDP(t, client)) != "still-responsive" {
		t.Fatal("responsive flow interrupted")
	}
	for _, c := range s.Snapshot(true).Connections {
		if c.Protocol == "socks5_udp" && c.Adapter != a[0].Name {
			t.Fatal("ICMP loss overrode live game replies")
		}
	}
	// Keep the same SOCKS control/UDP relay while the current path becomes silent.
	// The silence guard uses real packet timestamps, so wait just past its bound.
	time.Sleep(3100 * time.Millisecond)
	samples(p, a[0], host, -1, 3)
	samples(p, a[1], host, 30, 3)
	sendSOCKSUDP(t, client, relay, echoAddress, []byte("after"))
	if string(readSOCKSUDP(t, client)) != "after" {
		t.Fatal("replacement echo")
	}
	for _, c := range s.Snapshot(true).Connections {
		if c.Protocol == "socks5_udp" {
			if c.Adapter != a[1].Name {
				t.Fatal("flow did not migrate", c.Adapter)
			}
			return
		}
	}
	t.Fatal("replacement missing")
}
