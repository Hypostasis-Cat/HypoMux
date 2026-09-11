package proxy

import (
	"context"
	"testing"
	"time"
)

func evaluationFixture(t *testing.T) (*steamCDN, cdnKey, cdnKey, *time.Time) {
	t.Helper()
	c := newSteamCDN(context.Background(), true)
	t.Cleanup(c.cancel)
	now := time.Now().Truncate(5 * time.Second)
	c.now = func() time.Time { return now }
	candidate := cdnKey{"a", testSteamHost, "80", "5.6.7.8"}
	base := cdnKey{"a", testSteamHost, "80", "1.2.3.4"}
	seedCDN(c, "a", "80", candidate.ip)
	seedCDN(c, "a", "80", base.ip)
	c.entries[candidate].baselineIP = base.ip
	c.entries[candidate].ExpiresAt = now.Add(10 * time.Minute)
	c.entries[base].ExpiresAt = now.Add(10 * time.Minute)
	return c, candidate, base, &now
}

func TestSteamPeriodicEvaluationPromotesWithoutNewConnections(t *testing.T) {
	c, k, base, now := evaluationFixture(t)
	for range 5 {
		c.observe(k, c.generation, 4<<20, time.Second)
		c.observe(base, c.generation, 1<<20, time.Second)
	}
	c.evaluateGeneration(c.generation)
	if c.entries[k].Preferred {
		t.Fatal("one window promoted")
	}
	*now = now.Add(5 * time.Second)
	c.observe(k, c.generation, 4<<20, time.Second)
	c.observe(base, c.generation, 1<<20, time.Second)
	c.evaluateGeneration(c.generation)
	if !c.entries[k].Preferred {
		t.Fatal("periodic evaluation failed to promote")
	}
	*now = now.Add(11 * time.Second)
	c.evaluateGeneration(c.generation)
	if c.entries[k].Preferred {
		t.Fatal("stale preferred node retained")
	}
	old := c.generation
	c.configure(true, true)
	c.evaluateGeneration(old)
	if len(c.entries) != 0 {
		t.Fatal("old timer repopulated reset")
	}
}

func TestSteamLoadBucketsRestoreFreshSamplesAndRemainBounded(t *testing.T) {
	c, k, _, now := evaluationFixture(t)
	c.trafficChange(k, c.generation, 1, 0, false)
	for range 5 {
		c.observe(k, c.generation, 2<<20, time.Second)
	}
	c.trafficChange(k, c.generation, 1, 0, false)
	c.observe(k, c.generation, 1<<20, time.Second)
	c.trafficChange(k, c.generation, -1, 0, false)
	c.observe(k, c.generation, 2<<20, time.Second)
	if c.entries[k].Samples != 6 || c.entries[k].DownloadBPS != 2<<20 {
		t.Fatal("fresh bucket not restored")
	}
	for range 20 {
		c.trafficChange(k, c.generation, 1, 0, false)
		c.observe(k, c.generation, 1<<20, time.Second)
	}
	if len(c.entries[k].loadSamples) > 8 {
		t.Fatal("unbounded load history")
	}
	*now = now.Add(11 * time.Second)
	c.trafficChange(k, c.generation, -1, 0, false)
	c.observe(k, c.generation, 1<<20, time.Second)
	if c.entries[k].Samples != 1 {
		t.Fatal("stale load bucket restored")
	}
}

