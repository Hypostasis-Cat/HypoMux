package services

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestSingBoxRuleSetPlanPreservesOverlappingRulePriority(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	rules, err := normalizeRulesStrict([]RoutingRule{
		{MatchType: MatchDomain, Value: "a.example.com", Outbound: "nic_wifi"},
		{MatchType: MatchDomain, Value: "example.com", Outbound: "direct"},
		{MatchType: MatchIP, Value: "10.0.0.1/32", Outbound: "nic_wifi"},
		{MatchType: MatchIP, Value: "10.0.0.0/24", Outbound: "direct"},
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := writeSingBoxRuleSetPlan(rules, []string{"nic_wifi", "direct"})
	if err != nil {
		t.Fatal(err)
	}
	assertLogicalRuleSet := func(scope string) {
		t.Helper()
		path := ruleSetPathFor(t, plan, scope, "direct")
		var source struct {
			Version int              `json:"version"`
			Rules   []map[string]any `json:"rules"`
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if err := json.Unmarshal(data, &source); err != nil {
			t.Fatal(err)
		}
		if source.Version != singBoxRuleSetVersion || len(source.Rules) != 1 || source.Rules[0]["type"] != "logical" {
			t.Fatalf("%s direct rule-set did not preserve earlier overlapping rules: %s", scope, data)
		}
	}
	assertLogicalRuleSet(ruleSetScopeDomain)
	assertLogicalRuleSet(ruleSetScopeIP)
}

func TestRefreshSingBoxRuleSetsAtomicallyPublishesSavedRules(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	initial := []RoutingRule{{MatchType: MatchProcess, Value: "old.exe", Outbound: "direct"}}
	plan, err := writeSingBoxRuleSetPlan(initial, []string{"aggregation", "direct"})
	if err != nil {
		t.Fatal(err)
	}
	path := ruleSetPathFor(t, plan, ruleSetScopeProcess, "direct")
	updated := []RoutingRule{{MatchType: MatchProcess, Value: "new.exe", Outbound: "direct"}}
	if err := refreshSingBoxRuleSets(updated); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "new.exe") || strings.Contains(string(data), "old.exe") {
		t.Fatalf("hot-reloaded rule-set = %s", data)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temporary rule-set was not cleaned up: %v", err)
	}
}

func ruleSetPathFor(t *testing.T, plan singBoxRuleSetPlan, scope, outbound string) string {
	t.Helper()
	prefix := "hypomux-" + scope + "-"
	for _, raw := range plan.Definitions {
		definition := raw.(map[string]any)
		if strings.HasPrefix(definition["tag"].(string), prefix) {
			for _, routeRaw := range append(append([]any{}, plan.EarlyRouteRules...), plan.UserRouteRules...) {
				route := routeRaw.(map[string]any)
				tags := route["rule_set"].([]string)
				if tags[0] == definition["tag"] && route["outbound"] == outbound {
					return definition["path"].(string)
				}
			}
		}
	}
	t.Fatalf("missing %s/%s rule-set", scope, outbound)
	return ""
}
