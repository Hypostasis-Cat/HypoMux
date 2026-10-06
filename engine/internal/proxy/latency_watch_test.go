package proxy

import (
	"net/netip"
	"sync"
	"testing"
)

func TestLatencyWatchAddrPreservesLiteralAddressFilter(t *testing.T) {
	for _, host := range []string{
		"192.0.2.1", "2001:db8::1", "::ffff:192.0.2.1", "127.0.0.1", "::1",
		"0.0.0.0", "::", "::ffff:0.0.0.0", "224.0.0.1", "ff02::1",
		"::ffff:224.0.0.1", "169.254.1.1", "fe80::1", "::ffff:169.254.1.1",
	} {
		t.Run(host, func(t *testing.T) {
			p := newLatencyTable()
			p.watchAddr(netip.MustParseAddr(host))
			want := latencyHost(host)
			if want == "" {
				if len(p.targets) != 0 {
					t.Fatalf("watched filtered address %s", host)
				}
				return
			}
			if len(p.targets) != 1 || p.targets[netip.MustParseAddr(want)].IsZero() {
				t.Fatalf("address %s was not canonicalized to %s", host, want)
			}
		})
	}
	p := newLatencyTable()
	p.watchAddr(netip.Addr{})
	if len(p.targets) != 0 {
		t.Fatal("watched an invalid address")
	}
}

func TestLatencyWatchSnapshotFollowsSchedulingChanges(t *testing.T) {
	s := newScheduler(nil, false)
	s.setStrategy(StrategyLatency)
	s.watchLatency("192.0.2.1:443") // No table has been installed yet.
	first, second := newLatencyTable(), newLatencyTable()
	s.setLatency(first)
	s.watchLatency("[::ffff:192.0.2.1]:443")
	addr := netip.MustParseAddr("192.0.2.1")
	if len(first.targets) != 1 || first.targets[addr].IsZero() {
		t.Fatal("initial table did not receive a canonical target")
	}
	s.update(SchedulingConfig{Strategy: StrategyRoundRobin})
	s.watchLatencyAddr(netip.MustParseAddr("192.0.2.2"))
	if len(first.targets) != 1 {
		t.Fatal("disabled latency strategy kept watching targets")
	}
	s.setLatency(second)
	s.update(SchedulingConfig{Strategy: StrategyLatency})
	s.watchLatencyAddr(addr)
	if len(first.targets) != 1 || len(second.targets) != 1 {
		t.Fatal("re-enabled strategy did not publish the replacement table")
	}
	s.setLatency(nil)
	s.watchLatencyAddr(addr)
	if s.latencyWatch.Load() != nil {
		t.Fatal("removed latency table remained published")
	}
}

func TestLatencyWatchConcurrentSchedulingUpdates(t *testing.T) {
	adapters := []Adapter{{Name: "a", SourceIP: "127.0.0.1", Weight: 1}}
	s := newScheduler(adapters, false)
	s.setLatency(newLatencyTable())
	var workers sync.WaitGroup
	for worker := range 4 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			addr := netip.AddrFrom4([4]byte{192, 0, 2, byte(worker + 1)})
			for range 1000 {
				s.watchLatencyAddr(addr)
				s.watchLatency("[2001:db8::1]:443")
				s.Select(nil)
			}
		}()
	}
	for i := range 1000 {
		strategy := StrategyLatency
		if i%2 == 0 {
			strategy = StrategyRoundRobin
		}
		s.update(SchedulingConfig{Strategy: strategy, Adapters: adapters})
		if i%10 == 0 {
			s.mu.Lock()
			s.setLatency(newLatencyTable())
			s.mu.Unlock()
		}
	}
	workers.Wait()
	s.update(SchedulingConfig{Strategy: StrategyLatency, Adapters: adapters})
	addr := netip.MustParseAddr("203.0.113.1")
	s.watchLatencyAddr(addr)
	if s.latency.targets[addr].IsZero() {
		t.Fatal("current snapshot stopped watching after concurrent updates")
	}
}
