package services

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
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
	plan, err := writeSingBoxRuleSetPlan(rules, nil, []string{"nic_wifi", "direct"}, true)
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
	plan, err := writeSingBoxRuleSetPlan(initial, nil, []string{"aggregation", "direct"}, true)
	if err != nil {
		t.Fatal(err)
	}
	path := ruleSetPathFor(t, plan, ruleSetScopeProcess, "direct")
	updated := []RoutingRule{{MatchType: MatchProcess, Value: "new.exe", Outbound: "direct"}}
	if err := refreshSingBoxRuleSets(updated, nil); err != nil {
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

func TestPublishRuleSetFilesRollsBackEveryReplacementOnFailure(t *testing.T) {
	directory := t.TempDir()
	files := []ruleSetFile{
		{Path: filepath.Join(directory, "first.json"), Data: []byte("new-first"), Mode: 0o600},
		{Path: filepath.Join(directory, "second.json"), Data: []byte("new-second"), Mode: 0o600},
		{Path: filepath.Join(directory, "third.json"), Data: []byte("new-third"), Mode: 0o600},
	}
	if err := os.WriteFile(files[0].Path, []byte("old-first"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(files[1].Path, []byte("old-second"), 0o600); err != nil {
		t.Fatal(err)
	}
	replacements := 0
	_, err := publishRuleSetFiles(files, func(path string, data []byte, mode os.FileMode) error {
		replacements++
		if replacements == 3 {
			return errors.New("injected replacement failure")
		}
		return replaceFileAtomically(path, data, mode)
	})
	if err == nil {
		t.Fatal("injected replacement failure was ignored")
	}
	for path, expected := range map[string]string{
		files[0].Path: "old-first",
		files[1].Path: "old-second",
	} {
		data, readErr := os.ReadFile(path)
		if readErr != nil || string(data) != expected {
			t.Fatalf("%s was not restored: data=%q err=%v", filepath.Base(path), data, readErr)
		}
		if _, statErr := os.Stat(path + ".tmp"); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("temporary file remains for %s: %v", filepath.Base(path), statErr)
		}
	}
	if _, err := os.Stat(files[2].Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed target was unexpectedly created: %v", err)
	}
}

func TestRefreshRuleSetsRestoresFilesWhenSettingsCommitFails(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	initial := []RoutingRule{{MatchType: MatchProcess, Value: "old.exe", Outbound: "direct"}}
	plan, err := writeSingBoxRuleSetPlan(initial, nil, []string{"aggregation", "direct"}, false)
	if err != nil {
		t.Fatal(err)
	}
	paths := ruleSetPaths(plan)
	before := readRuleSetFiles(t, paths)
	updated := []RoutingRule{{MatchType: MatchProcess, Value: "new.exe", Outbound: "direct"}}
	commitErr := errors.New("injected settings failure")
	if err := refreshSingBoxRuleSetsAndCommit(updated, nil, func() error { return commitErr }); !errors.Is(err, commitErr) {
		t.Fatalf("refresh error = %v", err)
	}
	after := readRuleSetFiles(t, paths)
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("rule-sets changed after failed settings commit\nbefore=%q\nafter=%q", before, after)
	}
}

func TestRoutingSaveRestoresSettingsAndRuleSetsAfterPartialPublish(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	settings := NewSettingsService()
	service := NewRoutingRuleService(settings, NewAdapterService(settings), nil)
	initial := []RoutingRule{{MatchType: MatchProcess, Value: "old.exe", Outbound: "direct"}}
	if err := settings.saveRoutingRules(initial); err != nil {
		t.Fatal(err)
	}
	plan, err := writeSingBoxRuleSetPlan(initial, nil, []string{"aggregation", "direct"}, false)
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(settingsDirectory(), "runtime", "rule-sets")
	paths := append(ruleSetPaths(plan), filepath.Join(directory, singBoxRuleSetManifestName))
	beforeFiles := readRuleSetFiles(t, paths)
	beforeSettings, err := os.ReadFile(settings.path)
	if err != nil {
		t.Fatal(err)
	}

	originalReplace := replaceSingBoxRuleSet
	replacements := 0
	replaceSingBoxRuleSet = func(path string, data []byte, mode os.FileMode) error {
		replacements++
		if replacements == 3 {
			return errors.New("injected replacement failure")
		}
		return replaceFileAtomically(path, data, mode)
	}
	t.Cleanup(func() { replaceSingBoxRuleSet = originalReplace })

	if _, err := service.Save([]RoutingRule{{MatchType: MatchProcess, Value: "new.exe", Outbound: "direct"}}); err == nil {
		t.Fatal("partial rule-set replacement failure was ignored")
	}
	afterFiles := readRuleSetFiles(t, paths)
	if !reflect.DeepEqual(afterFiles, beforeFiles) {
		t.Fatalf("rule-sets changed after partial publish\nbefore=%q\nafter=%q", beforeFiles, afterFiles)
	}
	afterSettings, err := os.ReadFile(settings.path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(afterSettings, beforeSettings) || settings.Get().RoutingRules[0].Value != "old.exe" {
		t.Fatalf("settings changed after partial publish: %s", afterSettings)
	}
	for _, path := range paths {
		if _, err := os.Stat(path + ".tmp"); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("temporary file remains for %s: %v", filepath.Base(path), err)
		}
	}
}

func TestRuleSetRestartRequirementDetectsFirstOffPolicyDomainRule(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	if _, err := writeSingBoxRuleSetPlan(nil, nil, []string{"aggregation", "direct"}, false); err != nil {
		t.Fatal(err)
	}
	required, reason := singBoxRuleSetRestartRequirement(
		[]RoutingRule{{MatchType: MatchDomain, Value: "example.com", Outbound: "direct"}}, nil,
	)
	if !required || reason != "enable_fakeip" {
		t.Fatalf("restart requirement = %v, %q", required, reason)
	}
}

func TestExternalRuleSetTagAndPathStayStableAcrossURLEdits(t *testing.T) {
	set := RuleSet{ID: "steam-id", Name: "Steam", URL: "https://a.example.test/steam.json", Outbound: "direct"}
	firstTag, firstPath := externalRuleSetBinding(set)
	edited := set
	edited.URL = "https://b.example.test/other.yaml"
	secondTag, secondPath := externalRuleSetBinding(edited)
	if firstTag != secondTag || firstPath != secondPath {
		t.Fatalf("tag or path moved with the URL: %q/%q vs %q/%q", firstTag, firstPath, secondTag, secondPath)
	}
	if !strings.HasPrefix(firstTag, "hypomux-ext-") {
		t.Fatalf("unexpected external rule-set tag %q", firstTag)
	}
	otherTag, otherPath := externalRuleSetBinding(RuleSet{ID: "google-id", Outbound: "direct"})
	if otherTag == firstTag || otherPath == firstPath {
		t.Fatalf("different ids share a binding: %q %q", otherTag, otherPath)
	}
}

func TestSingBoxRuleSetPlanReferencesPublishedExternalRuleSet(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	set := RuleSet{
		ID: "steam-id", Name: "Steam", URL: "https://a.example.test/steam.json",
		Outbound: "nic_wifi", Priority: 50,
	}
	publishExternalRuleSetFixture(t, set, []string{"steamcommunity.com"})
	plan, err := writeSingBoxRuleSetPlan(nil, []RuleSet{set}, []string{"nic_wifi", "direct"}, true)
	if err != nil {
		t.Fatal(err)
	}
	tag, path := externalRuleSetBinding(set)
	definition := ruleSetDefinition(t, plan, tag)
	if definition["type"] != "local" || definition["format"] != "source" || definition["path"] != path {
		t.Fatalf("external rule-set definition is not the watched local source: %v", definition)
	}
	found := false
	for _, raw := range plan.ExternalRouteRules {
		reference := raw.(map[string]any)
		tags := reference["rule_set"].([]any)
		if len(tags) == 1 && tags[0] == tag && reference["outbound"] == "nic_wifi" {
			found = true
		}
	}
	if !found {
		t.Fatalf("pinned route rules do not reference %s: %v", tag, plan.ExternalRouteRules)
	}
	if !slices.Contains(plan.PriorityRuleSets, tag) {
		t.Fatalf("compatibility guard does not yield to %s: %v", tag, plan.PriorityRuleSets)
	}
	// A rule-set file cannot reference another rule-set (sing-box rejects the
	// recursion), so the tags must stay out of every per-outbound file.
	for _, scope := range []string{ruleSetScopeDomain, ruleSetScopeProcess, ruleSetScopeIP} {
		payload := readRuleSetPayload(t, ruleSetPathFor(t, plan, scope, "nic_wifi"))
		if payloadReferencesTag(payload, tag) {
			t.Fatalf("%s/%s rule-set references %s: %s", scope, "nic_wifi", tag, payload)
		}
		payload = readRuleSetPayload(t, ruleSetPathFor(t, plan, scope, "direct"))
		if payloadReferencesTag(payload, tag) {
			t.Fatalf("%s/%s rule-set references %s: %s", scope, "direct", tag, payload)
		}
	}
	watched := readRuleSetPayload(t, path)
	if !strings.Contains(watched, "steamcommunity.com") {
		t.Fatalf("watched file lost the ingested entries: %s", watched)
	}
}

func TestSingBoxRuleSetPlanNeverReferencesAnExternalRuleSetWithoutAPublishedFile(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	set := RuleSet{
		ID: "never-fetched", Name: "Fresh", URL: "https://a.example.test/fresh.json",
		Outbound: "direct",
	}
	plan, err := writeSingBoxRuleSetPlan(nil, []RuleSet{set}, []string{"aggregation", "direct"}, true)
	if err != nil {
		t.Fatal(err)
	}
	tag, path := externalRuleSetBinding(set)
	for _, raw := range plan.Definitions {
		definition := raw.(map[string]any)
		if definition["tag"] == tag || definition["path"] == path {
			t.Fatalf("a set with no published file was declared: %v", definition)
		}
	}
	if len(plan.ExternalRouteRules) != 0 {
		t.Fatalf("dangling external route rule emitted: %v", plan.ExternalRouteRules)
	}
}

func TestSingBoxRuleSetPlanSkipsDisabledExternalRuleSet(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	set := RuleSet{
		ID: "steam-id", Name: "Steam", URL: "https://a.example.test/steam.json",
		Outbound: "direct", Disabled: true,
	}
	publishExternalRuleSetFixture(t, set, []string{"steamcommunity.com"})
	plan, err := writeSingBoxRuleSetPlan(nil, []RuleSet{set}, []string{"aggregation", "direct"}, true)
	if err != nil {
		t.Fatal(err)
	}
	tag, _ := externalRuleSetBinding(set)
	for _, raw := range plan.Definitions {
		if raw.(map[string]any)["tag"] == tag {
			t.Fatalf("disabled rule-set is still declared: %v", raw)
		}
	}
	if len(plan.ExternalRouteRules) != 0 {
		t.Fatalf("disabled rule-set is still referenced: %v", plan.ExternalRouteRules)
	}
}

func TestExternalRuleSetExcludesHigherPriorityManualRule(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	set := RuleSet{
		ID: "steam-id", Name: "Steam", URL: "https://a.example.test/steam.json",
		Outbound: "direct", Priority: 10,
	}
	publishExternalRuleSetFixture(t, set, []string{"steamcommunity.com"})
	rules, err := normalizeRulesStrict([]RoutingRule{
		{MatchType: MatchDomain, Value: "steamcommunity.com", Outbound: "nic_wifi", Priority: 90},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writeSingBoxRuleSetPlan(rules, []RuleSet{set}, []string{"nic_wifi", "direct"}, true); err != nil {
		t.Fatal(err)
	}
	tag, path := externalRuleSetBinding(set)
	// The set is lower priority, so the manual destination must be carved out of
	// the watched file instead of being swallowed by the catch-all list.
	bound := readRuleSetPayload(t, path)
	if !strings.Contains(bound, "steamcommunity.com") || !strings.Contains(bound, `"invert"`) {
		t.Fatalf("higher-priority manual rule was not excluded from the set: %s", bound)
	}
	// The carve-out is concrete, never a rule_set reference: sing-box rejects
	// rule-set recursion inside rule-set files.
	if payloadReferencesTag(bound, tag) {
		t.Fatalf("watched file references another rule-set: %s", bound)
	}
	// An equal-priority manual rule must stay an exception too, while a
	// lower-priority one must not steal destinations from the list.
	low := append([]RoutingRule(nil), rules...)
	low[0].Priority = 10
	if _, err := writeSingBoxRuleSetPlan(low, []RuleSet{set}, []string{"nic_wifi", "direct"}, true); err != nil {
		t.Fatal(err)
	}
	if bound = readRuleSetPayload(t, path); !strings.Contains(bound, `"invert"`) {
		t.Fatalf("equal-priority manual rule was not excluded from the set: %s", bound)
	}
	low[0].Priority = 5
	if _, err := writeSingBoxRuleSetPlan(low, []RuleSet{set}, []string{"nic_wifi", "direct"}, true); err != nil {
		t.Fatal(err)
	}
	if bound = readRuleSetPayload(t, path); strings.Contains(bound, `"invert"`) {
		t.Fatalf("lower-priority manual rule wrongly excluded from the set: %s", bound)
	}
}

// TestExternalRuleSetPriorityChangeStaysHot pins the cost model of the feature:
// re-prioritizing a list only reorders content-level carve-outs, so neither the
// manifest nor the restart check may turn it into a restart.
func TestExternalRuleSetPriorityChangeStaysHot(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	set := RuleSet{
		ID: "steam-id", Name: "Steam", URL: "https://a.example.test/steam.json",
		Outbound: "direct", Priority: 10,
	}
	publishExternalRuleSetFixture(t, set, []string{"steamcommunity.com"})
	rules, err := normalizeRulesStrict([]RoutingRule{
		{MatchType: MatchDomain, Value: "steamcommunity.com", Outbound: "nic_wifi", Priority: 90},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writeSingBoxRuleSetPlan(rules, []RuleSet{set}, []string{"nic_wifi", "direct"}, true); err != nil {
		t.Fatal(err)
	}
	if required, reason := singBoxRuleSetRestartRequirement(rules, []RuleSet{set}); required {
		t.Fatalf("unchanged configuration demanded a restart: %q", reason)
	}
	reprioritized := set
	reprioritized.Priority = 80
	if required, reason := singBoxRuleSetRestartRequirement(rules, []RuleSet{reprioritized}); required {
		t.Fatalf("priority edit demanded a restart: %q", reason)
	}
	_, path := externalRuleSetBinding(set)
	if err := refreshSingBoxRuleSets(rules, []RuleSet{reprioritized}); err != nil {
		t.Fatal(err)
	}
	// Priority 80 still ranks below the manual rule, so the carve-out must
	// survive the recomposition.
	if payload := readRuleSetPayload(t, path); !strings.Contains(payload, `"invert"`) {
		t.Fatalf("re-prioritized list lost the manual-rule carve-out: %s", payload)
	}
	// Once the list outranks the manual rule the carve-out must disappear —
	// without a restart.
	reprioritized.Priority = 95
	if err := refreshSingBoxRuleSets(rules, []RuleSet{reprioritized}); err != nil {
		t.Fatal(err)
	}
	if payload := readRuleSetPayload(t, path); strings.Contains(payload, `"invert"`) {
		t.Fatalf("outranking list still excludes the manual rule: %s", payload)
	}
	rebound := set
	rebound.Outbound = "nic_wifi"
	if required, reason := singBoxRuleSetRestartRequirement(rules, []RuleSet{rebound}); !required || reason != "external_ruleset_changed" {
		t.Fatalf("outbound edit restart requirement = %v, %q", required, reason)
	}
}

// publishExternalRuleSetFixture stands in for the ingestion step: it writes the
// normalized payload an update would have published, so the compiler is tested
// against a set that has (or has not) been fetched successfully.
func publishExternalRuleSetFixture(t *testing.T, set RuleSet, suffixes []string) {
	t.Helper()
	tag, _ := externalRuleSetBinding(set)
	payload, err := json.MarshalIndent(map[string]any{
		"version": singBoxRuleSetVersion,
		"rules":   []any{map[string]any{"domain_suffix": suffixes}},
	}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	directory := ruleSetDirectory()
	source := externalRuleSetSourcePathIn(directory, set)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := replaceFileAtomically(source, append(payload, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if tag == "" {
		t.Fatal("empty rule-set tag")
	}
}

func TestSingBoxRuleSetManifestRecordsDeclaredExternalSets(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	set := RuleSet{
		ID: "steam-id", Name: "Steam", URL: "https://a.example.test/steam.json", Outbound: "direct",
	}
	publishExternalRuleSetFixture(t, set, []string{"steamcommunity.com"})
	if _, err := writeSingBoxRuleSetPlan(nil, []RuleSet{set}, []string{"aggregation", "direct"}, true); err != nil {
		t.Fatal(err)
	}
	tag, _ := externalRuleSetBinding(set)
	manifest := readRuleSetManifestFixture(t)
	if manifest.Version != singBoxRuleSetManifestVersion {
		t.Fatalf("manifest version = %d, want %d", manifest.Version, singBoxRuleSetManifestVersion)
	}
	want := []externalRuleSetRecord{{Tag: tag, Outbound: "direct", Priority: set.Priority}}
	if !reflect.DeepEqual(manifest.ExternalSets, want) {
		t.Fatalf("manifest external sets = %v, want %v", manifest.ExternalSets, want)
	}
}

func TestRefreshNeverReferencesAnExternalRuleSetTheRunningConfigOmitted(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	// The engine started while no list was configured, so the pinned main
	// configuration declares no external rule-set definition at all.
	plan, err := writeSingBoxRuleSetPlan(nil, nil, []string{"aggregation", "direct"}, true)
	if err != nil {
		t.Fatal(err)
	}
	domainFile := ruleSetPathFor(t, plan, ruleSetScopeDomain, "direct")

	set := RuleSet{
		ID: "late-id", Name: "Late", URL: "https://a.example.test/late.json", Outbound: "direct",
	}
	publishExternalRuleSetFixture(t, set, []string{"late.example"})
	if err := refreshSingBoxRuleSets(nil, []RuleSet{set}); err != nil {
		t.Fatal(err)
	}
	// A set the running configuration never declared must gain neither a
	// watched file nor a reference; the restart prompt carries the change.
	tag, path := externalRuleSetBinding(set)
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("undeclared set gained a watched file: %v", err)
	}
	if payload := readRuleSetPayload(t, domainFile); payloadReferencesTag(payload, tag) {
		t.Fatalf("dangling rule_set reference published for undeclared tag %s: %s", tag, payload)
	}
}

func TestRuleSetRestartRequirementDetectsNewlyLiveExternalRuleSet(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	if _, err := writeSingBoxRuleSetPlan(nil, nil, []string{"aggregation", "direct"}, true); err != nil {
		t.Fatal(err)
	}
	set := RuleSet{
		ID: "steam-id", Name: "Steam", URL: "https://a.example.test/steam.json", Outbound: "direct",
	}
	publishExternalRuleSetFixture(t, set, []string{"steamcommunity.com"})
	required, reason := singBoxRuleSetRestartRequirement(nil, []RuleSet{set})
	if !required || reason != "external_ruleset_changed" {
		t.Fatalf("restart requirement = %v, %q", required, reason)
	}
}

func TestRuleSetRestartRequirementDetectsARemovedExternalRuleSet(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	set := RuleSet{
		ID: "steam-id", Name: "Steam", URL: "https://a.example.test/steam.json", Outbound: "direct",
	}
	publishExternalRuleSetFixture(t, set, []string{"steamcommunity.com"})
	if _, err := writeSingBoxRuleSetPlan(nil, []RuleSet{set}, []string{"aggregation", "direct"}, true); err != nil {
		t.Fatal(err)
	}
	if required, reason := singBoxRuleSetRestartRequirement(nil, []RuleSet{set}); required {
		t.Fatalf("unchanged declaration demanded a restart: %q", reason)
	}
	// Deleting the list must surface a restart without removing the file the
	// running configuration still points at.
	if required, reason := singBoxRuleSetRestartRequirement(nil, nil); !required || reason != "external_ruleset_changed" {
		t.Fatalf("restart requirement = %v, %q", required, reason)
	}
	if _, err := os.Stat(ruleSetManifestPath()); err != nil {
		t.Fatalf("manifest disappeared: %v", err)
	}
}

func TestRuleSetRestartRequirementFallsBackOnOlderManifestOnlyWhenAListIsLive(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	if _, err := writeSingBoxRuleSetPlan(nil, nil, []string{"aggregation", "direct"}, true); err != nil {
		t.Fatal(err)
	}
	set := RuleSet{
		ID: "steam-id", Name: "Steam", URL: "https://a.example.test/steam.json", Outbound: "direct",
	}
	publishExternalRuleSetFixture(t, set, []string{"steamcommunity.com"})
	// A pre-upgrade manifest carries no declaration list, so an enabled list is
	// undecidable and must require a restart instead of being silently skipped.
	manifest := readRuleSetManifestFixture(t)
	manifest.Version = 2
	writeRuleSetManifestFixture(t, manifest)
	if required, reason := singBoxRuleSetRestartRequirement(nil, []RuleSet{set}); !required || reason != "external_ruleset_changed" {
		t.Fatalf("restart requirement = %v, %q", required, reason)
	}
	// With nothing subscribed the declaration list is irrelevant, so an old
	// manifest must not nag every upgrading user once.
	if required, reason := singBoxRuleSetRestartRequirement(nil, nil); required {
		t.Fatalf("older manifest without rule sets demanded a restart: %q", reason)
	}
	if err := refreshSingBoxRuleSets(nil, []RuleSet{set}); err != nil {
		t.Fatalf("older manifest rejected the refresh: %v", err)
	}
}

func readRuleSetManifestFixture(t *testing.T) singBoxRuleSetManifest {
	t.Helper()
	data, err := os.ReadFile(ruleSetManifestPath())
	if err != nil {
		t.Fatal(err)
	}
	var manifest singBoxRuleSetManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	return manifest
}

func writeRuleSetManifestFixture(t *testing.T, manifest singBoxRuleSetManifest) {
	t.Helper()
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := replaceFileAtomically(ruleSetManifestPath(), append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func ruleSetDefinition(t *testing.T, plan singBoxRuleSetPlan, tag string) map[string]any {
	t.Helper()
	for _, raw := range plan.Definitions {
		definition := raw.(map[string]any)
		if definition["tag"] == tag {
			return definition
		}
	}
	t.Fatalf("missing rule-set definition %s", tag)
	return nil
}

func readRuleSetPayload(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func payloadReferencesTag(payload, tag string) bool {
	var source struct {
		Rules []map[string]any `json:"rules"`
	}
	if err := json.Unmarshal([]byte(payload), &source); err != nil {
		return false
	}
	var found func(rule map[string]any) bool
	found = func(rule map[string]any) bool {
		if tags, ok := rule["rule_set"].([]any); ok {
			for _, candidate := range tags {
				if candidate == tag {
					return true
				}
			}
		}
		if nested, ok := rule["rules"].([]any); ok {
			for _, raw := range nested {
				if child, ok := raw.(map[string]any); ok && found(child) {
					return true
				}
			}
		}
		return false
	}
	for _, rule := range source.Rules {
		if found(rule) {
			return true
		}
	}
	return false
}

func ruleSetPaths(plan singBoxRuleSetPlan) []string {
	paths := make([]string, 0, len(plan.Definitions))
	for _, raw := range plan.Definitions {
		paths = append(paths, raw.(map[string]any)["path"].(string))
	}
	return paths
}

func readRuleSetFiles(t *testing.T, paths []string) map[string]string {
	t.Helper()
	result := make(map[string]string, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		result[path] = string(data)
	}
	return result
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
