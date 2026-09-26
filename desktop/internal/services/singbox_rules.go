package services

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

const (
	singBoxRuleSetManifestName    = "sing-box-rule-sets.json"
	singBoxRuleSetManifestVersion = 3
	singBoxRuleSetVersion         = 3

	ruleSetScopeCustomIP = "custom-ip"
	ruleSetScopeEarlyIP  = "early-ip"
	ruleSetScopeProcess  = "process"
	ruleSetScopeDomain   = "domain"
	ruleSetScopeIP       = "ip"
)

var (
	singBoxRuleSetMu      sync.Mutex
	replaceSingBoxRuleSet = replaceFileAtomically
)

type singBoxRuleSetManifest struct {
	Version        int      `json:"version"`
	Outbounds      []string `json:"outbounds"`
	UsesFakeIP     bool     `json:"uses_fakeip"`
	CustomPriority bool     `json:"custom_priority"`
	// ExternalSets lists the external rule-sets the pinned main configuration
	// declares route rules for. A refresh may only recompose these, because the
	// main configuration is not rewritten while the engine runs.
	ExternalSets []externalRuleSetRecord `json:"external_sets,omitempty"`
}

type ruleSetFile struct {
	Path string
	Data []byte
	Mode os.FileMode
}

type ruleSetFileSnapshot struct {
	Path    string
	Data    []byte
	Mode    os.FileMode
	Existed bool
}

type singBoxRuleSetBinding struct {
	Scope    string
	Outbound string
	Tag      string
	Path     string
}

type singBoxRuleSetPlan struct {
	Definitions        []any
	EarlyRouteRules    []any
	UserRouteRules     []any
	ExternalRouteRules []any
	PriorityRuleSets   []string
}

// ruleSetDirectory is the single root sing-box watches for hot reload. External
// rule-set files live beside the generated ones so they share the same atomic
// publish, rollback and watch behaviour.
func ruleSetDirectory() string {
	return filepath.Join(settingsDirectory(), "runtime", "rule-sets")
}

func ruleSetManifestPath() string {
	return filepath.Join(ruleSetDirectory(), singBoxRuleSetManifestName)
}

// readSingBoxRuleSetManifest accepts every version written by an older release.
// Unknown-newer or unreadable manifests are rejected: the caller then behaves as
// if nothing is declared, which is the safe direction for references.
func readSingBoxRuleSetManifest() (singBoxRuleSetManifest, error) {
	data, err := os.ReadFile(ruleSetManifestPath())
	if err != nil {
		return singBoxRuleSetManifest{}, err
	}
	var manifest singBoxRuleSetManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return singBoxRuleSetManifest{}, errors.New("sing-box 规则集清单无效")
	}
	if manifest.Version < 1 || manifest.Version > singBoxRuleSetManifestVersion {
		return singBoxRuleSetManifest{}, errors.New("sing-box 规则集清单无效")
	}
	return manifest, nil
}

// declaredExternalRuleSets keeps the sets the running configuration can resolve.
// A fresh plan declares every live set itself, but a refresh only recomposes file
// contents: referencing a tag the pinned main configuration never declared would
// leave sing-box watching a rule-set it never loaded. A manifest older than the
// external-set field carries no declaration list, which reads as "none declared"
// and surfaces through the restart prompt instead of a silent reference.
func declaredExternalRuleSets(directory string, sets []RuleSet, declaresItself bool) []RuleSet {
	live := liveExternalRuleSets(directory, sets)
	if declaresItself {
		return live
	}
	manifest, err := readSingBoxRuleSetManifest()
	if err != nil {
		return nil
	}
	declared := make(map[string]struct{}, len(manifest.ExternalSets))
	for _, record := range manifest.ExternalSets {
		declared[record.Tag] = struct{}{}
	}
	kept := make([]RuleSet, 0, len(live))
	for _, set := range live {
		tag, _ := externalRuleSetBinding(set)
		if _, exists := declared[tag]; exists {
			kept = append(kept, set)
		}
	}
	return kept
}

