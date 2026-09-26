package services

import (
	"testing"
)

func TestExternalRuleSetsRoundTripThroughSettings(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	settings := NewSettingsService()
	sets := []RuleSet{{
		ID:       "11111111-1111-4111-8111-111111111111",
		Name:     "Steam",
		URL:      "https://rules.example.test/steam.yaml",
		Outbound: "nic_wifi",
		Priority: 40,
	}}
	if err := settings.saveRuleSets(sets); err != nil {
		t.Fatal(err)
	}
	reloaded := NewSettingsService().Get().RuleSets
	if len(reloaded) != 1 {
		t.Fatalf("rule sets did not persist: %#v", reloaded)
	}
	if reloaded[0].Name != "Steam" || reloaded[0].URL != "https://rules.example.test/steam.yaml" ||
		reloaded[0].Outbound != "nic_wifi" || reloaded[0].Priority != 40 {
		t.Fatalf("rule set fields lost: %#v", reloaded[0])
	}
}

func TestExternalRuleSetsDefaultToEmptyAfterReload(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	settings := NewSettingsService()
	if err := settings.commitLocked(settings.Get()); err != nil {
		t.Fatal(err)
	}
	reloaded := NewSettingsService()
	if reloaded.StartupError() != nil {
		t.Fatal(reloaded.StartupError())
	}
	if got := reloaded.Get().RuleSets; len(got) != 0 {
		t.Fatalf("settings without rule_sets loaded %d entries", len(got))
	}
}

func TestValidateSettingsRejectsUnsafeExternalRuleSets(t *testing.T) {
	valid := RuleSet{
		ID: "11111111-1111-4111-8111-111111111111", Name: "Steam",
		URL: "https://rules.example.test/steam.yaml", Outbound: "direct",
	}
	settingsWith := func(set RuleSet) AppSettings {
		settings := DefaultSettings()
		settings.RuleSets = []RuleSet{set}
		return settings
	}
	if err := validateSettings(settingsWith(valid)); err != nil {
		t.Fatalf("valid rule set rejected: %v", err)
	}
	for name, mutate := range map[string]func(*RuleSet){
		"明文地址":   func(set *RuleSet) { set.URL = "http://rules.example.test/steam.yaml" },
		"缺少协议":   func(set *RuleSet) { set.URL = "rules.example.test/steam.yaml" },
		"带凭据地址":  func(set *RuleSet) { set.URL = "https://user:pass@rules.example.test/steam.yaml" },
		"未知出口":   func(set *RuleSet) { set.Outbound = "everything" },
		"空标识":    func(set *RuleSet) { set.ID = "" },
		"空名称":    func(set *RuleSet) { set.Name = "" },
		"超范围优先级": func(set *RuleSet) { set.Priority = 1000 },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := valid
			mutate(&candidate)
			if err := validateSettings(settingsWith(candidate)); err == nil {
				t.Fatalf("accepted unsafe rule set: %#v", candidate)
			}
		})
	}
}

func TestValidateSettingsRejectsDuplicateAndOversizedRuleSetLists(t *testing.T) {
	settings := DefaultSettings()
	settings.RuleSets = []RuleSet{
		{ID: "same", Name: "A", URL: "https://a.example.test/a.json", Outbound: "direct"},
		{ID: "same", Name: "B", URL: "https://b.example.test/b.json", Outbound: "aggregation"},
	}
	if err := validateSettings(settings); err == nil {
		t.Fatal("accepted duplicated rule set id")
	}

	settings = DefaultSettings()
	for index := 0; index <= RuleSetMaxCount; index++ {
		settings.RuleSets = append(settings.RuleSets, RuleSet{
			ID: string(rune('a'+index)) + "-unique", Name: "list",
			URL: "https://a.example.test/a.json", Outbound: "direct",
		})
	}
	if err := validateSettings(settings); err == nil {
		t.Fatalf("accepted more than %d rule sets", RuleSetMaxCount)
	}
}

func TestValidateRuleSetOutboundsRejectsUnavailableAdapter(t *testing.T) {
	sets := []RuleSet{{
		ID: "one", Name: "Steam", URL: "https://a.example.test/a.json", Outbound: "nic_WLAN",
	}}
	if err := validateRuleSetOutbounds(sets, []AdapterView{{ID: "WLAN", Selected: false, Operational: true}}); err == nil {
		t.Fatal("expected an unselected outbound to be rejected")
	}
	if err := validateRuleSetOutbounds(sets, []AdapterView{{ID: "WLAN", Selected: true, Operational: true}}); err != nil {
		t.Fatalf("selected outbound was rejected: %v", err)
	}
	sets[0].Disabled = true
	if err := validateRuleSetOutbounds(sets, nil); err != nil {
		t.Fatalf("a disabled rule set must not block startup: %v", err)
	}
}

func TestCloneSettingsDeepCopiesRuleSets(t *testing.T) {
	settings := DefaultSettings()
	settings.RuleSets = []RuleSet{{ID: "one", Name: "Steam", URL: "https://a.example.test/a.json", Outbound: "direct"}}
	clone := cloneSettings(settings)
	clone.RuleSets[0].Name = "Renamed"
	if settings.RuleSets[0].Name != "Steam" {
		t.Fatalf("clone shares the rule set slice: %#v", settings.RuleSets)
	}
}
