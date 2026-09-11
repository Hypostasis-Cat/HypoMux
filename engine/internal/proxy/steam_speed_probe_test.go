package proxy

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSteamSpeedBudgetConcurrentAndReset(t *testing.T) {
	c := newSteamCDN(context.Background(), true)
	defer c.cancel()
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if c.reserveSpeedProbe(steamSpeedProbeSize + 1) {
				accepted.Add(1)
			}
		}()
	}
	wg.Wait()
	if accepted.Load() != 15 {
		t.Fatal("budget oversubscribed", accepted.Load())
	}
	before := c.snapshot().SpeedProbeBytes
	c.configure(true, true)
	if c.reserveSpeedProbe(steamSpeedProbeSize+1) || c.snapshot().SpeedProbeBytes != before {
		t.Fatal("reset refilled budget")
	}
	c.configure(false, false)
	if c.reserveSpeedProbe(1) {
		t.Fatal("disabled probe admitted")
	}
	c.configure(true, false)
	if c.reserveSpeedProbe(steamSpeedProbeSize + 1) {
		t.Fatal("toggle refilled budget")
	}
	c.mu.Lock()
	c.speedBudgetAt = time.Now().Add(-61 * time.Second)
	c.mu.Unlock()
	if !c.reserveSpeedProbe(steamSpeedProbeSize + 1) {
		t.Fatal("budget never recovered")
	}
}

func TestSteamStagedProbeRequiresContentAndDoesNotScoreDownloads(t *testing.T) {
	for _, scenario := range []string{"valid", "first_mismatch", "revalidation_mismatch", "extended_mismatch", "redirect", "truncated", "oversized", "short_chunk"} {
		t.Run(scenario, func(t *testing.T) {
			var calls atomic.Int32
			total := int64(1 << 20)
			if scenario == "short_chunk" {
				total = 4096
			}
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Host != testSteamHost || r.URL.RequestURI() != testSteamChunk || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
					t.Error("request scope changed")
				}
				end, err := strconv.Atoi(strings.TrimPrefix(r.Header.Get("Range"), "bytes=0-"))
				if err != nil {
					t.Error(err)
					return
				}
				size := end + 1
				if size != 4096 && size != steamSpeedProbeSize {
					t.Error("unexpected range", size)
				}
				if size > 4096 && scenario == "redirect" {
					w.Header().Set("Location", "http://other.invalid/token")
					w.WriteHeader(302)
					return
				}
				w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", end, total))
				w.Header().Set("Content-Length", strconv.Itoa(size))
				if scenario == "oversized" && size > 4096 {
					w.Header().Set("Content-Length", strconv.Itoa(size+1))
				}
				w.WriteHeader(206)
				value := byte('x')
				if scenario == "first_mismatch" || scenario == "revalidation_mismatch" || scenario == "extended_mismatch" && size > 4096 {
					value = 'y'
				}
				if scenario == "truncated" && size > 4096 {
					size /= 2
				}
				if scenario == "oversized" && size > 4096 {
					size++
				}
				_, _ = w.Write(bytes.Repeat([]byte{value}, size))
			}))
			defer origin.Close()
			s, err := New(Config{SteamCDNEnabled: true, Adapters: []Adapter{{Name: "a", SourceIP: "127.0.0.1"}}})
			if err != nil {
				t.Fatal(err)
			}
			s.cdn = newSteamCDN(context.Background(), true)
			defer s.cdn.cancel()
			if scenario == "revalidation_mismatch" {
				seedCDN(s.cdn, "a", "80", "5.6.7.8")
			}
			s.dialTCP = func(ctx context.Context, d *net.Dialer, target string) (net.Conn, error) {
				if d.LocalAddr.String() != "127.0.0.1:0" || target != "5.6.7.8:80" {
					t.Error("lost adapter binding", d.LocalAddr, target)
				}
				return (&net.Dialer{}).DialContext(ctx, "tcp", origin.Listener.Addr().String())
			}
			s.validateObservedSteam(context.Background(), s.cdn.generation, s.config.Adapters[0], testSteamHost, "1.2.3.4", testSteamChunk, bytes.Repeat([]byte{'x'}, 4096), total, []cdnCandidate{{"5.6.7.8", time.Now().Add(time.Minute)}})
			snap := s.cdn.snapshot()
			if snap.SwitchedBytes != 0 || snap.OriginalBytes != 0 {
				t.Fatal("probe counted as transfer")
			}
			if scenario == "first_mismatch" {
				if calls.Load() != 1 || len(snap.Entries) != 0 {
					t.Fatal("invalid candidate received speed probe")
				}
				return
			}
			e := snap.Entries[0]
			if e.Samples != 0 || e.EffectiveBytes != 0 || e.DownloadBPS != 0 || e.Preferred {
				t.Fatal("probe promoted candidate")
			}
			if (e.ProbeBPS > 0) != (scenario == "valid") {
				t.Fatal("invalid speed result", scenario, e.ProbeBPS)
			}
			if (scenario == "extended_mismatch" || scenario == "revalidation_mismatch") && e.Validated {
				t.Fatal("changed content retained eligibility")
			}
			expected := int32(2)
			if scenario == "short_chunk" || scenario == "revalidation_mismatch" {
				expected = 1
			}
			if calls.Load() != expected {
				t.Fatal("redirect followed or extra request", calls.Load())
			}
		})
	}
}

