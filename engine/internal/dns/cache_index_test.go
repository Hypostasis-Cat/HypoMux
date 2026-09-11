package dns

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func TestCacheEvictsEarliestExpiryAndCleansIndex(t *testing.T) {
	root, cancel := context.WithCancel(context.Background())
	defer cancel()
	var dials atomic.Int64
	r, err := New(root, Config{Policy: PolicyOff, MaxCacheEntries: 2}, answeringDialer(t, &dials, "192.0.2.1", 60, 0))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	r.now = func() time.Time { return now }
	resolve := func(domain string, cached bool) {
		t.Helper()
		result, err := r.Resolve(root, Query{Domain: domain, Binding: loopbackBinding})
		if err != nil || result.Cached != cached {
			t.Fatalf("%s: cached=%v, err=%v", domain, result.Cached, err)
		}
	}
	resolve("early.example", false)
	now = now.Add(time.Second)
	resolve("later.example", false)
	resolve("early.example", true) // A recent hit must not change expiration-order eviction.
	resolve("new.example", false)
	resolve("later.example", true)
	resolve("early.example", false)
	if dials.Load() != 4 || r.cacheExpiry.Len() != 2 {
		t.Fatalf("dials=%d, index=%d", dials.Load(), r.cacheExpiry.Len())
	}
	now = now.Add(time.Minute)
	if status := r.Status(); status.CacheEntries != 0 || r.cacheExpiry.Len() != 0 {
		t.Fatalf("expired cache/index: %#v / %d", status, r.cacheExpiry.Len())
	}
}

func TestCacheHitChecksItsOwnDeadline(t *testing.T) {
	root, cancel := context.WithCancel(context.Background())
	defer cancel()
	var dials atomic.Int64
	r, err := New(root, Config{Policy: PolicyOff}, answeringDialer(t, &dials, "192.0.2.1", 1, 0))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	r.now = func() time.Time { return now }
	query := Query{Domain: "boundary.example", Binding: loopbackBinding}
	if _, err := r.Resolve(root, query); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	result, err := r.Resolve(root, query)
	if err != nil || result.Cached || dials.Load() != 2 || r.cacheExpiry.Len() != 1 {
		t.Fatalf("boundary result=%#v err=%v dials=%d index=%d", result, err, dials.Load(), r.cacheExpiry.Len())
	}
}

func BenchmarkResolverCacheHit(b *testing.B) {
	for _, size := range []int{1, 64, 1024, 16384} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			now := time.Now()
			binding := Binding{Name: "audit", SourceIP: "127.0.0.1", IfIndex: 1}
			r := &Resolver{root: context.Background(), now: func() time.Time { return now }, cache: make(map[cacheKey]cacheEntry)}
			for i := 0; i < size; i++ {
				key := cacheKey{adapter: binding.Name, sourceIP: binding.SourceIP, ifIndex: binding.IfIndex, domain: fmt.Sprintf("d%d.example", i), recordType: RecordA}
				at := now.Add(time.Hour)
				r.cache[key] = cacheEntry{result: Result{Address: "192.0.2.1", Addresses: []string{"192.0.2.1"}}, expiresAt: at}
				r.cacheExpiry.Set(key, at)
			}
			query := Query{Domain: "d0.example", RecordType: RecordA, Binding: binding}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				result, err := r.Resolve(context.Background(), query)
				if err != nil || !result.Cached {
					b.Fatal("expected cache hit", err)
				}
			}
		})
	}
}
