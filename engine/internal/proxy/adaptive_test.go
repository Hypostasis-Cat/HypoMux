package proxy

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

func adaptiveFixture() (*performanceTable, []Adapter, *time.Time) {
	now := time.Unix(100, 0)
	p := newPerformanceTable()
	p.now = func() time.Time { return now }
	return p, []Adapter{{Name: "fast", SourceIP: "127.0.0.1", Weight: 1}, {Name: "slow", SourceIP: "127.0.0.2", Weight: 1}}, &now
}

func TestAdaptiveReservationsConcurrentAndIdempotent(t *testing.T) {
	p, adapters, _ := adaptiveFixture()
	s := newScheduler(adapters, false)
	s.strategy = StrategyAdaptive
	s.performance = p
	var wg sync.WaitGroup
	leases := make(chan *performanceLease, 100)
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, l, ok := s.acquireTCP(nil, "")
			if !ok {
				t.Error("no candidate")
			}
			leases <- l
		}()
	}
	wg.Wait()
	close(leases)
	if d := p.link(adapters[0]).load - p.link(adapters[1]).load; d < -2 || d > 2 {
		t.Fatalf("cold start imbalance %d", d)
	}
	for l := range leases {
		l.finish()
		l.finish()
	}
	for _, a := range adapters {
		if l := p.link(a); l.load != 0 || len(l.leases) != 0 {
			t.Fatal("leaked reservation", l)
		}
	}
}

// A fast start on one link must not cause positive feedback that starves
// the other link. Compare actual scheduler sequences, not predicted scores.
func TestAdaptiveUnqualifiedSamplesMatchRoundRobin(t *testing.T) {
	for _, mode := range []string{"cold", "asymmetric", "stale", "uneven-load"} {
		t.Run(mode, func(t *testing.T) {
			p, adapters, now := adaptiveFixture()
			adaptive := newScheduler(adapters, false)
			adaptive.strategy = StrategyAdaptive
			adaptive.performance = p
			baseline := newScheduler(adapters, false)
			for i, a := range adapters {
				l := p.link(a)
				if mode != "cold" {
					l.samples = 10
					l.updated = *now
					l.rate = 1000000 / float64(1+99*i)
				}
				if mode == "stale" {
					l.updated = now.Add(-time.Minute)
				}
			}
			var held []*performanceLease
			defer func() {
				for _, l := range held {
					l.finish()
				}
			}()
			if mode == "uneven-load" {
				for range 20 {
					_, l := p.acquire(adapters, false, adapters[0])
					held = append(held, l)
				}
			}
			for i := 0; i < 80; i++ {
				var excluded map[string]struct{}
				if i%7 == 0 {
					excluded = map[string]struct{}{adapters[0].Name: {}}
				}
				want, ok := baseline.SelectForDomain(excluded, "example.test")
				got, l, gotOK := adaptive.acquireTCP(excluded, "example.test")
				if gotOK != ok || got.Name != want.Name {
					t.Fatalf("decision %d: got %s want %s", i, got.Name, want.Name)
				}
				held = append(held, l)
				if i%3 == 0 {
					l.finish()
				}
				*now = now.Add(time.Second)
			}
			status := adaptive.performanceSnapshot()
			if status.State != "warming-up" || status.EffectiveTCPStrategy != StrategyRoundRobin || status.Strategy != StrategyAdaptive {
				t.Fatalf("warmup not disclosed: %+v", status)
			}
		})
	}
}

func TestAdaptiveAccountingCompletedIdleAndBackpressure(t *testing.T) {
	p, adapters, now := adaptiveFixture()
	_, l := p.acquire(adapters, false, adapters[0])
	l.attach()
	w := performanceWriter{writer: io.Discard, lease: l}
	if _, err := w.Write(make([]byte, 1000)); err != nil {
		t.Fatal(err)
	}
	l.finish() // A connection shorter than one tick must still contribute bytes.
	*now = now.Add(time.Second)
	p.sample()
	link := p.link(adapters[0])
	if link.rate != 1000 || link.samples != 1 || link.load != 0 {
		t.Fatalf("bad completed accounting: %+v", link)
	}
	_, idle := p.acquire(adapters, false, adapters[0])
	idle.attach()
	*now = now.Add(4 * time.Second)
	p.sample()
	if link.load != 0 || link.samples != 1 {
		t.Fatal("idle connection counted as transfer")
	}
	link.bytes.Add(100)
	link.blocked.Add(int64(time.Second))
	*now = now.Add(time.Second)
	p.sample()
	if link.rate != 1000 || link.samples != 1 {
		t.Fatal("backpressure trained model")
	}
	idle.finish()
}

