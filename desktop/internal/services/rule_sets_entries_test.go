package services

import (
	"fmt"
	"testing"
)

func TestRuleSetEntriesReadsDisabledCacheAndPaginates(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	settings := NewSettingsService()
	set := RuleSet{ID: "inspect", Name: "Inspect", URL: "https://example.com/rules.json", Outbound: "direct", Disabled: true}
	if err := settings.saveRuleSets([]RuleSet{set}); err != nil {
		t.Fatal(err)
	}
	values := []string{}
	for i := 0; i < 205; i++ {
		values = append(values, fmt.Sprintf("host-%03d.example.com", i))
	}
	payload, _, err := canonicalExternalRuleSetPayload([]map[string]any{{"domain": values}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = publishExternalRuleSetSource(ruleSetDirectory(), set, payload); err != nil {
		t.Fatal(err)
	}
	service := NewRuleSetService(settings, NewAdapterService(settings))
	result, err := service.Entries(set.ID, "", 200, 100)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Downloaded || result.Total != 205 || len(result.Entries) != 5 || result.Entries[0].Value != "host-200.example.com" {
		t.Fatalf("unexpected page: %#v", result)
	}
	result, err = service.Entries(set.ID, "HOST-201", 0, 100)
	if err != nil || result.Total != 1 || result.Entries[0].Kind != "domain" {
		t.Fatalf("search: %#v %v", result, err)
	}
	if _, err = service.Entries("../unknown", "", 0, 100); err == nil {
		t.Fatal("unknown ID accepted")
	}
}

func TestRuleSetEntriesDistinguishesUnfetchedFromNoMatches(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	settings := NewSettingsService()
	set := RuleSet{ID: "unfetched", Name: "Unfetched", URL: "https://example.com/rules.json", Outbound: "direct"}
	if err := settings.saveRuleSets([]RuleSet{set}); err != nil {
		t.Fatal(err)
	}
	service := NewRuleSetService(settings, NewAdapterService(settings))
	result, err := service.Entries(set.ID, "", 0, 100)
	if err != nil || result.Downloaded || result.Entries == nil {
		t.Fatalf("unfetched: %#v %v", result, err)
	}
	payload, _, _ := canonicalExternalRuleSetPayload([]map[string]any{{"ip_cidr": []string{"1.1.1.0/24"}}})
	if _, err = publishExternalRuleSetSource(ruleSetDirectory(), set, payload); err != nil {
		t.Fatal(err)
	}
	result, err = service.Entries(set.ID, "absent", 0, 100)
	if err != nil || !result.Downloaded || result.Total != 0 {
		t.Fatalf("no matches: %#v %v", result, err)
	}
}
