package proxy

import (
	"fmt"
	"math"
	"testing"
	"time"
)

func TestAdaptiveMultiLinkFloorAndLegacyIsolation(t *testing.T) {
	p, _, now := adaptiveFixture()
	var adapters []Adapter
	for i := 0; i < 8; i++ {
		a := Adapter{Name: fmt.Sprint(i), SourceIP: fmt.Sprintf("127.0.0.%d", i+1), Weight: i + 1}
		adapters = append(adapters, a)
		l := p.link(a)
		l.rate = 1
		l.windowRate = 1
		l.windowTransfers = 2
		if i == 0 {
			l.rate = 1000
			l.windowRate = 1000
		}
	}
	p.allocation.reset(adapters, *now)
	for range 100 {
		*now = now.Add(time.Second)
		p.allocation.allocations += 32
		p.allocation.observe(p.links, *now)
	}
	counts := make(map[string]int)
	for range 8000 {
		counts[p.allocation.choose(adapters, adapters[0], *now).Name]++
	}
	for _, a := range adapters {
		if counts[a.Name] < 499 {
			t.Fatal("starved candidate", a.Name, counts)
		}
	}
	for _, weighted := range []bool{false, true} {
		withModel := newScheduler(adapters, weighted)
		withModel.performance = p
		baseline := newScheduler(adapters, weighted)
		for i := 0; i < 100; i++ {
			want, _ := baseline.Select(nil)
			got, l, _ := withModel.acquireTCP(nil, "")
			if got.Name != want.Name {
				t.Fatal("changed legacy selection", weighted, i)
			}
			l.finish()
		}
	}
}

func TestAdaptiveSharesBoundedAndGuarded(t *testing.T) {
	p, adapters, now := adaptiveFixture()
	p.allocation.reset(adapters, *now)
	for i, a := range adapters {
		l := p.link(a)
		l.windowTransfers = 4
		l.windowRate = 100 / float64(1+19*i)
		l.rate = l.windowRate
	}
	for second := 1; second <= 60; second++ {
		*now = now.Add(time.Second)
		for range 8 {
			p.allocation.choose(adapters, adapters[second%2], *now)
		}
		before := p.allocation.shares[0]
		p.allocation.observe(p.links, *now)
		if math.Abs(p.allocation.shares[0]-before) > 0.050001 {
			t.Fatal("abrupt allocation change")
		}
	}
	if p.allocation.state != "adapting" || p.allocation.shares[0] <= 0.6 {
		t.Fatal("never adapted", p.allocation)
	}
	counts := map[string]int{}
	for range 400 {
		counts[p.allocation.choose(adapters, adapters[0], *now).Name]++
	}
	if counts["slow"] < 100 || counts["fast"] <= 200 {
		t.Fatal("share floor or adaptation failed", counts)
	}
	// Even a gradual capacity loss must trip the aggregate throughput guard.
	for _, l := range p.links {
		l.windowRate *= 0.5
		l.rate *= 0.5
	}
	for range 5 {
		*now = now.Add(time.Second)
		p.allocation.observe(p.links, *now)
	}
	if p.allocation.state != "throughput-guard" || p.allocation.shares[0] != 0.5 {
		t.Fatal("missing throughput guard", p.allocation)
	}
	for range 20 {
		*now = now.Add(time.Second)
		p.allocation.allocations += 8
		p.allocation.observe(p.links, *now)
	}
	if p.allocation.state != "throughput-guard" {
		t.Fatal("guard did not hold cooldown")
	}
}

func TestAdaptiveRequiresDemandAndConnectionTurnover(t *testing.T) {
	p, adapters, now := adaptiveFixture()
	p.allocation.reset(adapters, *now)
	for i, a := range adapters {
		l := p.link(a)
		l.windowTransfers = 2
		l.windowRate = 100 / float64(i+1)
		l.rate = l.windowRate
	}
	for range 40 {
		*now = now.Add(time.Second)
		p.allocation.observe(p.links, *now)
	}
	if p.allocation.state == "adapting" {
		t.Fatal("adapted without natural arrivals")
	}
	p.link(adapters[1]).windowBlocked = true
	p.allocation.allocations = 100
	p.allocation.observe(p.links, *now)
	if p.allocation.state != "insufficient-demand" || p.allocation.reference != 0 {
		t.Fatal("learned from blocked client")
	}
}

// The environment below determines delivered bytes independently of scheduler
// predictions: two rate-limited links, per-stream source limits and an optional
// startup bottleneck. No Internet or WeGame-specific assumptions are required.
type allocationSimulationResult struct {
	bytes    float64
	perLink  [2]float64
	adapting int
}

