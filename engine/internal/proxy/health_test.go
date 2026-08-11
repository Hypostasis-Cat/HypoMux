package proxy

import (
	"context"
	"errors"
	"net"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/Hypostasis-Cat/HypoMux/engine/internal/dns"
)

func TestSharedHealthBackoffAndHalfOpenRecovery(t *testing.T) {
	adapters := []Adapter{
		{Name: "a", SourceIP: "127.0.0.1", Weight: 1},
		{Name: "b", SourceIP: "127.0.0.2", Weight: 1},
	}
	now := time.Date(2026, 7, 30, 4, 0, 0, 0, time.UTC)
	health := newHealthTable(adapters)
	health.now = func() time.Time { return now }
	proxyScheduler := newScheduler(adapters, false, health)
	tunScheduler := newScheduler(adapters, false, health)

	proxyScheduler.MarkFailure("a")
	selected, ok := tunScheduler.Select(nil)
	if !ok || selected.Name != "b" {
		t.Fatalf("shared cooldown selected %#v, %v", selected, ok)
	}
	snapshot, _ := health.snapshot()
	if snapshot["a"].State != "cooldown" ||
		snapshot["a"].ConsecutiveFailures != 1 ||
		snapshot["a"].CooldownUntil.Sub(now) != 2*time.Second {
		t.Fatalf("first failure snapshot = %#v", snapshot["a"])
	}

	now = now.Add(2 * time.Second)
	recoveryScheduler := newScheduler(adapters, false, health)
	selected, ok = recoveryScheduler.Select(nil)
	if !ok || selected.Name != "a" {
		t.Fatalf("half-open recovery selected %#v, %v", selected, ok)
	}
	snapshot, _ = health.snapshot()
	if snapshot["a"].State != "probing" {
		t.Fatalf("expired cooldown state = %#v", snapshot["a"])
	}

	recoveryScheduler.MarkSuccess("a")
	snapshot, _ = health.snapshot()
	if snapshot["a"].State != "healthy" ||
		snapshot["a"].ConsecutiveFailures != 0 ||
		snapshot["a"].Successes != 1 {
		t.Fatalf("recovered snapshot = %#v", snapshot["a"])
	}
}

func TestLocalFailureBackoffIsBounded(t *testing.T) {
	adapters := []Adapter{{Name: "a", SourceIP: "127.0.0.1"}}
	now := time.Date(2026, 7, 30, 4, 0, 0, 0, time.UTC)
	health := newHealthTable(adapters)
	health.now = func() time.Time { return now }

	for index, want := range []time.Duration{
		2 * time.Second,
		5 * time.Second,
		15 * time.Second,
		30 * time.Second,
		30 * time.Second,
	} {
		health.recordLocalFailure("a")
		snapshot, _ := health.snapshot()
		if got := snapshot["a"].CooldownUntil.Sub(now); got != want {
			t.Fatalf("failure %d backoff = %v, want %v", index+1, got, want)
		}
		now = now.Add(time.Second)
	}
}

func TestDomainQuarantineRequiresComparativeEvidenceAndAvoidsOutage(t *testing.T) {
	adapters := []Adapter{
		{Name: "a", SourceIP: "127.0.0.1"},
		{Name: "b", SourceIP: "127.0.0.2"},
	}
	now := time.Date(2026, 7, 30, 4, 0, 0, 0, time.UTC)
	health := newHealthTable(adapters)
	health.now = func() time.Time { return now }

	health.recordComparativeDomainFailure("a", "Example.COM.")
	scheduler := newScheduler(adapters, false, health)
	selected, ok := scheduler.SelectForDomain(nil, "example.com")
	if !ok || selected.Name != "a" {
		t.Fatalf("single evidence unexpectedly isolated adapter: %#v", selected)
	}

	health.recordComparativeDomainFailure("a", "example.com")
	scheduler = newScheduler(adapters, false, health)
	selected, ok = scheduler.SelectForDomain(nil, "example.com")
	if !ok || selected.Name != "b" {
		t.Fatalf("quarantined selection = %#v, %v", selected, ok)
	}
	scheduler = newScheduler(adapters, false, health)
	selected, _ = scheduler.SelectForDomain(nil, "other.example")
	if selected.Name != "a" {
		t.Fatalf("domain isolation leaked to another domain: %#v", selected)
	}

	health.recordComparativeDomainFailure("b", "example.com")
	health.recordComparativeDomainFailure("b", "example.com")
	scheduler = newScheduler(adapters, false, health)
	if _, ok = scheduler.SelectForDomain(nil, "example.com"); !ok {
		t.Fatal("all-domain quarantine caused a total outage")
	}

	_, quarantines := health.snapshot()
	if len(quarantines) != 2 ||
		quarantines[0].Domain != "example.com" ||
		quarantines[0].ExpiresAt.Sub(now) != domainQuarantineTTL {
		t.Fatalf("domain quarantine telemetry = %#v", quarantines)
	}

	now = now.Add(domainQuarantineTTL)
	_, quarantines = health.snapshot()
	if len(quarantines) != 0 {
		t.Fatalf("expired quarantines = %#v", quarantines)
	}
}

