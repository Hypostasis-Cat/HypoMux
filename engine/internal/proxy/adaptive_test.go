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

func TestAdaptiveRateLoadExplorationAndStaleness(t *testing.T) {
	p, adapters, now := adaptiveFixture()
	for i, a := range adapters {
		l := p.link(a)
		l.samples = 5
		l.updated = *now
		l.rate = 100 / float64(1+9*i)
	}
	chosen, l := p.acquire(adapters, true, adapters[1])
	if chosen.Name != "fast" {
		t.Fatal(chosen)
	}
	l.finish()
	p.link(adapters[0]).load = 20
	chosen, l = p.acquire(adapters, true, adapters[0])
	if chosen.Name != "slow" {
		t.Fatal("ignored projected load", chosen)
	}
	l.finish()
	p.link(adapters[0]).load = 0
	p.decisions = 19
	*now = now.Add(time.Second)
	p.link(adapters[1]).selected = now.Add(-10 * time.Second)
	chosen, l = p.acquire(adapters, true, adapters[0])
	if chosen.Name != "slow" {
		t.Fatal("missing exploration", chosen)
	}
	l.finish()
	*now = now.Add(time.Minute)
	p.link(adapters[0]).load = 2
	chosen, l = p.acquire(adapters, true, adapters[0])
	if chosen.Name != "slow" {
		t.Fatal("stale rate overrode load", chosen)
	}
	l.finish()
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
	if !ok || chosen.Name != adapters[1].Name {
		t.Fatal("explicit channel load ignored", chosen)
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
	if status.Strategy != StrategyAdaptive || status.UDPStrategy != StrategyRoundRobin || status.Links[0].Load != 1 || status.Links[1].Load != 1 {
		t.Fatalf("rollback corrupted active reservations: %+v", status)
	}
}

type halfCloseConn struct {
	net.Conn
	halfClosed bool
}

func (c *halfCloseConn) CloseWrite() error { c.halfClosed = true; return nil }
func (c *halfCloseConn) Close() error      { return nil }