// writeSingBoxRuleSetPlan materializes the mutable part of the TUN routing
// configuration as local source rule-sets. sing-box watches these files and
// reloads them after an atomic replacement, so the pinned main configuration
// and the TUN process can remain unchanged while user rules are edited.
//
// External rule-set files are published by ingestion, never rewritten here; this
// function only declares and references the ones that already have a file.
func writeSingBoxRuleSetPlan(rules []RoutingRule, sets []RuleSet, outbounds []string, usesFakeIP bool) (singBoxRuleSetPlan, error) {
	singBoxRuleSetMu.Lock()
	defer singBoxRuleSetMu.Unlock()
	plan, _, err := writeSingBoxRuleSetPlanLocked(rules, sets, outbounds, true, usesFakeIP, replaceSingBoxRuleSet)
	return plan, err
}

func writeSingBoxRuleSetPlanLocked(
	rules []RoutingRule,
	sets []RuleSet,
	outbounds []string,
	writeManifest bool,
	usesFakeIP bool,
	replace func(string, []byte, os.FileMode) error,
) (singBoxRuleSetPlan, func() error, error) {
	directory := ruleSetDirectory()
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return singBoxRuleSetPlan{}, nil, fmt.Errorf("创建 sing-box 规则集目录失败：%w", err)
	}
	live, payloads, err := loadExternalRuleSetPayloads(directory, sets, writeManifest)
	if err != nil {
		return singBoxRuleSetPlan{}, nil, err
	}
	outbounds = normalizedRuleSetOutbounds(outbounds, rules, live)
	bindings := buildSingBoxRuleSetBindings(directory, outbounds)
	candidates := manualRoutingCandidates(rules)
	sortCandidates(candidates)
	external := sortedExternalRuleSets(live)
	plan := singBoxRuleSetPlan{
		Definitions:        make([]any, 0, len(bindings)+len(live)),
		EarlyRouteRules:    make([]any, 0, len(outbounds)),
		UserRouteRules:     make([]any, 0, len(outbounds)*3),
		ExternalRouteRules: make([]any, 0, len(live)),
	}
	files := make([]ruleSetFile, 0, len(bindings)+len(live)+1)
	for _, binding := range bindings {
		sourceRules := buildSingBoxSourceRules(candidates, binding)
		payload, err := json.MarshalIndent(map[string]any{
			"version": singBoxRuleSetVersion,
			"rules":   sourceRules,
		}, "", "  ")
		if err != nil {
			return singBoxRuleSetPlan{}, nil, fmt.Errorf("编码 sing-box 规则集失败：%w", err)
		}
		payload = append(payload, '\n')
		files = append(files, ruleSetFile{Path: binding.Path, Data: payload, Mode: 0o600})
		plan.Definitions = append(plan.Definitions, map[string]any{
			"type": "local", "tag": binding.Tag, "format": "source", "path": binding.Path,
		})
		reference := map[string]any{
			"rule_set": []string{binding.Tag}, "outbound": binding.Outbound,
		}
		if binding.Outbound == OutboundReject {
			delete(reference, "outbound")
			reference["action"] = "reject"
		}
		if binding.Scope == ruleSetScopeEarlyIP {
			plan.EarlyRouteRules = append(plan.EarlyRouteRules, reference)
		} else if binding.Scope == ruleSetScopeCustomIP {
			// Explicit IP priorities must also take precedence over proxy bypass.
			plan.PriorityRuleSets = append(plan.PriorityRuleSets, binding.Tag)
		} else {
			plan.UserRouteRules = append(plan.UserRouteRules, reference)
			if binding.Scope == ruleSetScopeProcess || binding.Scope == ruleSetScopeDomain {
				plan.PriorityRuleSets = append(plan.PriorityRuleSets, binding.Tag)
			}
		}
	}
	// The watched external files are recomposed from the ingested payloads, never
	// rewritten from the subscription itself, so manual-rule carve-outs stay in
	// sync with every routing edit without a refetch. A rule-set file cannot
	// reference another rule-set (sing-box rejects the recursion), which is why
	// the carve-outs are materialized as concrete headless rules and the
	// rule_set references live only in the pinned main configuration.
	for index, set := range external {
		tag, path := externalRuleSetBinding(set)
		payload, err := composeExternalRuleSetFile(set, external[:index], candidates, payloads)
		if err != nil {
			return singBoxRuleSetPlan{}, nil, err
		}
		files = append(files, ruleSetFile{Path: path, Data: payload, Mode: 0o600})
		plan.Definitions = append(plan.Definitions, map[string]any{
			"type": "local", "tag": tag, "format": "source", "path": path,
		})
		plan.ExternalRouteRules = append(plan.ExternalRouteRules, map[string]any{
			"rule_set": []any{tag}, "outbound": set.Outbound,
		})
		// Subscribed lists carry user intent the same way manual rules do, so the
		// compatibility bypass must yield to them as well.
		plan.PriorityRuleSets = append(plan.PriorityRuleSets, tag)
	}
	if writeManifest {
		manifest, err := json.MarshalIndent(singBoxRuleSetManifest{
			Version: singBoxRuleSetManifestVersion, Outbounds: outbounds, UsesFakeIP: usesFakeIP,
			CustomPriority: true, ExternalSets: externalRuleSetRecords(external),
		}, "", "  ")
		if err != nil {
			return singBoxRuleSetPlan{}, nil, fmt.Errorf("编码 sing-box 规则集清单失败：%w", err)
		}
		manifest = append(manifest, '\n')
		files = append(files, ruleSetFile{
			Path: filepath.Join(directory, singBoxRuleSetManifestName), Data: manifest, Mode: 0o600,
		})
		cleanupOrphanExternalRuleSets(directory, external)
	}
	rollback, err := publishRuleSetFiles(files, replace)
	if err != nil {
		return singBoxRuleSetPlan{}, nil, err
	}
	return plan, rollback, nil
}