func TestSteamProbeOrderingExpiresAndDoesNotStarveUntried(t *testing.T) {
	now := time.Now()
	a := &SteamCDNEntry{IP: "5.6.7.9", ProbeBPS: 100, ProbedAt: now, ExpiresAt: now.Add(time.Hour)}
	b := &SteamCDNEntry{IP: "5.6.7.8", ProbeBPS: 10, ProbedAt: now, ExpiresAt: now.Add(time.Hour)}
	if !steamExploreBefore(a, b, now) {
		t.Fatal("fresh short test ignored")
	}
	a.Selections = 1
	if steamExploreBefore(a, b, now) {
		t.Fatal("untried candidate starved")
	}
	a.Selections = 0
	if steamExploreBefore(a, b, now.Add(61*time.Second)) {
		t.Fatal("stale probe still preferred")
	}
}

func TestSteamCandidateRotationVisitsBeyondFirstEight(t *testing.T) {
	var candidates []cdnCandidate
	for i := 0; i < 20; i++ {
		candidates = append(candidates, cdnCandidate{fmt.Sprintf("1.2.3.%d", i+1), time.Now().Add(time.Minute)})
	}
	seen := map[string]bool{}
	for rotation := 0; rotation < 20; rotation++ {
		for _, c := range mergeSteamCandidates([][]cdnCandidate{rotateSteamCandidates(candidates, rotation)}) {
			seen[c.ip] = true
		}
	}
	if len(seen) != 20 || candidates[0].ip != "1.2.3.1" {
		t.Fatal("rotation lost candidates or mutated input")
	}
}

func TestSteamSpeedProbeCancelledGenerationCannotWrite(t *testing.T) {
	s, _ := New(Config{SteamCDNEnabled: true, Adapters: []Adapter{{Name: "a", SourceIP: "127.0.0.1"}}})
	s.cdn = newSteamCDN(context.Background(), true)
	defer s.cdn.cancel()
	seedCDN(s.cdn, "a", "80", "5.6.7.8")
	started := make(chan struct{})
	s.dialTCP = func(ctx context.Context, _ *net.Dialer, _ string) (net.Conn, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	done := make(chan struct{})
	gen := s.cdn.generation
	go func() {
		defer close(done)
		s.measureSteamCandidates(context.Background(), gen, s.config.Adapters[0], testSteamHost, testSteamChunk, []byte("test"), 1<<20, []cdnCandidate{{"5.6.7.8", time.Now().Add(time.Minute)}})
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("probe not started")
	}
	s.cdn.configure(true, true)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("reset did not cancel probe")
	}
	if snap := s.cdn.snapshot(); len(snap.Entries) != 0 || snap.SpeedProbeBytes == 0 {
		t.Fatal("reset lost budget or leaked entries")
	}
}

func TestSteamSpeedProbeBatchLimitAndRevisit(t *testing.T) {
	var calls atomic.Int32
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", steamSpeedProbeSize-1, 1<<20))
		w.WriteHeader(206)
		_, _ = w.Write(bytes.Repeat([]byte{'x'}, steamSpeedProbeSize))
	}))
	defer origin.Close()
	s, _ := New(Config{SteamCDNEnabled: true, Adapters: []Adapter{{Name: "a", SourceIP: "127.0.0.1"}}})
	s.cdn = newSteamCDN(context.Background(), true)
	defer s.cdn.cancel()
	s.dialTCP = func(ctx context.Context, _ *net.Dialer, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", origin.Listener.Addr().String())
	}
	var candidates []cdnCandidate
	for _, ip := range []string{"5.6.7.8", "5.6.7.9", "5.6.7.10"} {
		seedCDN(s.cdn, "a", "80", ip)
		candidates = append(candidates, cdnCandidate{ip, time.Now().Add(time.Minute)})
	}
	measure := func() {
		s.measureSteamCandidates(context.Background(), s.cdn.generation, s.config.Adapters[0], testSteamHost, testSteamChunk, []byte("xxxx"), 1<<20, candidates)
	}
	measure()
	if calls.Load() != 2 {
		t.Fatal("batch limit not enforced", calls.Load())
	}
	measure()
	if calls.Load() != 3 {
		t.Fatal("remaining candidate starved", calls.Load())
	}
	measure()
	if calls.Load() != 3 {
		t.Fatal("fresh candidate retested", calls.Load())
	}
}