func TestAdaptiveRetiredBindingCannotDebitReplacement(t *testing.T) {
	p, adapters, _ := adaptiveFixture()
	_, old := p.acquire(adapters, false, adapters[0])
	p.retain(adapters[1:])
	_, fresh := p.acquire(adapters, false, adapters[0])
	old.finish()
	old.attach()
	old.finish()
	if fresh.link == old.link || fresh.link.load != 1 {
		t.Fatal("old lifecycle changed new generation")
	}
	fresh.finish()
}

func TestAdaptiveDialFailureReleasesAllReservations(t *testing.T) {
	_, adapters, _ := adaptiveFixture()
	s, err := New(Config{Adapters: adapters, Strategy: StrategyAdaptive})
	if err != nil {
		t.Fatal(err)
	}
	s.dialTCP = func(context.Context, *net.Dialer, string) (net.Conn, error) {
		return nil, errors.New("injected dial error")
	}
	if _, _, err = s.dialUpstream(context.Background(), "127.0.0.3:80", s.scheduler, true, false); err == nil {
		t.Fatal("expected dial failure")
	}
	for _, l := range s.performance.links {
		if l.load != 0 || len(l.leases) != 0 {
			t.Fatal("failed dial leaked lease")
		}
	}
}

func TestAdaptiveLegacyUDPAndStrategyValidation(t *testing.T) {
	p, adapters, _ := adaptiveFixture()
	s := newScheduler(adapters, false)
	s.strategy = StrategyAdaptive
	s.performance = p
	for i := 0; i < 6; i++ {
		a, ok := s.Select(nil)
		if !ok || a.Name != adapters[i%2].Name {
			t.Fatal("UDP rotation changed")
		}
	}
	if p.decisions != 0 {
		t.Fatal("UDP trained TCP")
	}
	for _, tc := range []struct {
		strategy string
		weighted bool
		want     string
	}{{"", false, StrategyRoundRobin}, {"", true, StrategyWeighted}, {StrategyAdaptive, true, StrategyAdaptive}} {
		cfg, err := ValidateScheduling(SchedulingConfig{Strategy: tc.strategy, Weighted: tc.weighted, Adapters: adapters})
		if err != nil || cfg.Strategy != tc.want || cfg.Weighted != (tc.want == StrategyWeighted) {
			t.Fatal(cfg, err)
		}
	}
	if _, err := NormalizeStrategy("typo", false); err == nil {
		t.Fatal("accepted unknown strategy")
	}
}

func TestAdaptiveBoundConnectionHalfClose(t *testing.T) {
	p, adapters, _ := adaptiveFixture()
	_, l := p.acquire(adapters, false, adapters[0])
	l.attach()
	conn := &halfCloseConn{}
	wrapped := &leasedConn{Conn: conn, lease: l}
	closeWrite(wrapped)
	if !conn.halfClosed || l.finished {
		t.Fatal("half-close released the transfer")
	}
	_ = wrapped.Close()
	_ = wrapped.Close()
	if !l.finished || l.link.load != 0 {
		t.Fatal("close did not release")
	}
}

func TestAdaptiveRuntimeRollbackAndExplicitChannelLoad(t *testing.T) {
	_, adapters, _ := adaptiveFixture()
	s, err := New(Config{Adapters: adapters, Strategy: StrategyAdaptive})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Start(); err != nil {
		t.Fatal(err)
	}
	defer stopServer(t, s)
	explicit := newScheduler(adapters[:1], false, s.health)
	explicit.performance = s.performance
	_, bound, _ := explicit.acquireTCP(nil, "")
	defer bound.finish()
	chosen, pending, ok := s.scheduler.acquireTCP(nil, "")
	if !ok || chosen.Name != adapters[0].Name {
		t.Fatal("protection changed baseline rotation", chosen)
	}
	defer pending.finish()
	previous, err := s.UpdateScheduling(SchedulingConfig{Strategy: StrategyRoundRobin, Adapters: adapters})
	if err != nil || previous.Strategy != StrategyAdaptive {
		t.Fatal("lost previous strategy", previous, err)
	}
	if _, err = s.UpdateScheduling(previous); err != nil {
		t.Fatal(err)
	}
	status := s.scheduler.performanceSnapshot()
	if status.Strategy != StrategyAdaptive || status.UDPStrategy != StrategyRoundRobin || status.Links[0].Load != 2 || status.Links[1].Load != 0 {
		t.Fatalf("rollback corrupted active reservations: %+v", status)
	}
}

type halfCloseConn struct {
	net.Conn
	halfClosed bool
}

func (c *halfCloseConn) CloseWrite() error { c.halfClosed = true; return nil }
func (c *halfCloseConn) Close() error      { return nil }