// loadExternalRuleSetPayloads reads the normalized subscription payloads of every
// set that can take effect: enabled, declared in the running configuration when
// recomposing, and with a readable source file. A set whose payload disappeared
// or became unreadable silently drops out of the plan; the manifest and restart
// check then surface the gap instead of emitting a dangling reference.
func loadExternalRuleSetPayloads(directory string, sets []RuleSet, declaresItself bool) ([]RuleSet, map[string][]map[string]any, error) {
	live := declaredExternalRuleSets(directory, sets, declaresItself)
	payloads := make(map[string][]map[string]any, len(live))
	kept := make([]RuleSet, 0, len(live))
	for _, set := range live {
		data, err := os.ReadFile(externalRuleSetSourcePathIn(directory, set))
		if err != nil {
			continue
		}
		rules, err := decodeExternalRuleSetPayload(data)
		if err != nil {
			continue
		}
		kept = append(kept, set)
		payloads[set.ID] = rules
	}
	return kept, payloads, nil
}

// composeExternalRuleSetFile merges the ingested subscription entries with the
// concrete matches that outrank the set: manual rules of other outbounds that
// sort ahead of it, and the entries of higher-ranked subscribed lists. The first
// match wins at runtime, so without these carve-outs a broad list would swallow
// its own exceptions.
func composeExternalRuleSetFile(
	set RuleSet,
	higherRanked []RuleSet,
	manual []routingCandidate,
	payloads map[string][]map[string]any,
) ([]byte, error) {
	exclusions := []any{}
	for _, candidate := range manual {
		if candidate.Disabled || candidate.Outbound == set.Outbound {
			continue
		}
		// Same-scope matches only need to rank ahead; an equal-priority domain or
		// process rule is the more specific statement of intent. IP matches only
		// exclude on a strictly higher priority: at equal priority the domain
		// scope already ranks ahead, and an equal-priority CIDR rule inverted
		// into a domain list would swallow fake-IP destinations wholesale.
		ranksAhead := candidate.Priority >= set.Priority
		if candidate.MatchType == MatchIP {
			ranksAhead = candidate.Priority > set.Priority
		}
		if !ranksAhead {
			continue
		}
		exclusions = append(exclusions, singBoxHeadlessRule(candidate.rule))
	}
	for _, other := range higherRanked {
		if other.Outbound == set.Outbound {
			continue
		}
		for _, rule := range payloads[other.ID] {
			exclusions = append(exclusions, rule)
		}
	}
	sourceRules := make([]any, 0, len(payloads[set.ID]))
	for _, rule := range payloads[set.ID] {
		if len(exclusions) == 0 {
			sourceRules = append(sourceRules, rule)
			continue
		}
		sourceRules = append(sourceRules, map[string]any{
			"type": "logical", "mode": "and", "rules": []any{
				rule,
				map[string]any{"type": "logical", "mode": "or", "rules": exclusions, "invert": true},
			},
		})
	}
	payload, err := json.MarshalIndent(map[string]any{
		"version": singBoxRuleSetVersion,
		"rules":   sourceRules,
	}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("编码外部规则集失败：%w", err)
	}
	return append(payload, '\n'), nil
}