func TestDomainIsolationCanBeDisabled(t *testing.T) {
	adapters := []Adapter{
		{Name: "a", SourceIP: "127.0.0.1"},
		{Name: "b", SourceIP: "127.0.0.2"},
	}
	health := newHealthTableConfigured(adapters, false, true, nil)
	health.recordComparativeDomainFailure("a", "example.com")
	health.recordComparativeDomainFailure("a", "example.com")

	scheduler := newScheduler(adapters, false, health)
	selected, ok := scheduler.SelectForDomain(nil, "example.com")
	if !ok || selected.Name != "a" {
		t.Fatalf("disabled domain isolation selected %#v, %v", selected, ok)
	}
	_, quarantines := health.snapshot()
	if len(quarantines) != 0 {
		t.Fatalf("disabled domain isolation retained quarantines: %#v", quarantines)
	}
}

func TestDomainIsolationCanKeepQuarantineWithoutAutomaticExpiry(t *testing.T) {
	adapters := []Adapter{{Name: "a", SourceIP: "127.0.0.1"}}
	now := time.Date(2026, 7, 30, 4, 0, 0, 0, time.UTC)
	health := newHealthTableConfigured(adapters, true, false, nil)
	health.now = func() time.Time { return now }
	health.recordComparativeDomainFailure("a", "example.com")
	health.recordComparativeDomainFailure("a", "example.com")

	_, quarantines := health.snapshot()
	if len(quarantines) != 1 ||
		!quarantines[0].ExpiresAt.Equal(now.AddDate(100, 0, 0)) {
		t.Fatalf("permanent domain quarantine = %#v", quarantines)
	}
}

func TestSuccessClearsOnlyMatchingDomainEvidence(t *testing.T) {
	adapters := []Adapter{{Name: "a", SourceIP: "127.0.0.1"}}
	health := newHealthTable(adapters)
	health.recordComparativeDomainFailure("a", "one.example")
	health.recordComparativeDomainFailure("a", "two.example")
	health.recordSuccess("a", "one.example")

	state := health.adapters["a"]
	if _, exists := state.domains["one.example"]; exists {
		t.Fatal("matching domain evidence remained after success")
	}
	if _, exists := state.domains["two.example"]; !exists {
		t.Fatal("unrelated domain evidence was cleared")
	}
}

func TestDomainEvidenceExpiresBeforeQuarantine(t *testing.T) {
	adapters := []Adapter{{Name: "a", SourceIP: "127.0.0.1"}}
	now := time.Date(2026, 7, 30, 4, 0, 0, 0, time.UTC)
	health := newHealthTable(adapters)
	health.now = func() time.Time { return now }

	health.recordComparativeDomainFailure("a", "example.com")
	now = now.Add(domainEvidenceTTL)
	health.recordComparativeDomainFailure("a", "example.com")
	entry := health.adapters["a"].domains["example.com"]
	if entry == nil || entry.evidence != 1 {
		t.Fatalf("stale evidence was retained: %#v", entry)
	}
}

