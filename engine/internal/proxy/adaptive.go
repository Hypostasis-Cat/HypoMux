package proxy

import (
	"context"
	"fmt"
	"math"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

const (
	StrategyRoundRobin = "round-robin"
	StrategyWeighted   = "weighted"
	StrategyAdaptive   = "adaptive-throughput"
)

func NormalizeStrategy(strategy string, weighted bool) (string, error) {
	if strategy == "" {
		if weighted {
			return StrategyWeighted, nil
		}
		return StrategyRoundRobin, nil
	}
	switch strategy {
	case StrategyRoundRobin, StrategyWeighted, StrategyAdaptive:
		return strategy, nil
	default:
		return "", fmt.Errorf("unknown scheduling strategy %q", strategy)
	}
}

type bindingKey struct {
	name, ip, ipv6 string
	index, index6  int
}

func performanceKey(a Adapter) bindingKey {
	return bindingKey{a.Name, a.SourceIP, a.SourceIPv6, a.IfIndex, a.IPv6IfIndex}
}

// States are retained by leases, never looked up by adapter name at release.
// Removing/rebinding a NIC retires the state; old transfers cannot debit a new one.
type linkPerformance struct {
	leases       map[*performanceLease]struct{}
	load         int
	bytes        atomic.Uint64
	blocked      atomic.Int64
	lastBytes    uint64
	lastBlocked  int64
	lastSample   time.Time
	exploreUntil time.Time
	rate         float64
	samples      int
	updated      time.Time
	selected     time.Time
}
type performanceTable struct {
	mu        sync.Mutex
	links     map[bindingKey]*linkPerformance
	now       func() time.Time
	decisions uint64
	reasons   map[string]uint64
}
type performanceLease struct {
	table       *performanceTable
	link        *linkPerformance
	bytes       atomic.Uint64
	lastBytes   uint64
	started     time.Time
	activeUntil time.Time
	attached    bool
	finished    bool
	counted     bool
}

func newPerformanceTable() *performanceTable {
	return &performanceTable{links: make(map[bindingKey]*linkPerformance), now: time.Now, reasons: make(map[string]uint64)}
}
func (p *performanceTable) link(a Adapter) *linkPerformance {
	k := performanceKey(a)
	l := p.links[k]
	if l == nil {
		l = &linkPerformance{leases: make(map[*performanceLease]struct{}), lastSample: p.now()}
		p.links[k] = l
	}
	return l
}
func (p *performanceTable) retain(adapters []Adapter) {
	p.mu.Lock()
	defer p.mu.Unlock()
	keep := make(map[bindingKey]bool, len(adapters))
	for _, a := range adapters {
		keep[performanceKey(a)] = true
	}
	for key := range p.links {
		if !keep[key] {
			delete(p.links, key)
		}
	}
}

// acquire is called under the scheduler lock; selecting and reserving are atomic.
// This experimental baseline uses observed aggregate service rate divided by
// projected load, NOT a claim to know capacity or causal marginal throughput.
func (p *performanceTable) acquire(candidates []Adapter, adaptive bool, fallback Adapter) (Adapter, *performanceLease) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	chosen := fallback
	exploring := false
	reason := "legacy-or-single"
	if adaptive && len(candidates) > 1 {
		p.decisions++
		best := -1.0
		allFresh := true
		for _, a := range candidates {
			l := p.link(a)
			if l.samples < 3 || now.Sub(l.updated) > 30*time.Second {
				allFresh = false
			}
		}
		// Cold/stale data: balanced reservations. Periodic least-recently-selected
		// exploration prevents a previously slower link from being starved.
		minLoad := int(^uint(0) >> 1)
		oldest := now.Add(time.Second)
		if p.decisions%20 == 0 {
			for _, a := range candidates {
				l := p.link(a)
				if !now.Before(l.exploreUntil) && l.selected.Before(oldest) {
					oldest, chosen, exploring = l.selected, a, true
				}
			}
		}
		for _, a := range candidates {
			if exploring {
				reason = "explore"
				break
			}
			l := p.link(a)
			load := l.load
			if !allFresh {
				reason = "load-fallback"
				if load < minLoad || load == minLoad && l.selected.Before(oldest) {
					minLoad, oldest, chosen = load, l.selected, a
				}
			} else {
				reason = "observed-rate"
				score := l.rate / float64(load+1)
				if score > best*1.1 || score >= best/1.1 && l.selected.Before(oldest) {
					best, oldest, chosen = score, l.selected, a
				}
			}
		}
	}
	l := p.link(chosen)
	p.reasons[reason]++
	l.selected = now
	if exploring {
		l.exploreUntil = now.Add(5 * time.Second)
	}
	lease := &performanceLease{table: p, link: l, started: now, counted: true}
	l.leases[lease] = struct{}{}
	l.load++
	return chosen, lease
}
func (l *performanceLease) attach() {
	l.table.mu.Lock()
	defer l.table.mu.Unlock()
	if !l.finished {
		l.attached = true
		l.started = l.table.now()
	}
}
func (l *performanceLease) finish() {
	if l == nil {
		return
	}
	l.table.mu.Lock()
	defer l.table.mu.Unlock()
	if l.finished {
		return
	}
	l.finished = true
	if l.counted {
		l.link.load--
		l.counted = false
	}
	delete(l.link.leases, l)
}
func (p *performanceTable) sample() {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	for _, link := range p.links {
		dt := now.Sub(link.lastSample)
		if dt <= 0 {
			continue
		}
		b, blocked := link.bytes.Load(), link.blocked.Load()
		delta := b - link.lastBytes
		rate := float64(delta) / dt.Seconds()
		valid := delta > 0
		backpressure := time.Duration(blocked-link.lastBlocked) > dt/2
		link.lastBytes, link.lastBlocked, link.lastSample = b, blocked, now
		link.load = 0
		for l := range link.leases {
			b := l.bytes.Load()
			delta := b - l.lastBytes
			if delta > 0 {
				l.activeUntil = now.Add(3 * time.Second)
			}
			l.lastBytes = b
			l.counted = !l.attached || now.Sub(l.started) < 3*time.Second || now.Before(l.activeUntil)
			if l.counted {
				link.load++
			}
		}
		// Idle/blocked windows are not evidence of a slower network.
		if valid && !backpressure {
			if link.samples == 0 {
				link.rate = rate
			} else {
				link.rate += 0.2 * (rate - link.rate)
			}
			link.rate = math.Max(0, link.rate)
			link.samples++
			link.updated = now
		}
	}
}
func (p *performanceTable) run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.sample()
		}
	}
}

