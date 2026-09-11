package proxy

import (
	"context"
	"net"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Hypostasis-Cat/HypoMux/engine/internal/dns"
)

func TestSteamAttributionMixedConnectionsAndReset(t *testing.T) {
	c := newSteamCDN(context.Background(), true)
	defer c.cancel()
	seedCDN(c, "a", "80", "5.6.7.8")
	k := cdnKey{"a", testSteamHost, "80", "5.6.7.8"}
	c.trafficChange(k, c.generation, 1, 1000, false, true)
	c.trafficChange(k, c.generation, 1, 2000, false, false)
	snap := c.snapshot()
	e := snap.Entries[0]
	if snap.SwitchedBytes != 1000 || snap.OriginalBytes != 2000 || e.SwitchedBytes != 1000 || e.SwitchedActive != 1 || e.SwitchedBPS != 200 || e.TotalBPS != 600 {
		t.Fatalf("bad attribution %+v %+v", snap, e)
	}
	old := c.generation
	c.configure(true, true)
	c.accountTransfer(k, old, 1, 9999, true, true)
	snap = c.snapshot()
	if snap.SwitchedBytes != 0 || snap.TransferFailures != 0 {
		t.Fatal("old transfer leaked")
	}
}

func TestSteamRouteFailurePauseIsolationAndRecovery(t *testing.T) {
	c := newSteamCDN(context.Background(), true)
	defer c.cancel()
	seedCDN(c, "a", "80", "5.6.7.8")
	k := cdnKey{"a", testSteamHost, "80", "5.6.7.8"}
	c.trafficChange(k, c.generation, 1, 0, false)
	for range 3 {
		c.accountTransfer(k, c.generation, 0, 0, true, true)
	}
	if ip, _ := c.useTrial("a", testSteamHost, "80", "1.2.3.4"); ip != "" {
		t.Fatal("paused route admitted")
	}
	seedCDN(c, "b", "80", "5.6.7.8")
	if ip, _ := c.useTrial("b", testSteamHost, "80", "1.2.3.4"); ip == "" {
		t.Fatal("pause leaked to another NIC")
	}
	now := time.Now().Add(61 * time.Second)
	c.now = func() time.Time { return now }
	c.entries[k].ExpiresAt = now.Add(time.Minute)
	c.decisions[steamFailureKey(k)] = 7
	if ip, _ := c.useTrial("a", testSteamHost, "80", "1.2.3.4"); ip == "" {
		t.Fatal("pause failed to expire")
	}
}

func TestSteamSessionHintsAreScopedUnvalidatedAndExpire(t *testing.T) {
	c := newSteamCDN(context.Background(), true)
	defer c.cancel()
	now := time.Now()
	c.now = func() time.Time { return now }
	c.observed[cdnKey{"a", testSteamHost, "80", "1.1.1.1"}] = now.Add(time.Minute)
	c.observed[cdnKey{"a", "other.steamcontent.com", "80", "8.8.8.8"}] = now.Add(time.Minute)
	c.observed[cdnKey{"a", testSteamHost, "443", "9.9.9.9"}] = now.Add(time.Minute)
	c.observed[cdnKey{"a", testSteamHost, "80", "10.0.0.1"}] = now.Add(time.Minute)
	if got := c.observedCandidatesLocked(testSteamHost, "80"); len(got) != 1 || got[0].ip != "1.1.1.1" {
		t.Fatal(got)
	}
	if len(c.entries) != 0 {
		t.Fatal("observation admitted without validation")
	}
	now = now.Add(2 * time.Minute)
	c.pruneLocked()
	if len(c.observedCandidatesLocked(testSteamHost, "80")) != 0 {
		t.Fatal("expired hint retained")
	}
}

func TestSteamAttributionUsesOneRateBucket(t *testing.T) {
	c := newSteamCDN(context.Background(), true)
	defer c.cancel()
	seedCDN(c, "a", "80", "5.6.7.8")
	k := cdnKey{"a", testSteamHost, "80", "5.6.7.8"}
	now := time.Now()
	c.now = func() time.Time { now = now.Add(time.Second); return now }
	c.trafficChange(k, c.generation, 1, 5000, false, true)
	for _, slot := range c.traffic[k].slots {
		if slot.switched > slot.bytes {
			t.Fatal("switched bytes exceed total in rate bucket")
		}
	}
}

func TestSteamDiscoverySourcesAndTTL(t *testing.T) {
	now := time.Now()
	nodes, sources := mergeSteamSources([]cdnCandidate{{"1.1.1.1", now.Add(time.Minute)}, {"8.8.8.8", now.Add(time.Minute)}}, []cdnCandidate{{"1.1.1.1", now.Add(time.Second)}, {"9.9.9.9", now.Add(time.Minute)}})
	if len(nodes) != 3 || !nodes[0].expires.Equal(now.Add(time.Second)) || sources["1.1.1.1"] != "dns_and_session" || sources["9.9.9.9"] != "session" || sources["8.8.8.8"] != "dns" {
		t.Fatal(nodes, sources)
	}
}

