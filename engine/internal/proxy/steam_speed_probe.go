package proxy

import (
	"bytes"
	"context"
	"sort"
	"time"
)

const (
	steamSpeedProbeSize   = 256 * 1024
	steamSpeedProbeBudget = 4 * 1024 * 1024
)

func (c *steamCDN) reserveSpeedProbe(amount int) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	if now.Sub(c.speedBudgetAt) >= time.Minute {
		c.speedBudgetAt = now
		c.speedBudgetBytes = 0
		c.speedBudgetAttempts = 0
	}
	if !c.enabled || c.ctx.Err() != nil || amount < 1 || amount > steamSpeedProbeSize+1 || c.speedBudgetAttempts >= 16 || c.speedBudgetBytes+amount > steamSpeedProbeBudget {
		return false
	}
	// Charge before I/O, including failed requests; toggling/reset cannot refill.
	c.speedBudgetBytes += amount
	c.speedBudgetAttempts++
	return true
}

func rotateSteamCandidates(candidates []cdnCandidate, rotation int) []cdnCandidate {
	if len(candidates) == 0 {
		return candidates
	}
	offset := rotation % len(candidates)
	if offset < 0 {
		offset += len(candidates)
	}
	result := make([]cdnCandidate, 0, len(candidates))
	result = append(result, candidates[offset:]...)
	return append(result, candidates[:offset]...)
}

func steamExploreBefore(a, b *SteamCDNEntry, now time.Time) bool {
	if a.Selections != b.Selections {
		return a.Selections < b.Selections
	}
	score := func(e *SteamCDNEntry) float64 {
		if now.Sub(e.ProbedAt) >= time.Minute || !e.ExpiresAt.After(now) {
			return 0
		}
		return e.ProbeBPS
	}
	if av, bv := score(a), score(b); av != bv {
		return av > bv
	}
	return a.IP < b.IP
}

func (s *Server) measureSteamCandidates(parent context.Context, gen uint64, adapter Adapter, host, uri string, baseline []byte, total int64, candidates []cdnCandidate) {
	if total < 64*1024 || len(baseline) == 0 || len(baseline) > 4096 || len(candidates) == 0 {
		return
	}
	c := s.cdn
	c.mu.Lock()
	if !c.enabled || gen != c.generation {
		c.mu.Unlock()
		return
	}
	root := c.ctx
	now := c.now()
	type probeTarget struct {
		candidate cdnCandidate
		previous  time.Time
	}
	var targets []probeTarget
	for _, candidate := range candidates {
		e := c.entries[cdnKey{adapter.Name, host, "80", candidate.ip}]
		if e != nil && e.Validated && e.ExpiresAt.After(now) && !e.CooldownUntil.After(now) && now.Sub(e.probeAttemptAt) >= time.Minute {
			targets = append(targets, probeTarget{candidate, e.probeAttemptAt})
		}
	}
	c.mu.Unlock()
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].previous.Equal(targets[j].previous) {
			return targets[i].candidate.ip < targets[j].candidate.ip
		}
		return targets[i].previous.Before(targets[j].previous)
	})
	ctx, cancel := context.WithCancel(parent)
	stop := context.AfterFunc(root, cancel)
	defer stop()
	defer cancel()
	size := int(min(total, int64(steamSpeedProbeSize)))
	for _, target := range targets[:min(2, len(targets))] {
		if ctx.Err() != nil {
			return
		}
		key := cdnKey{adapter.Name, host, "80", target.candidate.ip}
		c.mu.Lock()
		e := c.entries[key]
		allowed := c.enabled && gen == c.generation && e != nil && e.Validated && e.ExpiresAt.After(c.now()) && !e.CooldownUntil.After(c.now())
		if allowed {
			e.probeAttemptAt = c.now()
		}
		c.mu.Unlock()
		if !allowed {
			continue
		}
		started := time.Now()
		data, err := s.probeSteamRange(ctx, adapter, host, key.ip, uri, size, total, true)
		elapsed := time.Since(started)
		stage := "http_speed_sampled"
		if err != nil {
			stage = "http_speed_probe_failed"
			if err.Error() == "http_speed_budget" {
				stage = "http_speed_budget"
			}
		} else if len(data) < len(baseline) || !bytes.Equal(data[:len(baseline)], baseline) {
			stage = "http_content_mismatch"
		}
		c.mu.Lock()
		e = c.entries[key]
		if c.enabled && gen == c.generation && e != nil && e.ExpiresAt.After(c.now()) && ctx.Err() == nil {
			if stage == "http_speed_sampled" && elapsed > 0 {
				e.ProbeBPS = float64(len(data)) / elapsed.Seconds()
				e.ProbedAt = c.now()
			}
			if stage == "http_content_mismatch" {
				e.Validated = false
				e.Preferred = false
				e.ProbeBPS = 0
				e.DecisionReason = "content_mismatch"
			}
		}
		c.mu.Unlock()
		c.note(gen, host, adapter.Name, key.ip, stage)
		if stage == "http_speed_budget" {
			return
		}
	}
}