type SchedulingTelemetry struct {
	Strategy    string                    `json:"strategy"`
	UDPStrategy string                    `json:"udp_strategy"`
	State       string                    `json:"state"`
	Decisions   map[string]uint64         `json:"decisions"`
	Links       []SchedulingLinkTelemetry `json:"links"`
}
type SchedulingLinkTelemetry struct {
	Name        string  `json:"name"`
	DownloadBPS float64 `json:"download_bps"`
	Load        int     `json:"load"`
	Samples     int     `json:"samples"`
	SampleAgeMS int64   `json:"sample_age_ms"`
}

func (s *scheduler) performanceSnapshot() SchedulingTelemetry {
	s.mu.Lock()
	defer s.mu.Unlock()
	strategy, _ := NormalizeStrategy(s.strategy, s.weighted)
	result := SchedulingTelemetry{Strategy: strategy, UDPStrategy: strategy, State: "legacy", Decisions: map[string]uint64{}}
	if strategy == StrategyAdaptive {
		result.UDPStrategy = StrategyRoundRobin
		result.State = "learning"
	}
	if s.performance == nil {
		return result
	}
	p := s.performance
	p.mu.Lock()
	defer p.mu.Unlock()
	for reason, count := range p.reasons {
		result.Decisions[reason] = count
	}
	ready := true
	for _, adapter := range s.adapters {
		l := p.links[performanceKey(adapter)]
		item := SchedulingLinkTelemetry{Name: adapter.Name, SampleAgeMS: -1}
		if l != nil {
			item.DownloadBPS, item.Load, item.Samples = l.rate, l.load, l.samples
			if !l.updated.IsZero() {
				item.SampleAgeMS = p.now().Sub(l.updated).Milliseconds()
			}
		}
		if item.Samples < 3 || item.SampleAgeMS < 0 || item.SampleAgeMS > 30000 {
			ready = false
		}
		result.Links = append(result.Links, item)
	}
	if strategy == StrategyAdaptive && ready {
		result.State = "observing"
	}
	return result
}

type leasedConn struct {
	net.Conn
	lease *performanceLease
}

func (c *leasedConn) Close() error { c.lease.finish(); return c.Conn.Close() }
func (c *leasedConn) CloseWrite() error {
	if half, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return half.CloseWrite()
	}
	return nil
}

type performanceWriter struct {
	writer interface{ Write([]byte) (int, error) }
	lease  *performanceLease
}

func (w performanceWriter) Write(b []byte) (int, error) {
	start := time.Now()
	n, err := w.writer.Write(b)
	w.lease.link.blocked.Add(int64(time.Since(start)))
	if n > 0 {
		w.lease.bytes.Add(uint64(n))
		w.lease.link.bytes.Add(uint64(n))
	}
	return n, err
}