func TestSteamLoadChangeRelearnsWithoutLosingAttribution(t *testing.T) {
	c := newSteamCDN(context.Background(), true)
	defer c.cancel()
	seedCDN(c, "a", "80", "5.6.7.8")
	k := cdnKey{"a", testSteamHost, "80", "5.6.7.8"}
	c.trafficChange(k, c.generation, 1, 1048576, false, true)
	for range 5 {
		c.observe(k, c.generation, 1048576, time.Second)
	}
	c.entries[k].Preferred = true
	c.trafficChange(k, c.generation, 1, 1048576, false, false)
	c.observe(k, c.generation, 524288, time.Second)
	e := c.entries[k]
	if e.Samples != 1 || e.DownloadBPS != 524288 || e.Preferred {
		t.Fatal("old load influenced new score", e)
	}
	snap := c.snapshot()
	if snap.SwitchedBytes != 1048576 || snap.OriginalBytes != 1048576 {
		t.Fatal("load change lost attribution")
	}
}

func TestSteamPausedRouteSnapshotAndRecovery(t *testing.T) {
	c := newSteamCDN(context.Background(), true)
	defer c.cancel()
	seedCDN(c, "a", "80", "5.6.7.8")
	k := cdnKey{"a", testSteamHost, "80", "5.6.7.8"}
	now := time.Now()
	c.now = func() time.Time { return now }
	c.entries[k].ExpiresAt = now.Add(10 * time.Minute)
	c.entries[k].Preferred = true
	c.trafficChange(k, c.generation, 1, 0, false, true)
	for range 3 {
		c.accountTransfer(k, c.generation, 0, 0, true, true)
	}
	entry := c.snapshot().Entries[0]
	if entry.DecisionReason != "route_paused" || entry.Preferred {
		t.Fatal("pause hidden by waiting-sample state", entry)
	}
	// A selection can persist the pause reason; expiry must clear its display.
	c.entries[k].DecisionReason = "route_paused"
	now = now.Add(61 * time.Second)
	if entry = c.snapshot().Entries[0]; entry.DecisionReason == "route_paused" || entry.Preferred {
		t.Fatal("expired pause still displayed", entry)
	}
}

func TestSteamHTTPSObservedCandidatesRequireTLSOnEachNIC(t *testing.T) {
	origin := httptest.NewTLSServer(nil)
	defer origin.Close()
	s, err := New(Config{SteamCDNEnabled: true, Adapters: []Adapter{{Name: "a", SourceIP: "127.0.0.1"}, {Name: "b", SourceIP: "127.0.0.2"}}, DNS: dns.Config{Policy: dns.PolicyOff}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	s.cdn = newSteamCDN(ctx, true)
	defer s.cdn.cancel()
	for _, key := range []cdnKey{
		{"a", testSteamHost, "443", "5.6.7.8"},
		{"a", testSteamHost, "80", "8.8.8.8"},
		{"a", "other.steamcontent.com", "443", "9.9.9.9"},
	} {
		s.cdn.observed[key] = time.Now().Add(time.Minute)
	}
	resolver, err := dns.New(ctx, s.config.DNS, func(_ context.Context, network, _ string, _ dns.Binding) (net.Conn, error) {
		a, b := net.Pipe()
		go answerDNSAOnce(b, network, net.ParseIP("1.2.3.4").To4())
		return a, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	checked := map[string]int{}
	s.dialTCP = func(ctx context.Context, d *net.Dialer, target string) (net.Conn, error) {
		if target != "1.2.3.4:443" && target != "5.6.7.8:443" {
			t.Error("candidate escaped host/port scope", target)
		}
		checked[d.LocalAddr.String()+"/"+target]++
		return (&net.Dialer{}).DialContext(ctx, "tcp", origin.Listener.Addr().String())
	}
	s.probeSteamCandidates(ctx, s.cdn.generation, testSteamHost, "443", "", resolver)
	for _, source := range []string{"127.0.0.1:0", "127.0.0.2:0"} {
		if checked[source+"/5.6.7.8:443"] != 1 {
			t.Fatal("session candidate not independently verified on each NIC", checked)
		}
	}
	status := s.cdn.snapshot()
	if len(status.Entries) != 0 || status.StageCounts["tls_validation_failed"] != 4 {
		t.Fatal("untrusted TLS session hint was admitted", status)
	}
}
