package services

import (
	"encoding/json"
	"testing"
)

func TestNormalizeRoutingRulesMatchesV220Semantics(t *testing.T) {
	rules, err := normalizeRulesStrict([]RoutingRule{
		{MatchType: "domain", Value: "*.例子.测试.", Outbound: "aggregation"},
		{MatchType: "ip_cidr", Value: "192.168.1.99/24", Outbound: "direct"},
		{MatchType: "process_name", Value: "Game.exe", Outbound: "nic_wifi"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if rules[0].MatchType != MatchProcess || rules[1].MatchType != MatchDomain || rules[2].MatchType != MatchIP {
		t.Fatalf("unexpected precedence: %#v", rules)
	}
	if rules[1].Value != "xn--fsqu00a.xn--0zwm56d" {
		t.Fatalf("IDN was not canonicalized: %q", rules[1].Value)
	}
	if rules[2].Value != "192.168.1.0/24" {
		t.Fatalf("CIDR was not canonicalized: %q", rules[2].Value)
	}
}

func TestParseLegacyRoutingRuleExpandsEveryValueWithoutReordering(t *testing.T) {
	rules, err := parseRoutingRulesJSON([]byte(`[
		{"process_name":["a.exe","b.exe"],"outbound":"aggregation"},
		{"match_type":"domain","domain":["one.example","two.example"],"outbound":"direct"},
		{"ip_cidr":["192.0.2.1","198.51.100.0/24"],"outbound":"nic_Ethernet"}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a.exe", "b.exe", "one.example", "two.example", "192.0.2.1/32", "198.51.100.0/24"}
	if len(rules) != len(want) {
		t.Fatalf("expanded %d rules, want %d: %#v", len(rules), len(want), rules)
	}
	for index, value := range want {
		if rules[index].Value != value {
			t.Fatalf("rule %d = %q, want %q", index, rules[index].Value, value)
		}
	}
}

func TestRoutingOutboundsRejectUnselectedAdapter(t *testing.T) {
	rules := []RoutingRule{{MatchType: MatchProcess, Value: "game.exe", Outbound: "nic_WLAN"}}
	if err := validateRoutingOutbounds(rules, []AdapterView{{ID: "WLAN", Selected: false, Operational: true}}); err == nil {
		t.Fatal("expected an unselected outbound to be rejected")
	}
	if err := validateRoutingOutbounds(rules, []AdapterView{{ID: "WLAN", Selected: true, Operational: true}}); err != nil {
		t.Fatalf("selected outbound was rejected: %v", err)
	}
}

func TestParseRoutingBackupV1AndLegacyList(t *testing.T) {
	for name, payload := range map[string]string{
		"v1":     `{"format":"hypomux-routing-rules","version":1,"rules":[{"match_type":"domain","domain":["Example.COM"],"outbound":"direct"}]}`,
		"legacy": `[{"process_name":["steam.exe"],"outbound":"aggregation"}]`,
	} {
		t.Run(name, func(t *testing.T) {
			rules, err := parseRoutingBackup([]byte(payload))
			if err != nil {
				t.Fatal(err)
			}
			if len(rules) != 1 {
				t.Fatalf("expected one rule, got %d", len(rules))
			}
			if _, err := json.Marshal(rules); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRejectInvalidRulesWithoutPartialSave(t *testing.T) {
	_, err := normalizeRulesStrict([]RoutingRule{
		{MatchType: MatchProcess, Value: "ok.exe", Outbound: "direct"},
		{MatchType: MatchDomain, Value: "https://invalid/path", Outbound: "aggregation"},
	})
	if err == nil {
		t.Fatal("expected invalid domain to fail the complete batch")
	}
}