func TestSteamSlowCandidateNeedsSustainedFreshComparableEvidence(t *testing.T) {
	for _, scenario := range []string{"slow", "paused", "different_load", "stale_baseline", "request_gap"} {
		t.Run(scenario, func(t *testing.T) {
			c, k, base, now := evaluationFixture(t)
			for range 5 {
				c.observe(k, c.generation, 128<<10, time.Second)
				c.observe(base, c.generation, 2<<20, time.Second)
			}
			for i := 0; i < 4; i++ {
				if i > 0 {
					*now = now.Add(5 * time.Second)
				}
				if scenario != "paused" {
					elapsed := time.Second
					if scenario == "request_gap" {
						elapsed = 4 * time.Second
					}
					c.observe(k, c.generation, 128<<10, elapsed)
				}
				if scenario != "stale_baseline" {
					c.observe(base, c.generation, 2<<20, time.Second)
				} else {
					c.entries[base].lastSample = now.Add(-11 * time.Second)
				}
				if scenario == "different_load" {
					c.entries[base].sampleLoad = 2
					c.entries[base].loadSamples = nil
				}
				c.evaluateGeneration(c.generation)
			}
			if c.entries[k].performanceCooldown != (scenario == "slow") {
				t.Fatalf("bad cooling decision: %s", c.entries[k].DecisionReason)
			}
			if scenario == "slow" {
				c.decisions[steamFailureKey(k)] = 7
				if ip, _ := c.useTrial(k.adapter, k.domain, k.port, base.ip); ip != "" {
					t.Fatal("slow candidate reused")
				}
				*now = now.Add(61 * time.Second)
				c.evaluateGeneration(c.generation)
				c.decisions[steamFailureKey(k)] = 7
				if ip, _ := c.useTrial(k.adapter, k.domain, k.port, base.ip); ip != k.ip {
					t.Fatal("cooldown never recovered")
				}
			}
		})
	}
}

func TestSteamGroupTrialBudgetIncludesReservationsAndExpiredNodes(t *testing.T) {
	c, k, base, _ := evaluationFixture(t)
	for _, ip := range []string{"5.6.7.9", "5.6.7.10"} {
		seedCDN(c, "a", "80", ip)
	}
	for range 2 {
		c.decisions[steamFailureKey(k)] = 7
		if ip, _ := c.useTrial("a", testSteamHost, "80", base.ip); ip == "" {
			t.Fatal("trial not admitted")
		}
	}
	c.decisions[steamFailureKey(k)] = 7
	if ip, _ := c.useTrial("a", testSteamHost, "80", base.ip); ip != "" {
		t.Fatal("group exceeded two reservations")
	}
	for key, traffic := range c.traffic {
		if traffic.trials > 0 {
			c.entries[key].Validated = false
		}
	}
	c.decisions[steamFailureKey(k)] = 7
	if ip, _ := c.useTrial("a", testSteamHost, "80", base.ip); ip != "" {
		t.Fatal("invalidated active trial lost budget")
	}
	seedCDN(c, "b", "80", "9.9.9.9")
	if ip, _ := c.useTrial("b", testSteamHost, "80", base.ip); ip == "" {
		t.Fatal("budget leaked across adapters")
	}
}

func TestSteamComparableBaselineUsesFreshMatchingLoad(t *testing.T) {
	now := time.Now()
	e := &SteamCDNEntry{sampleLoad: 1}
	base := &SteamCDNEntry{sampleLoad: 3, loadSamples: map[int]steamLoadSample{1: {samples: 5, bps: 100, at: now}}}
	got := comparableSteamBaseline(now, e, base)
	if got == nil || got.sampleLoad != 1 || got.DownloadBPS != 100 || base.sampleLoad != 3 {
		t.Fatal("load comparison corrupted baseline")
	}
	if comparableSteamBaseline(now.Add(11*time.Second), e, base) != nil {
		t.Fatal("stale baseline accepted")
	}
}

func TestSteamEvaluationTimerRunsWithoutTrafficOrPolling(t *testing.T) {
	c := newSteamCDN(context.Background(), true)
	defer c.cancel()
	k := cdnKey{"a", testSteamHost, "80", "5.6.7.8"}
	seedCDN(c, "a", "80", k.ip)
	c.mu.Lock()
	c.entries[k].baselineIP = "1.2.3.4"
	c.mu.Unlock()
	// No connections, snapshots or explicit evaluation calls during this wait.
	time.Sleep(5500 * time.Millisecond)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries[k].EvaluatedAt.IsZero() {
		t.Fatal("background timer did not evaluate")
	}
}
