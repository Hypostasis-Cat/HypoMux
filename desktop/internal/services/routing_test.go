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

func TestPreviewRoutingBatchClassifiesNormalizedValues(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	settings := NewSettingsService()
	service := NewRoutingRuleService(settings, NewAdapterService(settings), nil)
	preview, err := service.PreviewBatch(
		MatchDomain,
		[]string{"New.Example.", "new.example", "same.example", "move.example", "https://invalid/path"},
		"direct",
		[]RoutingRule{
			{MatchType: MatchDomain, Value: "same.example", Outbound: "direct"},
			{MatchType: MatchDomain, Value: "move.example", Outbound: "aggregation"},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if preview.AddCount != 1 || preview.DuplicateCount != 2 || preview.ConflictCount != 1 || preview.InvalidCount != 1 {
		t.Fatalf("unexpected batch counts: %#v", preview)
	}
	statuses := []string{
		RoutingBatchAdd,
		RoutingBatchDuplicate,
		RoutingBatchDuplicate,
		RoutingBatchConflict,
		RoutingBatchInvalid,
	}
	for index, status := range statuses {
		if preview.Items[index].Status != status {
			t.Fatalf("item %d status = %q, want %q", index, preview.Items[index].Status, status)
		}
	}
	if preview.Items[0].Rule.Value != "new.example" || preview.Items[3].ExistingOutbound != "aggregation" {
		t.Fatalf("batch values were not normalized: %#v", preview.Items)
	}
}

func TestPreviewRoutingBatchRejectsOversizedInput(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	settings := NewSettingsService()
	service := NewRoutingRuleService(settings, NewAdapterService(settings), nil)
	values := make([]string, RoutingBatchMaxValues+1)
	if _, err := service.PreviewBatch(MatchIP, values, "direct", nil); err == nil {
		t.Fatal("expected oversized batch to be rejected")
	}
}

func TestRoutingPreferencesRoundTripAndDisabledOutbound(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	settings := NewSettingsService()
	service := NewRoutingRuleService(settings, NewAdapterService(settings), nil)
	rules, err := parseRoutingRulesJSON([]byte(`[{"match_type":"process","value":"old.exe","outbound":"nic_missing","disabled":true,"priority":80},{"match_type":"ip","value":"192.0.2.1","outbound":"direct","priority":90}]`))
	if err != nil {
		t.Fatal(err)
	}
	saved, err := service.Save(rules)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Rules[0].MatchType != MatchIP || !saved.Rules[1].Disabled {
		t.Fatalf("lost preferences: %#v", saved.Rules)
	}
	reloaded := NewSettingsService().Get().RoutingRules
	if len(reloaded) != 2 || reloaded[1].Priority != 80 || !reloaded[1].Disabled {
		t.Fatalf("lost preferences after restart: %#v", reloaded)
	}
	reloaded[1].Disabled = false
	if _, err := service.Save(reloaded); err == nil {
		t.Fatal("enabled unavailable rule must fail")
	}
	if !settings.Get().RoutingRules[1].Disabled {
		t.Fatal("failed save modified persisted rules")
	}
	data, _ := json.Marshal(saved.Rules)
	imported, err := parseRoutingBackup(data)
	if err != nil || !imported[1].Disabled || imported[0].Priority != 90 {
		t.Fatalf("backup lost preferences: %s: %v", data, err)
	}
}

func TestRoutingPreferenceValidation(t *testing.T) {
	for _, value := range []string{`"high"`, `1.5`, `-1`, `1000`} {
		if _, err := parseRoutingRulesJSON([]byte(`[{"match_type":"process","value":"a.exe","outbound":"direct","priority":` + value + `}]`)); err == nil {
			t.Fatalf("accepted priority %s", value)
		}
	}
	legacy, err := parseRoutingRulesJSON([]byte(`[{"process_name":["a.exe"],"outbound":"direct"}]`))
	if err != nil || legacy[0].Disabled || legacy[0].Priority != 0 {
		t.Fatalf("legacy defaults changed: %#v %v", legacy, err)
	}
}

func TestSaveRoutingOrderPersistsWithEmptyListAndAppliesToNewRules(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	settings := NewSettingsService()
	service := NewRoutingRuleService(settings, NewAdapterService(settings), nil)
	order := []string{MatchIP, MatchDomain, MatchProcess}
	if _, err := service.SaveOrdered(nil, order); err != nil {
		t.Fatal(err)
	}
	reloaded := NewSettingsService()
	if got := routingMatchOrder(reloaded.Get()); got[0] != MatchIP || got[2] != MatchProcess {
		t.Fatal(got)
	}
	saved, err := service.Save([]RoutingRule{{MatchType: MatchProcess, Value: "app.exe", Outbound: "direct", Priority: 999}, {MatchType: MatchIP, Value: "203.0.113.0/24", Outbound: "aggregation"}})
	if err != nil || saved.Rules[0].MatchType != MatchIP || saved.Rules[1].Priority != 0 {
		t.Fatalf("new rules ignore order: %#v %v", saved, err)
	}
	if _, err := service.SaveOrdered(nil, []string{MatchIP, MatchIP, MatchDomain}); err == nil {
		t.Fatal("accepted duplicate types")
	}
	snapshot, err := service.Snapshot()
	if err != nil || len(snapshot.Rules) != 2 {
		t.Fatal("failed save changed rules")
	}
}
