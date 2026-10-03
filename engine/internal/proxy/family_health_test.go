package proxy

import (
	"testing"
	"time"
)

func TestIPv6HealthFailureDoesNotPoisonIPv4OrNewAddress(t *testing.T) {
	a := Adapter{Name: "dual", SourceIP: "192.0.2.1", SourceIPv6: "2001:db8::1", IfIndex: 7, IPv6IfIndex: 8}
	h := newHealthTable([]Adapter{a})
	h.recordFamily(a, "tcp6", false)
	if h.familyAvailable(a, "udp6") || !h.familyAvailable(a, "tcp4") {
		t.Fatal("address-family health was not isolated")
	}
	global, _ := h.snapshot()
	if global[a.Name].State != "healthy" {
		t.Fatal("IPv6 fault poisoned whole adapter")
	}
	a.SourceIPv6 = "2001:db8::2"
	if !h.familyAvailable(a, "tcp6") {
		t.Fatal("new IPv6 address inherited stale fault")
	}
	h.recordFamily(a, "tcp6", true)
	if len(h.families) != 1 {
		t.Fatal("renumbered binding was not retired")
	}
	if got := h.familySnapshot(a, "udp6"); got.State != "healthy" || got.Successes != 1 {
		t.Fatalf("recovery=%+v", got)
	}
}

func TestFamilyCooldownExpiresAndTelemetrySurvivesIPv4Success(t *testing.T) {
	a := Adapter{Name: "dual", SourceIP: "192.0.2.1", SourceIPv6: "2001:db8::1"}
	h := newHealthTable([]Adapter{a})
	now := time.Now()
	h.now = func() time.Time { return now }
	h.recordFamily(a, "udp6", false)
	h.recordFamily(a, "tcp4", true)
	if h.familySnapshot(a, "tcp6").State != "cooldown" {
		t.Fatal("IPv4 success cleared IPv6 failure")
	}
	now = now.Add(3 * time.Second)
	if !h.familyAvailable(a, "udp6") {
		t.Fatal("expired IPv6 cooldown blocked recovery")
	}
}

func TestLiteralFamilyCooldownPrefersHealthyNICAndAllowsRecovery(t *testing.T) {
	a := []Adapter{{Name: "a", SourceIPv6: "2001:db8::1"}, {Name: "b", SourceIPv6: "2001:db8::2"}}
	h := newHealthTable(a)
	if h.familySnapshot(a[0], "tcp6").State != "unknown" {
		t.Fatal("unmeasured family was reported healthy")
	}
	h.recordFamily(a[0], "tcp6", false)
	excluded := map[string]struct{}{}
	excludeCoolingFamilies(a, excluded, h, "tcp4")
	if _, ok := excluded["a"]; !ok {
		t.Fatal("NAT64 selected a cooling IPv6 family")
	}
	h.recordFamily(a[1], "udp6", false)
	excluded = map[string]struct{}{}
	excludeCoolingFamilies(a, excluded, h, "udp6")
	if len(excluded) != 0 {
		t.Fatal("all cooling families blocked recovery")
	}
}
