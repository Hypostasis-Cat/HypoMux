package proxy

import (
	"bytes"
	"context"
	"net"
	"sync"
	"time"

	"github.com/Hypostasis-Cat/HypoMux/engine/internal/dns"
)

func (s *Server) discoverObservedSteam(session *connection, adapter Adapter, uri string, baseline []byte, total int64, validUntil time.Time) {
	c := s.cdn
	k := session.cdnKey
	c.note(session.cdnGeneration, k.domain, k.adapter, k.ip, "http_reference_ready")
	c.mu.Lock()
	c.pruneLocked()
	key := adapter.Name + "/" + net.JoinHostPort(k.domain, k.port)
	if !c.enabled || c.generation != session.cdnGeneration || c.probing >= 2 || len(c.discovery) >= 64 || c.discovery[key].After(c.now()) {
		c.mu.Unlock()
		return
	}
	c.discovery[key] = c.now().Add(30 * time.Second)
	c.probing++
	gen, root := c.generation, c.ctx
	observed := c.observedCandidatesLocked(k.domain, k.port)
	c.mu.Unlock()
	go func() {
		deadline := time.Now().Add(20 * time.Second)
		if validUntil.Before(deadline) {
			deadline = validUntil
		}
		ctx, cancel := context.WithDeadline(root, deadline)
		defer cancel()
		defer func() {
			c.mu.Lock()
			if c.generation == gen {
				c.probing--
			}
			c.mu.Unlock()
		}()
		resolver, e := dns.New(ctx, s.config.DNS, s.dialDNS)
		if e != nil {
			return
		}
		candidates, sources := mergeSteamSources(s.steamCandidates(ctx, k.domain, resolver), observed)
		s.validateObservedSteam(ctx, gen, adapter, k.domain, k.ip, uri, baseline, total, candidates, sources)
	}()
}
func (s *Server) validateObservedSteam(ctx context.Context, gen uint64, adapter Adapter, host, originalIP, uri string, baseline []byte, total int64, candidates []cdnCandidate, sourceSets ...map[string]string) {
	c := s.cdn
	if len(candidates) == 0 {
		c.note(gen, host, adapter.Name, "", "dns_no_candidates")
		return
	}
	pool := steamCandidatesForAdapter(candidates, adapter, originalIP, c.now())
	passed := make([]bool, len(pool))
	validate := func(candidate cdnCandidate) bool {
		if candidate.ip == originalIP || !adapterSupportsNetwork(adapter, networkForIP("tcp", net.ParseIP(candidate.ip))) {
			return false
		}
		if ctx.Err() != nil {
			return false
		}
		sample, e := s.probeSteamHTTPRange(ctx, adapter, host, candidate.ip, uri, len(baseline), total)
		stage := "verified"
		if e != nil {
			stage = steamProbeReason(e)
		} else if !bytes.Equal(sample, baseline) {
			stage = "http_content_mismatch"
		}
		c.note(gen, host, adapter.Name, candidate.ip, stage)
		if stage != "verified" {
			if stage == "http_content_mismatch" {
				c.mu.Lock()
				if c.enabled && gen == c.generation {
					if entry := c.entries[cdnKey{adapter.Name, host, "80", candidate.ip}]; entry != nil {
						entry.Validated = false
						entry.Preferred = false
						entry.ProbeBPS = 0
						entry.DecisionReason = "content_mismatch"
					}
				}
				c.mu.Unlock()
			}
			return false
		}
		c.mu.Lock()
		admitted := false
		if c.enabled && c.generation == gen && ctx.Err() == nil && candidate.expires.After(c.now()) {
			key := cdnKey{adapter.Name, host, "80", candidate.ip}
			entry := c.entries[key]
			if entry == nil {
				if len(c.entries) >= 512 {
					c.mu.Unlock()
					return false
				}
				entry = &SteamCDNEntry{Adapter: adapter.Name, Domain: host, Port: "80", IP: candidate.ip}
				c.entries[key] = entry
			}
			entry.Validated = true
			entry.Source = "dns"
			if len(sourceSets) > 0 && sourceSets[0][candidate.ip] != "" {
				entry.Source = sourceSets[0][candidate.ip]
			}
			if entry.DecisionReason == "validation_expired" {
				entry.DecisionReason = "stale_samples"
			}
			entry.ExpiresAt = candidate.expires
			admitted = true
		}
		c.mu.Unlock()
		return admitted
	}
	// Two bounded workers let a healthy candidate become usable even while a
	// different CDN times out. Existing request/byte budgets remain shared.
	jobs := make(chan int, len(pool))
	for i := range pool {
		jobs <- i
	}
	close(jobs)
	var workers sync.WaitGroup
	for range min(2, len(pool)) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for i := range jobs {
				passed[i] = validate(pool[i])
			}
		}()
	}
	workers.Wait()
	var verified []cdnCandidate
	for i, candidate := range pool {
		if passed[i] {
			verified = append(verified, candidate)
		}
	}
	if len(pool) == 0 {
		c.note(gen, host, adapter.Name, originalIP, "only_original")
	}
	s.measureSteamCandidates(ctx, gen, adapter, host, uri, baseline, total, verified)
}

type cdnTraffic struct {
	switchedActive                         int
	switchedBytes, originalBytes, failures uint64

	slots [5]struct {
		second   int64
		bytes    uint64
		switched uint64
	}
	active int
	trials int
}

