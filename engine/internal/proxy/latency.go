package proxy

import (
	"context"
	"math"
	"net"
	"sync"
	"time"

	"github.com/Hypostasis-Cat/HypoMux/engine/internal/diagnostic"
)

const StrategyLatency = "latency-first"

// Reference RTT is only a fallback estimate, never a game RTT measurement.
var latencyReferences = [...]string{"223.5.5.5", "1.1.1.1"}

const (
	latencyFreshness   = 8 * time.Second
	latencyTargetTTL   = 30 * time.Second
	latencyTargetLimit = 8
)

type latencySample struct {
	RTTMS     float64   `json:"rtt_ms"`
	JitterMS  float64   `json:"jitter_ms"`
	Loss      float64   `json:"probe_loss"`
	Samples   int       `json:"samples"`
	Failures  int       `json:"consecutive_failures"`
	Updated   time.Time `json:"updated_at"`
	LastReply time.Time `json:"last_reply_at"`
}

type latencyKey struct {
	binding bindingKey
	target  string
}
type latencyChoice struct {
	binding bindingKey
	since   time.Time
}
type latencyTable struct {
	mu      sync.Mutex
	values  map[latencyKey]latencySample
	targets map[string]time.Time
	choices map[string]latencyChoice
	now     func() time.Time
	probe   func(context.Context, diagnostic.Config) diagnostic.Result
	reasons map[string]uint64
}

func newLatencyTable() *latencyTable {
	return &latencyTable{values: make(map[latencyKey]latencySample), targets: make(map[string]time.Time), choices: make(map[string]latencyChoice), now: time.Now, probe: diagnostic.Run, reasons: make(map[string]uint64)}
}

func latencyHost(target string) string {
	if host, _, err := net.SplitHostPort(target); err == nil {
		target = host
	}
	ip := net.ParseIP(target)
	// The existing source-bound diagnostic supports IPv4 only. Do not apply
	// IPv4 measurements to IPv6 destinations or resolve names on the system NIC.
	if ip == nil || ip.To4() == nil || ip.IsUnspecified() || ip.IsMulticast() {
		return ""
	}
	return ip.String()
}

func (p *latencyTable) watch(target string) {
	target = latencyHost(target)
	if target == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, exists := p.targets[target]; !exists && len(p.targets) >= latencyTargetLimit {
		return
	}
	p.targets[target] = p.now()
}

func (p *latencyTable) record(a Adapter, target string, result diagnostic.Result) {
	if result.Sent == 0 {
		return
	} // Unsupported platform/cancellation is not loss.
	p.mu.Lock()
	defer p.mu.Unlock()
	k := latencyKey{performanceKey(a), target}
	v := p.values[k]
	now := p.now()
	if now.Sub(v.Updated) > latencyFreshness {
		v = latencySample{}
	}
	loss := 1.0
	if result.Received > 0 {
		loss = 0
		rtt := float64(result.AvgLatencyMS)
		if v.LastReply.IsZero() {
			v.RTTMS = rtt
		} else {
			v.JitterMS += .25 * (math.Abs(rtt-v.RTTMS) - v.JitterMS)
			v.RTTMS += .25 * (rtt - v.RTTMS)
		}
		v.LastReply, v.Failures = now, 0
	} else {
		v.Failures++
	}
	if v.Samples == 0 {
		v.Loss = loss
	} else {
		v.Loss += .25 * (loss - v.Loss)
	}
	v.Samples++
	v.Updated = now
	p.values[k] = v
}

func (v latencySample) fresh(now time.Time) bool {
	return v.Samples >= 3 && now.Sub(v.Updated) <= latencyFreshness
}
func (v latencySample) score() float64 {
	if v.LastReply.IsZero() || v.Failures >= 3 {
		return math.Inf(1)
	}
	return v.RTTMS + 2*v.JitterMS + 400*v.Loss
}

// Compare the same probe destination across NICs; never mix game and reference
// scores, or interpret arbitrary encrypted UDP packet spacing as RTT.
func (p *latencyTable) selectAdapter(candidates []Adapter, target string) Adapter {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	host := latencyHost(target)
	ipTarget := target
	if h, _, err := net.SplitHostPort(target); err == nil {
		ipTarget = h
	}
	if ip := net.ParseIP(ipTarget); ip != nil && ip.To4() == nil {
		p.reasons["unsupported-target"]++
		return candidates[0]
	}
	sources := []string{host, latencyReferences[0], latencyReferences[1]}
	for _, source := range sources {
		if source == "" {
			continue
		}
		ready := true
		best, bestScore := candidates[0], math.Inf(1)
		for _, a := range candidates {
			v := p.values[latencyKey{performanceKey(a), source}]
			if !v.fresh(now) {
				ready = false
				break
			}
			if score := v.score(); score < bestScore {
				best, bestScore = a, score
			}
		}
		if !ready || math.IsInf(bestScore, 1) {
			continue
		}
		previous := p.choices[source]
		for _, a := range candidates {
			if performanceKey(a) != previous.binding {
				continue
			}
			oldScore := p.values[latencyKey{previous.binding, source}].score()
			// A failing incumbent bypasses hold-down. Small transient wins do not.
			if !math.IsInf(oldScore, 1) && (now.Sub(previous.since) < 10*time.Second || oldScore-bestScore < math.Max(8, oldScore*.15)) {
				best = a
			}
		}
		if performanceKey(best) != previous.binding {
			p.choices[source] = latencyChoice{performanceKey(best), now}
		}
		reason := "reference-estimate"
		if source == host {
			reason = "target-icmp"
		}
		p.reasons[reason]++
		return best
	}
	p.reasons["learning-stable-fallback"]++
	return candidates[0]
}

