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
