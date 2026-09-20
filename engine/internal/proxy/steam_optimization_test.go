package proxy

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestSteamWarmStartUsesFreshBaselineWithoutWaitingEightConnections(t *testing.T) {
	c, k, base, now := evaluationFixture(t)
	seedCDN(c, "a", "80", "5.6.7.9")
	c.decisions[steamFailureKey(k)] = 0
	for range 3 {
		c.observe(base, c.generation, 1<<20, time.Second)
	}
	if ip, _ := c.useTrial(k.adapter, k.domain, k.port, base.ip); ip != k.ip {
		t.Fatalf("fresh baseline did not start a verified trial: %q", ip)
	}
	if ip, _ := c.useTrial(k.adapter, k.domain, k.port, base.ip); ip != "" {
		t.Fatal("warm start admitted another concurrent trial")
	}
	c.releaseTrial(k, c.generation)
	*now = now.Add(11 * time.Second)
	if ip, _ := c.useTrial(k.adapter, k.domain, k.port, base.ip); ip != "" {
		t.Fatal("stale baseline enabled accelerated exploration")
	}
}

func TestSteamPreferredKeepsOriginalControlConnections(t *testing.T) {
	c, k, base, now := evaluationFixture(t)
	for range 5 {
		c.observe(k, c.generation, 4<<20, time.Second)
		c.observe(base, c.generation, 1<<20, time.Second)
	}
	c.evaluateGeneration(c.generation)
	*now = now.Add(5 * time.Second)
	c.observe(k, c.generation, 4<<20, time.Second)
	c.observe(base, c.generation, 1<<20, time.Second)
	c.evaluateGeneration(c.generation)
	if !c.entries[k].Preferred {
		t.Fatal("fixture was not promoted")
	}
	c.decisions[steamFailureKey(k)] = 0
	originals := 0
	for range 16 {
		ip, gen := c.useTrial(k.adapter, k.domain, k.port, base.ip)
		if ip == "" {
			originals++
		} else {
			c.releaseTrial(k, gen)
		}
	}
	if originals != 2 {
		t.Fatalf("wanted two original control connections, got %d", originals)
	}
}

func TestSteamPauseAndFailuresDiscardOldPerformanceEvidence(t *testing.T) {
	for _, action := range []string{"pause", "dial_failure", "transfer_failure"} {
		t.Run(action, func(t *testing.T) {
			c, k, _, now := evaluationFixture(t)
			for range 8 {
				c.observe(k, c.generation, 8<<20, time.Second)
			}
			c.entries[k].Preferred = true
			switch action {
			case "pause":
				*now = now.Add(11 * time.Second)
			case "dial_failure":
				c.outcome(k, c.generation, false)
			case "transfer_failure":
				c.finishTransfer(k, c.generation, 0, true)
			}
			c.observe(k, c.generation, 1<<20, time.Second)
			e := c.entries[k]
			if e.Samples != 1 || e.EffectiveBytes != 1<<20 || e.DownloadBPS != 1<<20 || e.Preferred {
				t.Fatalf("old high-speed evidence contaminated recovery: %+v", e)
			}
			if !e.Validated {
				t.Fatal("performance reset discarded content validation")
			}
		})
	}
}

func TestSteamModerateRegressionNeedsSustainedEvidence(t *testing.T) {
	for _, ratio := range []float64{0.6, 0.9} {
		c, k, base, now := evaluationFixture(t)
		for range 5 {
			c.observe(k, c.generation, uint64(ratio*(4<<20)), time.Second)
			c.observe(base, c.generation, 4<<20, time.Second)
		}
		for window := 0; window < 4; window++ {
			if window > 0 {
				*now = now.Add(5 * time.Second)
			}
			c.observe(k, c.generation, uint64(ratio*(4<<20)), time.Second)
			c.observe(base, c.generation, 4<<20, time.Second)
			c.evaluateGeneration(c.generation)
			if window < 3 && c.entries[k].performanceCooldown {
				t.Fatal("transient regression was cooled prematurely")
			}
		}
		if c.entries[k].performanceCooldown != (ratio == 0.6) {
			t.Fatalf("unexpected sustained regression decision for ratio %v", ratio)
		}
	}
}

func TestSteamParallelValidationAdmitsHealthyCandidateBeforeSlowTimeout(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Range", "bytes 0-3/4")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = io.WriteString(w, "data")
	}))
	defer origin.Close()
	s, err := New(Config{SteamCDNEnabled: true, Adapters: []Adapter{{Name: "a", SourceIP: "127.0.0.1"}}})
	if err != nil {
		t.Fatal(err)
	}
	s.cdn = newSteamCDN(context.Background(), true)
	defer s.cdn.cancel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var active, peak atomic.Int32
	s.dialTCP = func(ctx context.Context, _ *net.Dialer, target string) (net.Conn, error) {
		n := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
		}
		if target == "5.6.7.8:80" {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return (&net.Dialer{}).DialContext(ctx, "tcp", origin.Listener.Addr().String())
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.validateObservedSteam(ctx, s.cdn.generation, s.config.Adapters[0], testSteamHost, "1.2.3.4", testSteamChunk, []byte("data"), 4, []cdnCandidate{{"5.6.7.8", time.Now().Add(time.Minute)}, {"5.6.7.9", time.Now().Add(time.Minute)}})
	}()
	deadline := time.Now().Add(time.Second)
	found := false
	for time.Now().Before(deadline) {
		for _, e := range s.cdn.snapshot().Entries {
			found = found || e.IP == "5.6.7.9" && e.Validated
		}
		if found {
			break
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done
	if !found {
		t.Fatal("healthy candidate was blocked behind a slow candidate")
	}
	if peak.Load() > 2 {
		t.Fatal("validation concurrency exceeded two")
	}
}