// Only repeated comparative failures on a previously responsive target count.
// ICMP blocked everywhere, stale probes and silent applications are not faults.
func (p *latencyTable) failedAgainst(current, alternative Adapter, target string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	host := latencyHost(target)
	if host == "" {
		return false
	}
	now := p.now()
	failed := func(source string) bool {
		a := p.values[latencyKey{performanceKey(current), source}]
		b := p.values[latencyKey{performanceKey(alternative), source}]
		return a.fresh(now) && b.fresh(now) && !a.LastReply.IsZero() && a.Failures >= 3 && b.Failures == 0 && now.Sub(b.LastReply) <= 3*time.Second
	}
	return failed(host) || (failed(latencyReferences[0]) && failed(latencyReferences[1]))
}

func (s *scheduler) selectForTarget(excluded map[string]struct{}, target string) (Adapter, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.strategy != StrategyLatency || s.latency == nil {
		return s.selectLocked(excluded, "")
	}
	candidates := s.health.candidates(s.adapters, excluded, "")
	if len(candidates) == 0 {
		return Adapter{}, false
	}
	return s.latency.selectAdapter(candidates, target), true
}

func (s *scheduler) watchLatency(target string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.strategy == StrategyLatency && s.latency != nil {
		s.latency.watch(target)
	}
}

func (s *scheduler) latencyFailover(current Adapter, target string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.strategy != StrategyLatency || s.latency == nil {
		return false
	}
	ip := net.ParseIP(latencyHost(target))
	if ip == nil {
		return false
	}
	for _, other := range s.health.candidates(s.adapters, map[string]struct{}{current.Name: {}}, "") {
		if adapterSupportsNetwork(other, networkForIP("udp", ip)) && s.latency.failedAgainst(current, other, target) {
			return true
		}
	}
	return false
}

func (p *latencyTable) run(ctx context.Context, scheduler *scheduler) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		cfg := scheduler.snapshot()
		if cfg.Strategy == StrategyLatency {
			p.round(ctx, cfg.Adapters)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (p *latencyTable) round(ctx context.Context, adapters []Adapter) {
	p.mu.Lock()
	now := p.now()
	targets := append([]string(nil), latencyReferences[:]...)
	keep := map[string]bool{latencyReferences[0]: true, latencyReferences[1]: true}
	for host, seen := range p.targets {
		if now.Sub(seen) > latencyTargetTTL {
			delete(p.targets, host)
			delete(p.choices, host)
			continue
		}
		if !keep[host] {
			targets = append(targets, host)
			keep[host] = true
		}
	}
	bindings := make(map[bindingKey]bool, len(adapters))
	for _, a := range adapters {
		bindings[performanceKey(a)] = true
	}
	for k := range p.values {
		if !bindings[k.binding] || !keep[k.target] {
			delete(p.values, k)
		}
	}
	p.mu.Unlock()
	// Bounded workers and per-probe deadlines; all probes join before shutdown.
	type job struct {
		adapter Adapter
		target  string
	}
	jobs := make(chan job)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				if ctx.Err() != nil {
					continue
				}
				result := p.probe(ctx, diagnostic.Config{SourceIP: j.adapter.SourceIP, TargetIP: j.target, Count: 1, Timeout: 500 * time.Millisecond})
				if ctx.Err() == nil {
					p.record(j.adapter, j.target, result)
				}
			}
		}()
	}
	for _, target := range targets {
		for _, a := range adapters {
			select {
			case jobs <- job{a, target}:
			case <-ctx.Done():
			}
		}
	}
	close(jobs)
	wg.Wait()
}

type LatencyTelemetry struct {
	Adapter string `json:"adapter"`
	Target  string `json:"target"`
	Source  string `json:"source"`
	latencySample
}

func (p *latencyTable) snapshot(adapters []Adapter) ([]LatencyTelemetry, map[string]uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	items := make([]LatencyTelemetry, 0)
	for _, a := range adapters {
		for k, v := range p.values {
			if k.binding != performanceKey(a) {
				continue
			}
			source := "target-icmp"
			if k.target == latencyReferences[0] || k.target == latencyReferences[1] {
				source = "reference-estimate"
			}
			items = append(items, LatencyTelemetry{a.Name, k.target, source, v})
		}
	}
	reasons := make(map[string]uint64, len(p.reasons))
	for k, v := range p.reasons {
		reasons[k] = v
	}
	return items, reasons
}