func TestDialLearnsDomainOnlyAfterAlternateAdapterSuccess(t *testing.T) {
	adapters := []Adapter{
		{Name: "a", SourceIP: "127.0.0.1"},
		{Name: "b", SourceIP: "127.0.0.2"},
	}
	server, err := New(Config{
		Adapters: adapters,
		DNS: dns.Config{
			Policy:        dns.PolicyOff,
			LegacyServers: []string{"192.0.2.53"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := dns.New(context.Background(), server.config.DNS, func(
		_ context.Context,
		network string,
		_ string,
		_ dns.Binding,
	) (net.Conn, error) {
		client, dnsServer := net.Pipe()
		go answerDNSAOnce(dnsServer, network, net.ParseIP("127.0.0.1").To4())
		return client, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	server.resolver = resolver
	dials := map[string]int{}
	server.dialTCP = func(
		_ context.Context,
		dialer *net.Dialer,
		_ string,
	) (net.Conn, error) {
		source := dialer.LocalAddr.(*net.TCPAddr).IP.String()
		dials[source]++
		if source == "127.0.0.1" {
			return nil, syscall.Errno(10061)
		}
		client, peer := net.Pipe()
		go peer.Close()
		return client, nil
	}

	for range domainFailureThreshold {
		connection, adapter, dialErr := server.dialUpstream(
			context.Background(),
			"Example.COM.:443",
			server.scheduler,
			false,
			false,
		)
		if dialErr != nil {
			t.Fatal(dialErr)
		}
		_ = connection.Close()
		if adapter.Name != "b" {
			t.Fatalf("alternate adapter = %q", adapter.Name)
		}
	}
	_, quarantines := server.health.snapshot()
	if len(quarantines) != 1 ||
		quarantines[0].Adapter != "a" ||
		quarantines[0].Domain != "example.com" {
		t.Fatalf("learned quarantines = %#v", quarantines)
	}
	before := dials["127.0.0.1"]
	connection, adapter, err := server.dialUpstream(
		context.Background(),
		"example.com:443",
		server.scheduler,
		false,
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	_ = connection.Close()
	if adapter.Name != "b" || dials["127.0.0.1"] != before {
		t.Fatalf("quarantine was not applied: adapter=%q dials=%#v", adapter.Name, dials)
	}
	health, _ := server.health.snapshot()
	if health["a"].Failures != 0 {
		t.Fatalf("remote refusal poisoned global health: %#v", health["a"])
	}
}

func TestDialDoesNotLearnDomainWhenEveryAdapterFails(t *testing.T) {
	adapters := []Adapter{
		{Name: "a", SourceIP: "127.0.0.1"},
		{Name: "b", SourceIP: "127.0.0.2"},
	}
	server, err := New(Config{
		Adapters: adapters,
		DNS: dns.Config{
			Policy:        dns.PolicyOff,
			LegacyServers: []string{"192.0.2.53"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := dns.New(context.Background(), server.config.DNS, func(
		_ context.Context,
		network string,
		_ string,
		_ dns.Binding,
	) (net.Conn, error) {
		client, dnsServer := net.Pipe()
		go answerDNSAOnce(dnsServer, network, net.ParseIP("127.0.0.1").To4())
		return client, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	server.resolver = resolver
	server.dialTCP = func(
		context.Context,
		*net.Dialer,
		string,
	) (net.Conn, error) {
		return nil, syscall.Errno(10061)
	}

	for range 2 {
		if _, _, err := server.dialUpstream(
			context.Background(),
			"outage.example:443",
			server.scheduler,
			false,
			false,
		); err == nil {
			t.Fatal("all-adapter failure unexpectedly succeeded")
		}
	}
	_, quarantines := server.health.snapshot()
	if len(quarantines) != 0 {
		t.Fatalf("all-adapter outage was learned: %#v", quarantines)
	}
}

func TestServerSnapshotPublishesAdaptiveHealth(t *testing.T) {
	server, err := New(Config{
		Adapters: []Adapter{{Name: "a", SourceIP: "127.0.0.1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	server.health.recordLocalFailure("a")
	server.health.recordComparativeDomainFailure("a", "example.com")
	server.health.recordComparativeDomainFailure("a", "example.com")

	snapshot := server.Snapshot(false)
	if len(snapshot.Adapters) != 1 {
		t.Fatalf("adapter telemetry = %#v", snapshot.Adapters)
	}
	adapter := snapshot.Adapters[0]
	if adapter.HealthState != "cooldown" ||
		adapter.ConsecutiveFailures != 1 ||
		adapter.HealthFailures != 1 ||
		adapter.CooldownUntil == nil ||
		adapter.DomainQuarantines != 1 {
		t.Fatalf("adaptive health telemetry = %#v", adapter)
	}
	if len(snapshot.DomainQuarantines) != 1 ||
		snapshot.DomainQuarantines[0].Adapter != "a" ||
		snapshot.DomainQuarantines[0].Domain != "example.com" {
		t.Fatalf("domain quarantine telemetry = %#v", snapshot.DomainQuarantines)
	}
}

func TestLocalConnectFailureClassification(t *testing.T) {
	if !isLocalConnectFailure(syscall.Errno(10022)) {
		t.Fatal("Windows disabled-socket error was not classified as local")
	}
	if !isLocalConnectFailure(syscall.Errno(10051)) {
		t.Fatal("Windows network-unreachable error was not classified as local")
	}
	if !isLocalConnectFailure(syscall.Errno(1214)) {
		t.Fatal("Windows disabled-interface error was not classified as local")
	}
	if isLocalConnectFailure(syscall.Errno(10061)) {
		t.Fatal("remote connection refusal was classified as local")
	}
	if isLocalConnectFailure(errors.New("plain failure")) {
		t.Fatal("untyped failure was classified as local")
	}
}

func TestSharedHealthConcurrentSelectionAndTelemetry(t *testing.T) {
	adapters := []Adapter{
		{Name: "a", SourceIP: "127.0.0.1"},
		{Name: "b", SourceIP: "127.0.0.2"},
		{Name: "c", SourceIP: "127.0.0.3"},
	}
	health := newHealthTable(adapters)
	schedulers := []*scheduler{
		newScheduler(adapters, false, health),
		newScheduler(adapters[:2], true, health),
		newScheduler(adapters[1:], false, health),
	}
	var workers sync.WaitGroup
	for worker := range 24 {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			scheduler := schedulers[index%len(schedulers)]
			for iteration := range 500 {
				selected, ok := scheduler.SelectForDomain(
					nil,
					"Concurrent.Example.",
				)
				if !ok {
					t.Errorf("worker %d found no adapter", index)
					return
				}
				switch iteration % 4 {
				case 0:
					scheduler.MarkFailure(selected.Name)
				case 1:
					scheduler.MarkSuccess(
						selected.Name,
						"concurrent.example",
					)
				case 2:
					health.recordComparativeDomainFailure(
						selected.Name,
						"concurrent.example",
					)
				default:
					health.snapshot()
				}
			}
		}(worker)
	}
	workers.Wait()
	snapshot, _ := health.snapshot()
	if len(snapshot) != len(adapters) {
		t.Fatalf("health snapshot lost adapters: %#v", snapshot)
	}
}
