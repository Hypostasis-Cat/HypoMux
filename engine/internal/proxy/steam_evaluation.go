package proxy

import (
	"context"
	"time"
)

type steamLoadSample struct {
	samples, bytes uint64
	bps            float64
	at             time.Time
}

func (c *steamCDN) saveLoadLocked(e *SteamCDNEntry) {
	if e.loadSamples == nil {
		e.loadSamples = make(map[int]steamLoadSample)
	}
	if _, ok := e.loadSamples[e.sampleLoad]; !ok && len(e.loadSamples) >= 8 {
		oldest := 0
		for load, sample := range e.loadSamples {
			if oldest == 0 || sample.at.Before(e.loadSamples[oldest].at) {
				oldest = load
			}
		}
		delete(e.loadSamples, oldest)
	}
	e.loadSamples[e.sampleLoad] = steamLoadSample{e.Samples, e.EffectiveBytes, e.DownloadBPS, e.lastSample}
}

func (c *steamCDN) runEvaluation(ctx context.Context, gen uint64) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.evaluateGeneration(gen)
		}
	}
}

func (c *steamCDN) evaluateGeneration(gen uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.enabled || gen != c.generation || c.ctx.Err() != nil {
		return
	}
	c.pruneLocked()
	now := c.now()
	for k, e := range c.entries {
		if e.baselineIP == "" {
			continue
		}
		base := c.entries[cdnKey{k.adapter, k.domain, k.port, e.baselineIP}]
		c.evaluateEntryLocked(k, e, base, now)
	}
}

func comparableSteamBaseline(now time.Time, e, base *SteamCDNEntry) *SteamCDNEntry {
	if base == nil || e.sampleLoad == 0 || base.sampleLoad == e.sampleLoad {
		return base
	}
	sample, ok := base.loadSamples[e.sampleLoad]
	if !ok || now.Sub(sample.at) >= 10*time.Second {
		return nil
	}
	copy := *base
	copy.Samples, copy.EffectiveBytes, copy.DownloadBPS, copy.lastSample = sample.samples, sample.bytes, sample.bps, sample.at
	copy.sampleLoad = e.sampleLoad
	return &copy
}

func (c *steamCDN) evaluateEntryLocked(k cdnKey, e, base *SteamCDNEntry, now time.Time) {
	if !e.Validated || !e.ExpiresAt.After(now) {
		e.Preferred = false
		return
	}
	if state := c.failures[steamFailureKey(k)]; state != nil && state.until.After(now) {
		e.Preferred = false
		e.DecisionReason = "route_paused"
		return
	}
	if e.CooldownUntil.After(now) {
		e.Preferred = false
		return
	}
	if e.performanceCooldown {
		// A new trial must establish fresh evidence after the cooldown.
		e.performanceCooldown = false
		e.Samples, e.EffectiveBytes, e.scoredSamples = 0, 0, 0
		e.loadSamples = nil
		e.slowSince = time.Time{}
		e.slowWindows = 0
	}
	window := now.Unix() / 5
	if e.lastScoreWindow == window {
		return
	}
	consecutive := e.lastScoreWindow == window-1
	e.lastScoreWindow = window
	e.EvaluatedAt = now
	base = comparableSteamBaseline(now, e, base)
	reason := steamPromotionReason(now, e, base)
	fresh := e.Samples > e.scoredSamples && now.Sub(e.lastSample) < 10*time.Second
	// Slow decisions require fresh useful samples on both paths. No-data stalls
	// and client-backpressure samples never advance this decision.
	slow := fresh && e.slowSampleEligible && e.Samples >= 5 && base != nil && base.Samples >= 3 && now.Sub(base.lastSample) < 10*time.Second && base.DownloadBPS > 0 && (e.sampleLoad == 0 || base.sampleLoad == e.sampleLoad) && e.DownloadBPS < base.DownloadBPS*0.25
	if slow {
		if e.slowWindows == 0 || !consecutive {
			e.slowSince = now
			e.slowWindows = 0
		}
		e.slowWindows++
		if e.slowWindows >= 4 && now.Sub(e.slowSince) >= 15*time.Second {
			e.CooldownUntil = now.Add(time.Minute)
			e.performanceCooldown = true
			reason = "slow_candidate"
		}
	} else {
		e.slowWindows = 0
		e.slowSince = time.Time{}
	}
	if reason == "advantage_window" {
		if !consecutive {
			e.advantageWindows = 0
		}
		e.advantageWindows++
		e.Preferred = e.advantageWindows >= 2
		if e.Preferred {
			reason = "preferred"
		}
	} else {
		e.advantageWindows = 0
		e.Preferred = false
	}
	e.scoredSamples = e.Samples
	e.DecisionReason = reason
}

func (c *steamCDN) groupTrialsLocked(adapter, host, port string) int {
	total := 0
	for k, t := range c.traffic {
		if k.adapter == adapter && k.domain == host && k.port == port {
			if e := c.entries[k]; e == nil || !e.Preferred {
				total += t.trials
			}
		}
	}
	return total
}
