package services

import (
	"reflect"
	"testing"
)

func TestCheckedQuickAddPreservesOrderAndRejectsConcurrentWrites(t *testing.T) {
	sets, _ := auditRuleSetService(t)
	routing := NewRoutingRuleService(sets.settings, sets.adapters, nil)
	order := []string{"ip", "domain", "process"}
	initial, err := routing.SaveOrdered([]RoutingRule{{MatchType: "process", Value: "existing.exe", Outbound: "direct"}}, order)
	if err != nil {
		t.Fatal(err)
	}
	quickRules := append(append([]RoutingRule(nil), initial.Rules...), RoutingRule{MatchType: "domain", Value: "example.com", Outbound: "aggregation"})
	saved, err := routing.SaveOrderedChecked(quickRules, initial.MatchOrder, initial.Revision)
	if err != nil || !reflect.DeepEqual(saved.MatchOrder, order) {
		t.Fatalf("order=%v error=%v", saved.MatchOrder, err)
	}
	stale := saved
	concurrentRules := append(append([]RoutingRule(nil), saved.Rules...), RoutingRule{MatchType: "process", Value: "AI-added.exe", Outbound: "direct"})
	concurrent, err := routing.SaveOrderedChecked(concurrentRules, order, saved.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := routing.SaveOrderedChecked(stale.Rules, stale.MatchOrder, stale.Revision); err == nil {
		t.Fatal("stale quick add overwrote AI rules")
	}
	current, err := routing.Snapshot()
	if err != nil || !reflect.DeepEqual(current.Rules, concurrent.Rules) || !reflect.DeepEqual(current.MatchOrder, order) {
		t.Fatalf("concurrent write was lost: %+v %v", current, err)
	}
}