// sortedExternalRuleSets orders the live sets the way the plan references them:
// by descending priority, ties broken by name so the output stays stable.
func sortedExternalRuleSets(sets []RuleSet) []RuleSet {
	sorted := append([]RuleSet(nil), sets...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Priority != sorted[j].Priority {
			return sorted[i].Priority > sorted[j].Priority
		}
		return strings.ToLower(sorted[i].Name) < strings.ToLower(sorted[j].Name)
	})
	return sorted
}

// cleanupOrphanExternalRuleSets removes ingested payloads and watched files of
// sets the configuration no longer lists. It only runs while writing a fresh
// manifest, i.e. before an engine start republishes the configuration, so a
// running sing-box never loses a file it is still watching.
func cleanupOrphanExternalRuleSets(directory string, sets []RuleSet) {
	keep := make(map[string]struct{}, len(sets)*2)
	for _, set := range sets {
		tag, path := externalRuleSetBinding(set)
		keep[path] = struct{}{}
		keep[externalRuleSetSourcePathIn(directory, set)] = struct{}{}
		_ = tag
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "ext-") ||
			(!strings.HasSuffix(name, ".json") && !strings.HasSuffix(name, ".source.json")) {
			continue
		}
		if _, ok := keep[filepath.Join(directory, name)]; ok {
			continue
		}
		_ = os.Remove(filepath.Join(directory, name))
	}
}

// refreshSingBoxRuleSets updates the files referenced by the currently active
// TUN configuration. If TUN has never been started there is no manifest and the
// next start will create the rule-sets from the persisted settings.
func refreshSingBoxRuleSets(rules []RoutingRule, sets []RuleSet) error {
	return refreshSingBoxRuleSetsAndCommit(rules, sets, func() error { return nil })
}

// refreshSingBoxRuleSetsAndCommit keeps the live rule-sets and persisted
// settings aligned. A failed replacement or commit restores every published
// file before returning the error.
func refreshSingBoxRuleSetsAndCommit(rules []RoutingRule, sets []RuleSet, commit func() error) error {
	singBoxRuleSetMu.Lock()
	defer singBoxRuleSetMu.Unlock()
	manifest, err := readSingBoxRuleSetManifest()
	if errors.Is(err, os.ErrNotExist) {
		return commit()
	}
	if err != nil {
		return fmt.Errorf("读取 sing-box 规则集清单失败：%w", err)
	}
	_, rollback, err := writeSingBoxRuleSetPlanLocked(
		rules, sets, manifest.Outbounds, false, manifest.UsesFakeIP, replaceSingBoxRuleSet,
	)
	if err != nil {
		return err
	}
	if err := commit(); err != nil {
		if rollbackErr := rollback(); rollbackErr != nil {
			return fmt.Errorf("%w；恢复 sing-box 规则集失败：%v", err, rollbackErr)
		}
		return err
	}
	return nil
}

