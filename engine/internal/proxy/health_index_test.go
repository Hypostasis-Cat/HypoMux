package proxy

import (
	"fmt"
	"testing"
	"time"
)

func TestDomainIndexRenewalAndSuccessDoNotLeaveOldDeadlines(t *testing.T) {
	h := newHealthTable([]Adapter{{Name: "a"}})
	now := time.Now()
	h.now = func() time.Time { return now }
	h.recordComparativeDomainFailure("a", "renew.example")
	now = now.Add(time.Minute)
	h.recordComparativeDomainFailure("a", "renew.example")
	for i := 0; i < 1000; i++ {
		h.recordComparativeDomainFailure("a", "renew.example")
	}
	state := h.adapters["a"]
	if state.domainExpiry.Len() != 1 {
		t.Fatal("renewals accumulated index entries")
	}
	now = now.Add(domainEvidenceTTL)
	_, quarantines := h.snapshot()
	if len(quarantines) != 1 {
		t.Fatal("old evidence deadline removed renewed quarantine")
	}
	h.recordSuccess("a", "renew.example")
	if state.domainExpiry.Len() != 0 || len(state.domains) != 0 {
		t.Fatal("success retained indexed evidence")
	}
	h.recordComparativeDomainFailure("a", "renew.example")
	now = now.Add(domainEvidenceTTL)
	h.candidates([]Adapter{{Name: "a"}}, nil, "renew.example")
	if state.domainExpiry.Len() != 0 || len(state.domains) != 0 {
		t.Fatal("evidence did not expire at boundary")
	}
}

func TestSeededDomainIndexExpiresAndRetainsOtherDomains(t *testing.T) {
	now := time.Now()
	h := newHealthTableConfigured([]Adapter{{Name: "a"}}, true, true, []DomainQuarantineSeed{
		{Adapter: "a", Domain: "due.example", ExpiresAt: now},
		{Adapter: "a", Domain: "kept.example", ExpiresAt: now.Add(time.Hour)},
		{Adapter: "a", Domain: "kept.example", ExpiresAt: now.Add(2 * time.Hour)},
	})
	h.now = func() time.Time { return now }
	_, quarantines := h.snapshot()
	if len(quarantines) != 1 || quarantines[0].Domain != "kept.example" || h.adapters["a"].domainExpiry.Len() != 1 {
		t.Fatalf("seed expiry: %#v", quarantines)
	}
}

func BenchmarkSchedulerDomainEvidence(b *testing.B) {
	for _, size := range []int{0, 64, 1024, 8192} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			adapters := []Adapter{{Name: "a", Weight: 1}, {Name: "b", Weight: 1}}
			s := newScheduler(adapters, true)
			now := time.Now()
			s.health.now = func() time.Time { return now }
			for _, a := range adapters {
				for i := 0; i < size; i++ {
					domain := fmt.Sprintf("d%d.example", i)
					at := now.Add(time.Hour)
					s.health.adapters[a.Name].domains[domain] = &domainHealth{evidence: 2, expiresAt: at}
					s.health.adapters[a.Name].domainExpiry.Set(domain, at)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, ok := s.SelectForDomain(nil, "unrelated.example"); !ok {
					b.Fatal("no adapter")
				}
			}
		})
	}
}
