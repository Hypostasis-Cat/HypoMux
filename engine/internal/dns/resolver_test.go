package dns

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var loopbackBinding = Binding{
	Name:     "loopback",
	SourceIP: "127.0.0.1",
	IfIndex:  1,
}

func TestResolverCachesAndSharesInflightLookup(t *testing.T) {
	var dials atomic.Int64
	dial := answeringDialer(t, &dials, "192.0.2.44", 60, 25*time.Millisecond)
	resolver, err := New(context.Background(), Config{
		Policy:          PolicyOff,
		LegacyServers:   []string{"192.0.2.53"},
		CacheTTL:        time.Minute,
		QueryTimeout:    time.Second,
		MaxCacheEntries: 16,
	}, dial)
	if err != nil {
		t.Fatal(err)
	}

	results := make(chan Result, 8)
	errors := make(chan error, 8)
	for range 8 {
		go func() {
			result, err := resolver.Resolve(context.Background(), Query{
				Domain:     "shared.example",
				RecordType: RecordA,
				Binding:    loopbackBinding,
			})
			results <- result
			errors <- err
		}()
	}
	for range 8 {
		if err := <-errors; err != nil {
			t.Fatal(err)
		}
		if result := <-results; result.Address != "192.0.2.44" {
			t.Fatalf("address = %q", result.Address)
		}
	}
	if dials.Load() != 1 {
		t.Fatalf("network dials = %d, want 1", dials.Load())
	}

	cached, err := resolver.Resolve(context.Background(), Query{
		Domain:     "shared.example",
		RecordType: RecordA,
		Binding:    loopbackBinding,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !cached.Cached {
		t.Fatal("second lookup was not served from cache")
	}
	status := resolver.Status()
	if status.CacheEntries != 1 || status.CacheHits != 1 || status.Queries != 9 {
		t.Fatalf("status = %#v", status)
	}
}

func TestResolverExpiresCacheAndCapsDNSAnswerTTL(t *testing.T) {
	var dials atomic.Int64
	resolver, err := New(context.Background(), Config{
		Policy:          PolicyOff,
		LegacyServers:   []string{"192.0.2.53"},
		CacheTTL:        5 * time.Second,
		QueryTimeout:    time.Second,
		MaxCacheEntries: 16,
	}, answeringDialer(t, &dials, "192.0.2.45", 60, 0))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC)
	resolver.now = func() time.Time { return now }

	first, err := resolver.Resolve(context.Background(), Query{
		Domain:  "expiry.example",
		Binding: loopbackBinding,
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.ExpiresAt == nil || !first.ExpiresAt.Equal(now.Add(5*time.Second)) {
		t.Fatalf("expiry = %#v", first.ExpiresAt)
	}
	now = now.Add(6 * time.Second)
	second, err := resolver.Resolve(context.Background(), Query{
		Domain:  "expiry.example",
		Binding: loopbackBinding,
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Cached || dials.Load() != 2 {
		t.Fatalf("expired result = %#v, dials = %d", second, dials.Load())
	}
}

func TestAutoPolicyFallsBackOnlyToBoundTraditionalDNS(t *testing.T) {
	var dohDials atomic.Int64
	var legacyDials atomic.Int64
	dial := func(
		ctx context.Context,
		network string,
		address string,
		binding Binding,
	) (net.Conn, error) {
		if strings.HasSuffix(address, ":443") {
			dohDials.Add(1)
			return nil, errors.New("test DoH unavailable")
		}
		legacyDials.Add(1)
		return answeringConnection(t, "192.0.2.46", 30, 0, network), nil
	}
	resolver, err := New(context.Background(), Config{
		Policy:        PolicyAuto,
		LegacyServers: []string{"192.0.2.53"},
		QueryTimeout:  time.Second,
	}, dial)
	if err != nil {
		t.Fatal(err)
	}
	result, err := resolver.Resolve(context.Background(), Query{
		Domain:  "fallback.example",
		Binding: loopbackBinding,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Transport != "udp" || result.Server != "192.0.2.53:53" {
		t.Fatalf("fallback result = %#v", result)
	}
	if dohDials.Load() != int64(len(Endpoints(PolicyAuto))) || legacyDials.Load() != 1 {
		t.Fatalf("DoH dials = %d, legacy dials = %d", dohDials.Load(), legacyDials.Load())
	}
	if resolver.Status().AutomaticFallbacks != 1 {
		t.Fatalf("status = %#v", resolver.Status())
	}
}

func TestSystemPolicyUsesTraditionalDNSWithoutDoH(t *testing.T) {
	var dohDials atomic.Int64
	var legacyDials atomic.Int64
	dial := func(
		ctx context.Context,
		network string,
		address string,
		binding Binding,
	) (net.Conn, error) {
		if strings.HasSuffix(address, ":443") {
			dohDials.Add(1)
			return nil, errors.New("system policy must not use DoH")
		}
		legacyDials.Add(1)
		return answeringConnection(t, "192.0.2.47", 30, 0, network), nil
	}
	resolver, err := New(context.Background(), Config{
		Policy:        PolicySystem,
		LegacyServers: []string{"192.0.2.53"},
		QueryTimeout:  time.Second,
	}, dial)
	if err != nil {
		t.Fatal(err)
	}
	result, err := resolver.Resolve(context.Background(), Query{
		Domain:  "system.example",
		Binding: loopbackBinding,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Transport != "udp" || result.Server != "192.0.2.53:53" {
		t.Fatalf("system result = %#v", result)
	}
	if dohDials.Load() != 0 || legacyDials.Load() != 1 {
		t.Fatalf("DoH dials = %d, legacy dials = %d", dohDials.Load(), legacyDials.Load())
	}
	if endpoints := resolver.Status().DoHEndpoints; len(endpoints) != 0 {
		t.Fatalf("system policy advertised DoH endpoints: %#v", endpoints)
	}
}

func TestExplicitProviderEmitsOneControlledFallbackEvent(t *testing.T) {
	resolver, err := New(context.Background(), Config{
		Policy:           PolicyAliDNS,
		QueryTimeout:     100 * time.Millisecond,
		FailureThreshold: 2,
	}, func(context.Context, string, string, Binding) (net.Conn, error) {
		return nil, errors.New("test provider unavailable")
	})
	if err != nil {
		t.Fatal(err)
	}
	events := make(chan FallbackEvent, 2)
	resolver.SetFallbackHandler(func(event FallbackEvent) {
		events <- event
	})
	for _, domain := range []string{"first.example", "second.example", "third.example"} {
		if _, err := resolver.Resolve(context.Background(), Query{
			Domain:  domain,
			Binding: loopbackBinding,
		}); err == nil {
			t.Fatalf("%s unexpectedly resolved", domain)
		}
	}
	select {
	case event := <-events:
		if event.Adapter != loopbackBinding.Name || event.Policy != PolicyAliDNS {
			t.Fatalf("event = %#v", event)
		}
	default:
		t.Fatal("fallback event was not emitted")
	}
	select {
	case duplicate := <-events:
		t.Fatalf("duplicate event = %#v", duplicate)
	default:
	}
}

func TestRootCancellationReleasesLookup(t *testing.T) {
	root, cancel := context.WithCancel(context.Background())
	resolver, err := New(root, Config{
		Policy:           PolicyAliDNS,
		QueryTimeout:     10 * time.Second,
		FailureThreshold: 1,
	}, func(ctx context.Context, _ string, _ string, _ Binding) (net.Conn, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	events := make(chan FallbackEvent, 1)
	resolver.SetFallbackHandler(func(event FallbackEvent) {
		events <- event
	})
	done := make(chan error, 1)
	go func() {
		_, err := resolver.Resolve(context.Background(), Query{
			Domain:  "cancel.example",
			Binding: loopbackBinding,
		})
		done <- err
	}()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("lookup did not stop after root cancellation")
	}
	deadline := time.Now().Add(time.Second)
	for resolver.Status().Inflight != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if resolver.Status().Inflight != 0 {
		t.Fatal("cancelled lookup remained in flight")
	}
	select {
	case event := <-events:
		t.Fatalf("shutdown cancellation emitted fallback event: %#v", event)
	default:
	}
}

func answeringDialer(
	t *testing.T,
	dials *atomic.Int64,
	address string,
	ttl uint32,
	delay time.Duration,
) DialFunc {
	t.Helper()
	return func(
		_ context.Context,
		network string,
		_ string,
		_ Binding,
	) (net.Conn, error) {
		dials.Add(1)
		return answeringConnection(t, address, ttl, delay, network), nil
	}
}

func answeringConnection(
	t *testing.T,
	address string,
	ttl uint32,
	delay time.Duration,
	network string,
) net.Conn {
	t.Helper()
	client, server := net.Pipe()
	go func() {
		defer server.Close()
		var query []byte
		if network == "tcp4" {
			var length uint16
			if err := binary.Read(server, binary.BigEndian, &length); err != nil {
				return
			}
			query = make([]byte, int(length))
			if _, err := io.ReadFull(server, query); err != nil {
				return
			}
		} else {
			buffer := make([]byte, 4096)
			count, err := server.Read(buffer)
			if err != nil {
				return
			}
			query = append([]byte(nil), buffer[:count]...)
		}
		if delay > 0 {
			time.Sleep(delay)
		}
		response, err := answerForQueryRaw(query, dnsTypeA, address, ttl)
		if err != nil {
			return
		}
		if network == "tcp4" {
			_ = binary.Write(server, binary.BigEndian, uint16(len(response)))
		}
		_, _ = server.Write(response)
	}()
	return client
}

func TestDoHRaceIsBounded(t *testing.T) {
	var concurrent atomic.Int64
	var peak atomic.Int64
	dial := func(_ context.Context, _ string, _ string, _ Binding) (net.Conn, error) {
		now := concurrent.Add(1)
		defer concurrent.Add(-1)
		for {
			prev := peak.Load()
			if now <= prev || peak.CompareAndSwap(prev, now) {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
		return nil, errors.New("simulated DoH outage")
	}
	resolver, err := New(context.Background(), Config{
		Policy:          PolicyAuto,
		LegacyServers:   []string{"192.0.2.53"},
		CacheTTL:        time.Minute,
		QueryTimeout:    2 * time.Second,
		MaxCacheEntries: 16,
	}, dial)
	if err != nil {
		t.Fatal(err)
	}
	_, err = resolver.Resolve(context.Background(), Query{
		Domain:     "race.example",
		RecordType: RecordA,
		Binding:    loopbackBinding,
	})
	if err == nil {
		t.Fatal("expected resolution to fail with simulated DoH and legacy outage")
	}
	if peak.Load() > 2 {
		t.Fatalf("concurrent DoH dials peaked at %d, want <= 2", peak.Load())
	}
}

func TestDoHRaceAdvancesToLaterEndpoints(t *testing.T) {
	endpoints := Endpoints(PolicyAuto)
	if len(endpoints) < 3 {
		t.Skip("PolicyAuto no longer provides at least three endpoints")
	}
	blocked := map[string]bool{
		net.JoinHostPort(endpoints[0].IP, "443"): true,
		net.JoinHostPort(endpoints[1].IP, "443"): true,
	}
	var laterAttempts atomic.Int64
	var laterCtxAlive atomic.Bool
	laterCtxAlive.Store(true)
	// Barrier: later endpoints must only dial after both blocked endpoints have
	// already started dialing, so the test is deterministic regardless of
	// goroutine scheduling order.
	blockedStarted := make(chan struct{})
	var blockedCount atomic.Int64
	const blockedTotal = 2
	dial := func(ctx context.Context, _ string, address string, _ Binding) (net.Conn, error) {
		if blocked[address] {
			if blockedCount.Add(1) == blockedTotal {
				close(blockedStarted)
			}
			<-ctx.Done()
			return nil, ctx.Err()
		}
		<-blockedStarted
		if strings.HasSuffix(address, ":443") {
			if laterAttempts.Add(1) == 1 && ctx.Err() != nil {
				laterCtxAlive.Store(false)
			}
		}
		return nil, errors.New("simulated TLS failure")
	}
	resolver, err := New(context.Background(), Config{
		Policy:          PolicyAuto,
		LegacyServers:   []string{"192.0.2.53"},
		CacheTTL:        time.Minute,
		QueryTimeout:    3 * time.Second,
		MaxCacheEntries: 16,
	}, dial)
	if err != nil {
		t.Fatal(err)
	}
	_, err = resolver.Resolve(context.Background(), Query{
		Domain:     "later.example",
		RecordType: RecordA,
		Binding:    loopbackBinding,
	})
	if err == nil {
		t.Fatal("expected resolution to fail (DoH and legacy all fail)")
	}
	if laterAttempts.Load() == 0 {
		t.Fatal("endpoints after the first batch were never dialed")
	}
	if !laterCtxAlive.Load() {
		t.Fatal("later endpoint was dialed with an already-canceled context; it never got a chance to answer")
	}
}
