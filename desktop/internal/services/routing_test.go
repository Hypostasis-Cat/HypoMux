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