func publishRuleSetFiles(
	files []ruleSetFile,
	replace func(string, []byte, os.FileMode) error,
) (func() error, error) {
	snapshots := make([]ruleSetFileSnapshot, 0, len(files))
	for _, file := range files {
		snapshot := ruleSetFileSnapshot{Path: file.Path, Mode: file.Mode}
		data, err := os.ReadFile(file.Path)
		if err == nil {
			snapshot.Data = data
			snapshot.Existed = true
			if info, statErr := os.Stat(file.Path); statErr == nil {
				snapshot.Mode = info.Mode().Perm()
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("读取现有 sing-box 规则集失败：%w", err)
		}
		snapshots = append(snapshots, snapshot)
	}

	for index, file := range files {
		if err := replace(file.Path, file.Data, file.Mode); err != nil {
			if rollbackErr := rollbackSnapshots(snapshots[:index]); rollbackErr != nil {
				return nil, fmt.Errorf("更新 sing-box 规则集 %s 失败：%w；回滚失败：%v", filepath.Base(file.Path), err, rollbackErr)
			}
			return nil, fmt.Errorf("更新 sing-box 规则集 %s 失败：%w", filepath.Base(file.Path), err)
		}
	}
	return func() error { return rollbackSnapshots(snapshots) }, nil
}

func rollbackSnapshots(snapshots []ruleSetFileSnapshot) error {
	var failures []error
	for index := len(snapshots) - 1; index >= 0; index-- {
		snapshot := snapshots[index]
		var err error
		if snapshot.Existed {
			err = replaceFileAtomically(snapshot.Path, snapshot.Data, snapshot.Mode)
		} else {
			err = os.Remove(snapshot.Path)
			if errors.Is(err, os.ErrNotExist) {
				err = nil
			}
		}
		if err != nil {
			failures = append(failures, fmt.Errorf("恢复 %s 失败：%w", filepath.Base(snapshot.Path), err))
		}
	}
	return errors.Join(failures...)
}

func singBoxRuleSetRestartRequirement(rules []RoutingRule, sets []RuleSet) (bool, string) {
	singBoxRuleSetMu.Lock()
	defer singBoxRuleSetMu.Unlock()
	manifest, err := readSingBoxRuleSetManifest()
	if err != nil {
		return false, ""
	}
	live := liveExternalRuleSets(ruleSetDirectory(), sets)
	// An older manifest carries no declaration list, so any enabled list is a
	// change we cannot prove is already in the running configuration. Priority
	// edits alone stay hot — they only reorder content-level carve-outs — while
	// membership and outbound bindings change the pinned route rules.
	if !sameExternalRuleSetRecords(manifest.ExternalSets, externalRuleSetRecords(live)) {
		return true, "external_ruleset_changed"
	}
	available := make(map[string]struct{}, len(manifest.Outbounds))
	for _, outbound := range manifest.Outbounds {
		available[outbound] = struct{}{}
	}
	for _, rule := range rules {
		if rule.Disabled {
			continue
		}
		if rule.Priority > 0 && !manifest.CustomPriority {
			return true, "priority_changed"
		}
		if _, ok := available[rule.Outbound]; !ok {
			return true, "outbound_changed"
		}
	}
	if !manifest.UsesFakeIP {
		// Every external rule-set matches domains, so one that is live needs the
		// same FakeIP resolution an enabled domain rule needs.
		if len(live) > 0 {
			return true, "enable_fakeip"
		}
		for _, rule := range rules {
			if rule.Disabled {
				continue
			}
			if rule.MatchType == MatchDomain {
				return true, "enable_fakeip"
			}
		}
	}
	return false, ""
}

func normalizedRuleSetOutbounds(outbounds []string, rules []RoutingRule, sets []RuleSet) []string {
	seen := make(map[string]struct{}, len(outbounds)+len(rules))
	result := make([]string, 0, len(outbounds)+len(rules))
	for _, outbound := range outbounds {
		outbound = strings.TrimSpace(outbound)
		if outbound == "" {
			continue
		}
		if _, exists := seen[outbound]; exists {
			continue
		}
		seen[outbound] = struct{}{}
		result = append(result, outbound)
	}
	// A rule can reference an adapter selected immediately before a save. Keep
	// its file ready even if an older active manifest did not list it; it will be
	// referenced after the normal engine restart that applies adapter changes.
	missing := []string{}
	for _, rule := range rules {
		if rule.Disabled {
			continue
		}
		if _, exists := seen[rule.Outbound]; exists {
			continue
		}
		seen[rule.Outbound] = struct{}{}
		missing = append(missing, rule.Outbound)
	}
	// A subscribed list is bound to one outbound as a whole, so it reserves a
	// file the same way an individual rule does.
	for _, set := range sets {
		if _, exists := seen[set.Outbound]; exists {
			continue
		}
		seen[set.Outbound] = struct{}{}
		missing = append(missing, set.Outbound)
	}
	sort.Strings(missing)
	return append(result, missing...)
}

func buildSingBoxRuleSetBindings(directory string, outbounds []string) []singBoxRuleSetBinding {
	bindings := make([]singBoxRuleSetBinding, 0, len(outbounds)*3)
	for _, scope := range []string{ruleSetScopeProcess, ruleSetScopeDomain, ruleSetScopeIP, ruleSetScopeCustomIP} {
		for _, outbound := range outbounds {
			bindings = append(bindings, newSingBoxRuleSetBinding(directory, scope, outbound))
		}
	}
	// These sets are only used inside the process-scoped compatibility rules.
	// Keep stable files for every adapter so later edits can be hot-reloaded.
	early := make([]singBoxRuleSetBinding, 0, len(outbounds))
	for _, outbound := range outbounds {
		if strings.HasPrefix(outbound, "nic_") {
			early = append(early, newSingBoxRuleSetBinding(directory, ruleSetScopeEarlyIP, outbound))
		}
	}
	return append(early, bindings...)
}

func newSingBoxRuleSetBinding(directory, scope, outbound string) singBoxRuleSetBinding {
	digest := sha256.Sum256([]byte(scope + "\x00" + outbound))
	suffix := hex.EncodeToString(digest[:8])
	return singBoxRuleSetBinding{
		Scope: scope, Outbound: outbound,
		Tag:  "hypomux-" + scope + "-" + suffix,
		Path: filepath.Join(directory, scope+"-"+suffix+".json"),
	}
}

func buildSingBoxSourceRules(candidates []routingCandidate, binding singBoxRuleSetBinding) []any {
	matchType := binding.Scope
	if binding.Scope == ruleSetScopeEarlyIP || binding.Scope == ruleSetScopeCustomIP {
		matchType = MatchIP
		// Include other outbounds when computing exclusions: a more specific
		// direct/aggregation rule must not be swallowed by an adapter catch-all.
	}
	result := []any{}
	for index, candidate := range candidates {
		if binding.Scope == ruleSetScopeCustomIP && candidate.Priority == 0 {
			continue
		}
		if candidate.Disabled || candidate.MatchType != matchType || candidate.Outbound != binding.Outbound {
			continue
		}
		base := singBoxHeadlessRule(candidate.rule)
		exclusions := []any{}
		// Stable per-type files keep hot reload possible. Exclude higher-priority
		// matches so fixed route-reference order cannot override user priority.
		for _, earlier := range candidates[:index] {
			if !earlier.Disabled && earlier.Outbound != candidate.Outbound &&
				((earlier.MatchType != candidate.MatchType && earlier.Priority > candidate.Priority) ||
					routingRulesCanOverlap(earlier.rule, candidate.rule)) {
				exclusions = append(exclusions, singBoxHeadlessRule(earlier.rule))
			}
		}
		if len(exclusions) == 0 {
			result = append(result, base)
			continue
		}
		result = append(result, map[string]any{
			"type": "logical", "mode": "and", "rules": []any{
				base,
				map[string]any{"type": "logical", "mode": "or", "rules": exclusions, "invert": true},
			},
		})
	}
	return result
}

// routingCandidate is the compile-time view of one manual routing decision. The
// flat fields above are the comparison view; rule keeps the full persisted
// record so ordering can never drop a field a later RoutingRule change adds.
type routingCandidate struct {
	MatchType string
	Value     string
	Outbound  string
	Priority  int
	Disabled  bool
	rule      RoutingRule
}

func manualRoutingCandidates(rules []RoutingRule) []routingCandidate {
	candidates := make([]routingCandidate, 0, len(rules))
	for _, rule := range rules {
		candidates = append(candidates, routingCandidate{
			MatchType: rule.MatchType, Value: rule.Value, Outbound: rule.Outbound,
			Priority: rule.Priority, Disabled: rule.Disabled, rule: rule,
		})
	}
	return candidates
}

// sortCandidates is the single ordering authority for rule-set compilation: the
// first match wins at runtime, so an exception must sort ahead of the broader
// match it carves out of.
func sortCandidates(candidates []routingCandidate) {
	rank := map[string]int{MatchProcess: 0, MatchDomain: 1, MatchIP: 2}
	sort.SliceStable(candidates, func(i, j int) bool {
		left, right := candidates[i], candidates[j]
		if left.Priority != right.Priority {
			return left.Priority > right.Priority
		}
		if rank[left.MatchType] != rank[right.MatchType] {
			return rank[left.MatchType] < rank[right.MatchType]
		}
		if left.MatchType == MatchDomain {
			// A child domain is an exception to its parent, just as a narrower
			// CIDR is an exception to a broader IP rule. Sort it first before
			// rule-set generation computes exclusions between outbounds.
			leftDepth := strings.Count(strings.TrimPrefix(left.Value, "."), ".")
			rightDepth := strings.Count(strings.TrimPrefix(right.Value, "."), ".")
			if leftDepth != rightDepth {
				return leftDepth > rightDepth
			}
		}
		if left.MatchType == MatchIP {
			_, leftNetwork, _ := net.ParseCIDR(left.Value)
			_, rightNetwork, _ := net.ParseCIDR(right.Value)
			leftBits, _ := leftNetwork.Mask.Size()
			rightBits, _ := rightNetwork.Mask.Size()
			if leftBits != rightBits {
				return leftBits > rightBits
			}
		}
		return strings.ToLower(left.Value) < strings.ToLower(right.Value)
	})
}

func singBoxHeadlessRule(rule RoutingRule) map[string]any {
	entry := map[string]any{}
	switch rule.MatchType {
	case MatchProcess:
		entry["process_name"] = []string{rule.Value}
	case MatchDomain:
		entry["domain"] = []string{rule.Value}
		entry["domain_suffix"] = []string{"." + strings.TrimPrefix(rule.Value, ".")}
	case MatchIP:
		entry["ip_cidr"] = []string{rule.Value}
	}
	return entry
}

func routingRulesCanOverlap(left, right RoutingRule) bool {
	if left.MatchType != right.MatchType {
		return false
	}
	switch left.MatchType {
	case MatchProcess:
		return strings.EqualFold(left.Value, right.Value)
	case MatchDomain:
		leftValue := strings.TrimPrefix(strings.ToLower(left.Value), ".")
		rightValue := strings.TrimPrefix(strings.ToLower(right.Value), ".")
		return leftValue == rightValue || strings.HasSuffix(leftValue, "."+rightValue) ||
			strings.HasSuffix(rightValue, "."+leftValue)
	case MatchIP:
		leftIP, leftNetwork, leftErr := net.ParseCIDR(left.Value)
		rightIP, rightNetwork, rightErr := net.ParseCIDR(right.Value)
		return leftErr == nil && rightErr == nil &&
			(leftNetwork.Contains(rightIP) || rightNetwork.Contains(leftIP))
	default:
		return false
	}
}

func replaceFileAtomically(path string, data []byte, mode os.FileMode) error {
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, data, mode); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}
