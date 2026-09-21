package proxy

import (
	"math"
	"time"
)

// Bounded feedback changes allocation shares, never ranks a new connection by
// instantaneous rate/load. Half of the allocation budget remains uniform, so
// every eligible link has at least 1/(2*N) of new connections in steady state.
// All state is protected by performanceTable.mu.
type adaptiveAllocation struct {
	keys          []bindingKey
	shares        []float64
	credit        []float64
	started       time.Time
	cooldownUntil time.Time
	state         string
	allocations   int
	windowSamples int
	windowTotal   float64
	reference     float64
}

func (a *adaptiveAllocation) reset(candidates []Adapter, now time.Time) {
	*a = adaptiveAllocation{started: now, state: "warming-up"}
	for _, candidate := range candidates {
		a.keys = append(a.keys, performanceKey(candidate))
		a.shares = append(a.shares, 1/float64(len(candidates)))
		a.credit = append(a.credit, 0)
	}
}
func (a *adaptiveAllocation) matches(candidates []Adapter) bool {
	if len(candidates) != len(a.keys) {
		return false
	}
	for i, candidate := range candidates {
		if a.keys[i] != performanceKey(candidate) {
			return false
		}
	}
	return true
}
func (a *adaptiveAllocation) uniform(state string) {
	for i := range a.shares {
		a.shares[i] = 1 / float64(len(a.shares))
		a.credit[i] = 0
	}
	a.state = state
}
func (a *adaptiveAllocation) choose(candidates []Adapter, fallback Adapter, now time.Time) Adapter {
	if !a.matches(candidates) {
		a.reset(candidates, now)
	}
	a.allocations++
	if a.state != "adapting" {
		return fallback
	}
	// Smooth weighted rotation distributes a bounded share rather than letting
	// every concurrent dial chase the largest instantaneous score.
	best := 0
	for i, share := range a.shares {
		a.credit[i] += share
		if a.credit[i] > a.credit[best] {
			best = i
		}
	}
	a.credit[best]--
	return candidates[best]
}

func (a *adaptiveAllocation) observe(links map[bindingKey]*linkPerformance, now time.Time) {
	if len(a.keys) < 2 {
		return
	}
	var total float64
	for _, key := range a.keys {
		l := links[key]
		// Require multiple actually transferring streams on every candidate.
		// Startup, idle keep-alives and cumulative historical samples don't qualify.
		if l == nil || l.windowTransfers < 2 || l.windowBlocked || l.windowRate <= 0 {
			a.windowSamples, a.windowTotal, a.allocations = 0, 0, 0
			a.reference = 0
			if now.Before(a.cooldownUntil) {
				a.uniform("throughput-guard")
			} else {
				a.uniform("insufficient-demand")
			}
			return
		}
		total += l.windowRate
	}
	if now.Before(a.cooldownUntil) {
		a.uniform("throughput-guard")
		a.allocations = 0
		return
	}
	a.windowTotal += total
	a.windowSamples++
	if a.windowSamples < 5 {
		return
	}
	mean := a.windowTotal / float64(a.windowSamples)
	arrivals := a.allocations
	a.windowSamples, a.windowTotal, a.allocations = 0, 0, 0
	if now.Sub(a.started) < 15*time.Second {
		a.uniform("warming-up")
		a.reference = mean
		return
	}
	// An unchanged set of long-lived TCP streams cannot test a new allocation.
	if arrivals < max(4, len(a.keys)) {
		return
	}
	if a.reference > 0 && mean < a.reference*0.9 {
		a.uniform("throughput-guard")
		a.cooldownUntil = now.Add(30 * time.Second)
		a.reference = 0
		return
	}
	if a.reference == 0 {
		a.reference = mean
	} else {
		a.reference += 0.2 * (mean - a.reference)
	}
	var observed float64
	for _, key := range a.keys {
		observed += links[key].rate
	}
	if observed <= 0 {
		return
	}
	// Mix uniform allocation with sustained observed service. This is an
	// allocation heuristic, not a capacity estimate or proof of causal gain.
	target := make([]float64, len(a.keys))
	maxChange := 0.0
	for i, key := range a.keys {
		target[i] = 0.5/float64(len(a.keys)) + 0.5*links[key].rate/observed
		maxChange = math.Max(maxChange, math.Abs(target[i]-a.shares[i]))
	}
	if maxChange < 0.02 {
		if a.state != "adapting" {
			a.state = "balanced"
		}
		return
	} // don't chase small measurement differences
	step := math.Min(1, 0.05/maxChange)
	for i := range a.shares {
		a.shares[i] += step * (target[i] - a.shares[i])
	}
	a.state = "adapting"
}
