package services

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
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
	plan, err := writeSingBoxRuleSetPlan(rules, []string{"nic_wifi", "direct"}, true)
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
	plan, err := writeSingBoxRuleSetPlan(initial, []string{"aggregation", "direct"}, true)
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
	plan, err := writeSingBoxRuleSetPlan(initial, []string{"aggregation", "direct"}, false)
	if err != nil {
		t.Fatal(err)
	}
	paths := ruleSetPaths(plan)
	before := readRuleSetFiles(t, paths)
	updated := []RoutingRule{{MatchType: MatchProcess, Value: "new.exe", Outbound: "direct"}}
	commitErr := errors.New("injected settings failure")
	if err := refreshSingBoxRuleSetsAndCommit(updated, func() error { return commitErr }); !errors.Is(err, commitErr) {
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
	plan, err := writeSingBoxRuleSetPlan(initial, []string{"aggregation", "direct"}, false)
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
	if _, err := writeSingBoxRuleSetPlan(nil, []string{"aggregation", "direct"}, false); err != nil {
		t.Fatal(err)
	}
	required, reason := singBoxRuleSetRestartRequirement(
		[]RoutingRule{{MatchType: MatchDomain, Value: "example.com", Outbound: "direct"}},
	)
	if !required || reason != "enable_fakeip" {
		t.Fatalf("restart requirement = %v, %q", required, reason)
	}
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