func simulateAllocation(t *testing.T, strategy string, capacity [2]float64, startup bool, rotating bool) allocationSimulationResult {
	t.Helper()
	p, adapters, now := adaptiveFixture()
	s := newScheduler(adapters, false)
	s.performance = p
	if strategy == "adaptive" {
		s.strategy = StrategyAdaptive
	}
	type stream struct {
		lease     *performanceLease
		link      int
		remaining float64
	}
	var streams []*stream
	oldSelected := map[string]time.Time{}
	result := allocationSimulationResult{}
	spawn := func() {
		var chosen Adapter
		var lease *performanceLease
		if strategy == "old" {
			// Old rate/load heuristic, without periodic exploration. The startup
			// regression uses only 16 selections, before the first exploration.
			fallback, _ := s.Select(nil)
			chosen = fallback
			fresh := true
			for _, a := range adapters {
				l := p.link(a)
				if l.samples < 3 || now.Sub(l.updated) > 30*time.Second {
					fresh = false
				}
			}
			best := -1.0
			minLoad := int(^uint(0) >> 1)
			oldest := now.Add(time.Second)
			for _, a := range adapters {
				l := p.link(a)
				if fresh {
					score := l.rate / float64(l.load+1)
					if score > best*1.1 || score >= best/1.1 && oldSelected[a.Name].Before(oldest) {
						best = score
						chosen = a
						oldest = oldSelected[a.Name]
					}
				} else if l.load < minLoad || l.load == minLoad && oldSelected[a.Name].Before(oldest) {
					minLoad = l.load
					chosen = a
					oldest = oldSelected[a.Name]
				}
			}
			_, lease = p.acquire(adapters, false, chosen)
			oldSelected[chosen.Name] = *now
		} else {
			chosen, lease, _ = s.acquireTCP(nil, "")
		}
		lease.attach()
		idx := 0
		if chosen.Name == adapters[1].Name {
			idx = 1
		}
		streams = append(streams, &stream{lease: lease, link: idx, remaining: 64})
	}
	for second := 0; second < 180; second++ {
		desired := 4
		if second >= 6 {
			desired = 16
		}
		for len(streams) < desired {
			spawn()
		}
		counts := [2]int{}
		for _, f := range streams {
			counts[f.link]++
		}
		limits := capacity
		if startup && second < 15 {
			limits[1] = 2
		}
		for _, f := range streams {
			delivered := math.Min(8, limits[f.link]/float64(counts[f.link]))
			if rotating {
				delivered = math.Min(delivered, f.remaining)
				f.remaining -= delivered
			}
			amount := uint64(delivered * 1000000)
			f.lease.bytes.Add(amount)
			f.lease.link.bytes.Add(amount)
			if second >= 30 {
				result.bytes += float64(amount)
				result.perLink[f.link] += float64(amount)
			}
		}
		*now = now.Add(time.Second)
		p.sample()
		if p.allocation.state == "adapting" {
			result.adapting++
		}
		if rotating {
			alive := streams[:0]
			for _, f := range streams {
				if f.remaining <= 0 {
					f.lease.finish()
				} else {
					alive = append(alive, f)
				}
			}
			streams = alive
		}
	}
	for _, f := range streams {
		f.lease.finish()
	}
	return result
}

func TestAdaptiveDownloadModelAgainstRoundRobin(t *testing.T) {
	for _, tc := range []struct {
		name              string
		capacity          [2]float64
		startup, rotating bool
	}{
		{"startup-long-streams", [2]float64{35, 35}, true, false},
		{"symmetric-chunks", [2]float64{35, 35}, false, true},
		{"asymmetric-chunks", [2]float64{60, 10}, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rr := simulateAllocation(t, "rr", tc.capacity, tc.startup, tc.rotating)
			adaptive := simulateAllocation(t, "adaptive", tc.capacity, tc.startup, tc.rotating)
			old := simulateAllocation(t, "old", tc.capacity, tc.startup, tc.rotating)
			t.Logf("delivered MB: round-robin=%.2f old=%.2f adaptive=%.2f; adaptive link MB=%v, adapting seconds=%d", rr.bytes/1e6, old.bytes/1e6, adaptive.bytes/1e6, [2]float64{adaptive.perLink[0] / 1e6, adaptive.perLink[1] / 1e6}, adaptive.adapting)
			if adaptive.bytes < rr.bytes*0.98 {
				t.Fatal("regressed against round-robin in controlled workload")
			}
			if tc.startup && old.bytes >= rr.bytes*0.9 {
				t.Fatal("model did not reproduce old allocation regression")
			}
			if tc.name == "asymmetric-chunks" && adaptive.adapting == 0 {
				t.Fatal("adaptive selector never activated")
			}
		})
	}
}