func (c *steamCDN) trafficChange(key cdnKey, gen uint64, delta int, amount uint64, trial bool, attribution ...bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.enabled || gen != c.generation {
		return
	}
	t := c.traffic[key]
	if t == nil {
		if len(c.traffic) >= 512 {
			return
		}
		t = &cdnTraffic{}
		c.traffic[key] = t
	}
	t.active += delta
	if t.active < 0 {
		t.active = 0
	}
	if trial {
		t.trials += delta
		if t.trials < 0 {
			t.trials = 0
		}
	}
	sec := c.now().Unix()
	slot := &t.slots[sec%5]
	if slot.second != sec {
		slot.second = sec
		slot.bytes = 0
		slot.switched = 0
	}
	slot.bytes += amount
	if len(attribution) > 0 {
		if attribution[0] {
			slot.switched += amount
		}
		c.accountTransferLocked(key, gen, delta, amount, attribution[0], false)
	}
}
func (c *steamCDN) useTrial(adapter, host, port, original string) (string, uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	gen := c.generation
	if !c.enabled {
		return "", gen
	}
	c.pruneLocked()
	key := adapter + "/" + host + ":" + port
	if len(c.decisions) >= 512 && c.decisions[key] == 0 {
		return "", gen
	}
	c.decisions[key]++
	explore := c.decisions[key]%8 == 0
	baseline := c.entries[cdnKey{adapter, host, port, original}]
	now := c.now()
	hasPreferred := false
	for k, e := range c.entries {
		if k.adapter != adapter || k.domain != host || k.port != port || k.ip == original || !e.Validated {
			continue
		}
		if e.baselineIP != original {
			e.advantageWindows = 0
			e.Preferred = false
		}
		e.baselineIP = original
		c.evaluateEntryLocked(k, e, baseline, now)
		hasPreferred = hasPreferred || e.Preferred
	}
	// Keep a small original-node control stream so successful selection cannot
	// starve its own baseline and oscillate between preferred and stale states.
	if hasPreferred && c.decisions[key]%8 == 7 {
		return "", gen
	}

	groupTrials := c.groupTrialsLocked(adapter, host, port)
	var selected *SteamCDNEntry
	for k, e := range c.entries {
		if k.adapter != adapter || k.domain != host || k.port != port || k.ip == original || !e.Validated || e.CooldownUntil.After(c.now()) {
			continue
		}
		if c.traffic[k] == nil && len(c.traffic) >= 512 {
			continue
		}
		if state := c.failures[steamFailureKey(k)]; state != nil && state.until.After(c.now()) {
			e.DecisionReason = "route_paused"
			continue
		}
		// Score active trials before enforcing admission limits: a long download
		// must be able to earn promotion without first closing its connection.
		if t := c.traffic[k]; t != nil && ((!e.Preferred && t.trials > 0) || (e.Preferred && t.trials >= 4)) {
			continue
		}
		if !e.Preferred && groupTrials >= 2 {
			e.AdmissionReason = "group_trial_limit"
			continue
		}
		if e.CooldownUntil.After(c.now()) {
			continue
		}
		// A fresh original baseline can start the first unused candidate without
		// waiting for eight more connections. Never bootstrap beside another trial
		// or after a preferred route has already been found.
		bootstrap := !hasPreferred && groupTrials == 0 && e.Selections == 0 && baseline != nil && baseline.Samples >= 3 && now.Sub(baseline.lastSample) < 10*time.Second
		if !explore && !e.Preferred && !bootstrap {
			continue
		}
		if selected == nil || (explore || bootstrap) && steamExploreBefore(e, selected, now) || !explore && !bootstrap && (e.DownloadBPS > selected.DownloadBPS || e.DownloadBPS == selected.DownloadBPS && e.IP < selected.IP) {
			selected = e
		}
	}
	if selected == nil {
		return "", gen
	}
	selected.Selections++
	// Reserve atomically before dialing so concurrent requests cannot exceed the trial limit.
	k := cdnKey{adapter, host, port, selected.IP}
	t := c.traffic[k]
	if t == nil {
		t = &cdnTraffic{}
		c.traffic[k] = t
	}
	t.trials++
	return selected.IP, gen
}
func (c *steamCDN) releaseTrial(key cdnKey, gen uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if gen == c.generation {
		if t := c.traffic[key]; t != nil && t.trials > 0 {
			t.trials--
		}
	}
}

func (c *steamCDN) finishTransfer(key cdnKey, gen uint64, amount uint64, failed bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.enabled || gen != c.generation {
		return
	}
	e := c.entries[key]
	if e == nil {
		return
	}
	if failed {
		resetSteamPerformance(e)
		e.CooldownUntil = c.now().Add(time.Minute)
		return
	}
	if amount >= 64*1024 {
		e.SuccessfulConnections++
	}
}

// Reasons describe the latest comparison against an original on the same NIC
// and hostname. They are not an estimate of total application speedup.
func steamPromotionReason(now time.Time, entry, baseline *SteamCDNEntry) string {
	if entry.Samples < 5 || entry.EffectiveBytes < 8*1024*1024 {
		return "insufficient_samples"
	}
	if now.Sub(entry.lastSample) >= 10*time.Second || entry.Samples <= entry.scoredSamples {
		return "stale_samples"
	}
	if baseline == nil || baseline.Samples < 3 || baseline.DownloadBPS <= 0 || now.Sub(baseline.lastSample) >= 10*time.Second {
		return "baseline_missing"
	}
	if entry.sampleLoad > 0 && baseline.sampleLoad > 0 && entry.sampleLoad != baseline.sampleLoad {
		return "load_mismatch"
	}
	if entry.DownloadBPS <= baseline.DownloadBPS*1.15 {
		return "advantage_insufficient"
	}
	return "advantage_window"
}
