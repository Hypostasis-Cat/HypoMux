//go:build windows

package services

import (
	"os/exec"
	"testing"
)

// TestTunConfigWithExternalRuleSetPassesBundledSingBoxCheck is the load-bearing
// check for the whole design: it proves the bundled sing-box accepts a pinned
// configuration that references an external rule-set, that the watched file
// never nests a rule_set predicate (sing-box rejects rule-set recursion), and
// that a subscribed list and a manual exception resolve in the intended order.
func TestTunConfigWithExternalRuleSetPassesBundledSingBoxCheck(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	endpoints := map[string]string{
		"nic_ethernet": "127.0.0.1:19101",
		"nic_wifi":     "127.0.0.1:19102",
		"aggregation":  "127.0.0.1:19103",
		"direct":       "127.0.0.1:19104",
	}
	set := RuleSet{
		ID: "steam-id", Name: "Steam", URL: "https://a.example.test/steam.json",
		Outbound: "nic_wifi", Priority: 20,
	}
	publishExternalRuleSetFixture(t, set, []string{".steamcommunity.com"})
	rules, err := normalizeRulesStrict([]RoutingRule{
		{MatchType: MatchDomain, Value: "login.steamcommunity.com", Outbound: "direct", Priority: 90},
	})
	if err != nil {
		t.Fatal(err)
	}
	executable, configPath, _, err := writeSingBoxConfigWithOptions(
		endpoints, AdapterView{}, dnsResolveResult{Transport: "udp", Server: "1.1.1.1"},
		rules, compatibilityPlan{}, true,
		tunConfigOptions{DNSPolicy: "auto", RuleSets: []RuleSet{set}},
	)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(executable, "check", "--disable-color", "-c", configPath)
	if output, checkErr := command.CombinedOutput(); checkErr != nil {
		t.Fatalf("bundled sing-box rejected the external rule-set config: %v\n%s", checkErr, output)
	}

	for _, testCase := range []struct {
		domain string
		want   string
		reason string
	}{
		{"cdn.steamcommunity.com", "nic_wifi", "a subscribed list must route its whole category"},
		{"login.steamcommunity.com", "direct", "a higher-priority manual rule must stay an exception"},
		{"example.com", "aggregation", "an unrelated destination must fall through to the final outbound"},
	} {
		if got := priorityRouteFor(t, configPath, priorityFlow{domain: testCase.domain}); got != testCase.want {
			t.Fatalf("%s: %s routed to %q, want %q", testCase.reason, testCase.domain, got, testCase.want)
		}
	}
}

// TestTunConfigWithoutPublishedRuleSetFileStillPassesCheck covers the other side
// of the invariant: a configured list that has never been fetched must leave no
// reference behind, so the engine can still start.
func TestTunConfigWithoutPublishedRuleSetFileStillPassesCheck(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	endpoints := map[string]string{
		"nic_ethernet": "127.0.0.1:19111",
		"nic_wifi":     "127.0.0.1:19112",
		"aggregation":  "127.0.0.1:19113",
		"direct":       "127.0.0.1:19114",
	}
	set := RuleSet{
		ID: "never-fetched", Name: "Fresh", URL: "https://a.example.test/fresh.json",
		Outbound: "direct",
	}
	executable, configPath, _, err := writeSingBoxConfigWithOptions(
		endpoints, AdapterView{}, dnsResolveResult{Transport: "udp", Server: "1.1.1.1"},
		nil, compatibilityPlan{}, true,
		tunConfigOptions{DNSPolicy: "auto", RuleSets: []RuleSet{set}},
	)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(executable, "check", "--disable-color", "-c", configPath)
	if output, checkErr := command.CombinedOutput(); checkErr != nil {
		t.Fatalf("un-fetched rule-set leaked into the config: %v\n%s", checkErr, output)
	}
	if got := priorityRouteFor(t, configPath, priorityFlow{domain: "fresh.example"}); got != "aggregation" {
		t.Fatalf("an un-fetched list changed routing: got %q", got)
	}
}

func TestExternalRulesKeepPinnedRoutingCorrectAcrossEdits(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	high := RuleSet{ID: "high", Name: "High", URL: "https://example.com/high", Outbound: "direct", Priority: 80}
	low := RuleSet{ID: "low", Name: "Low", URL: "https://example.com/low", Outbound: "aggregation", Priority: 20}
	for _, set := range []RuleSet{high, low} {
		publishExternalRuleSetFixture(t, set, []string{".example.com"})
	}
	rules, err := normalizeRulesStrict([]RoutingRule{{MatchType: MatchDomain, Value: "login.example.com", Outbound: "aggregation", Priority: 90}})
	if err != nil {
		t.Fatal(err)
	}
	executable, path, _, err := writeSingBoxConfigWithOptions(map[string]string{"nic_ethernet": "127.0.0.1:19101", "nic_wifi": "127.0.0.1:19102", "direct": "127.0.0.1:19104", "aggregation": "127.0.0.1:19103"}, AdapterView{}, dnsResolveResult{Transport: "udp", Server: "1.1.1.1"}, rules, compatibilityPlan{}, true, tunConfigOptions{DNSPolicy: "auto", RuleSets: []RuleSet{high, low}})
	if err != nil {
		t.Fatal(err)
	}
	check := func(domain, want string) {
		t.Helper()
		if got := priorityRouteFor(t, path, priorityFlow{domain: domain}); got != want {
			t.Fatalf("%s routed to %s, want %s", domain, got, want)
		}
	}
	check("cdn.example.com", "direct")
	check("login.example.com", "aggregation")
	// Outbound edits cannot change the pinned route before restart. Its manual
	// carve-out must still use direct, or login would incorrectly become direct.
	high.Outbound = "aggregation"
	if err := refreshSingBoxRuleSets(rules, []RuleSet{high, low}); err != nil {
		t.Fatal(err)
	}
	check("login.example.com", "aggregation")
	check("cdn.example.com", "direct")
	high.Outbound = "direct"
	low.Priority = 85
	if err := refreshSingBoxRuleSets(rules, []RuleSet{high, low}); err != nil {
		t.Fatal(err)
	}
	check("cdn.example.com", "aggregation")
	low.Disabled = true
	if err := refreshSingBoxRuleSets(rules, []RuleSet{high, low}); err != nil {
		t.Fatal(err)
	}
	check("cdn.example.com", "direct")
	// Removing the earlier pinned set must not leave its old file matching.
	low.Disabled = false
	if err := refreshSingBoxRuleSets(rules, []RuleSet{low}); err != nil {
		t.Fatal(err)
	}
	check("cdn.example.com", "aggregation")
	if output, err := exec.Command(executable, "check", "--disable-color", "-c", path).CombinedOutput(); err != nil {
		t.Fatalf("empty disabled set rejected: %v %s", err, output)
	}
}

func TestExternalRejectUsesRejectAction(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	set := RuleSet{ID: "block", Name: "Block", URL: "https://example.com/block", Outbound: OutboundReject}
	publishExternalRuleSetFixture(t, set, []string{".example.com"})
	plan, err := writeSingBoxRuleSetPlan(nil, []RuleSet{set}, []string{"direct", "aggregation"}, true)
	if err != nil {
		t.Fatal(err)
	}
	route := plan.ExternalRouteRules[0].(map[string]any)
	if route["action"] != "reject" || route["outbound"] != nil {
		t.Fatalf("invalid rejection route: %+v", route)
	}
}
