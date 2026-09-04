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
	singBoxRuleSetManifestVersion = 2
	singBoxRuleSetVersion         = 3

	ruleSetScopeEarlyIP = "early-ip"
	ruleSetScopeProcess = "process"
	ruleSetScopeDomain  = "domain"
	ruleSetScopeIP      = "ip"
)

var (
	singBoxRuleSetMu      sync.Mutex
	replaceSingBoxRuleSet = replaceFileAtomically
)

type singBoxRuleSetManifest struct {
	Version    int      `json:"version"`
	Outbounds  []string `json:"outbounds"`
	UsesFakeIP bool     `json:"uses_fakeip"`
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
	Definitions     []any
	EarlyRouteRules []any
	UserRouteRules  []any
}

// writeSingBoxRuleSetPlan materializes the mutable part of the TUN routing
// configuration as local source rule-sets. sing-box watches these files and
// reloads them after an atomic replacement, so the pinned main configuration
// and the TUN process can remain unchanged while user rules are edited.
func writeSingBoxRuleSetPlan(rules []RoutingRule, outbounds []string, usesFakeIP bool) (singBoxRuleSetPlan, error) {
	singBoxRuleSetMu.Lock()
	defer singBoxRuleSetMu.Unlock()
	plan, _, err := writeSingBoxRuleSetPlanLocked(rules, outbounds, true, usesFakeIP, replaceSingBoxRuleSet)
	return plan, err
}

func writeSingBoxRuleSetPlanLocked(
	rules []RoutingRule,
	outbounds []string,
	writeManifest bool,
	usesFakeIP bool,
	replace func(string, []byte, os.FileMode) error,
) (singBoxRuleSetPlan, func() error, error) {
	directory := filepath.Join(settingsDirectory(), "runtime", "rule-sets")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return singBoxRuleSetPlan{}, nil, fmt.Errorf("创建 sing-box 规则集目录失败：%w", err)
	}
	outbounds = normalizedRuleSetOutbounds(outbounds, rules)
	bindings := buildSingBoxRuleSetBindings(directory, outbounds)
	plan := singBoxRuleSetPlan{
		Definitions:     make([]any, 0, len(bindings)),
		EarlyRouteRules: make([]any, 0, len(outbounds)),
		UserRouteRules:  make([]any, 0, len(outbounds)*3),
	}
	files := make([]ruleSetFile, 0, len(bindings)+1)
	for _, binding := range bindings {
		sourceRules := buildSingBoxSourceRules(rules, binding)
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
		if binding.Scope == ruleSetScopeEarlyIP {
			plan.EarlyRouteRules = append(plan.EarlyRouteRules, reference)
		} else {
			plan.UserRouteRules = append(plan.UserRouteRules, reference)
		}
	}
	if writeManifest {
		manifest, err := json.MarshalIndent(singBoxRuleSetManifest{
			Version: singBoxRuleSetManifestVersion, Outbounds: outbounds, UsesFakeIP: usesFakeIP,
		}, "", "  ")
		if err != nil {
			return singBoxRuleSetPlan{}, nil, fmt.Errorf("编码 sing-box 规则集清单失败：%w", err)
		}
		manifest = append(manifest, '\n')
		files = append(files, ruleSetFile{
			Path: filepath.Join(directory, singBoxRuleSetManifestName), Data: manifest, Mode: 0o600,
		})
	}
	rollback, err := publishRuleSetFiles(files, replace)
	if err != nil {
		return singBoxRuleSetPlan{}, nil, err
	}
	return plan, rollback, nil
}

// refreshSingBoxRuleSets updates the files referenced by the currently active
// TUN configuration. If TUN has never been started there is no manifest and the
// next start will create the rule-sets from the persisted settings.
func refreshSingBoxRuleSets(rules []RoutingRule) error {
	return refreshSingBoxRuleSetsAndCommit(rules, func() error { return nil })
}

// refreshSingBoxRuleSetsAndCommit keeps the live rule-sets and persisted
// settings aligned. A failed replacement or commit restores every published
// file before returning the error.
func refreshSingBoxRuleSetsAndCommit(rules []RoutingRule, commit func() error) error {
	singBoxRuleSetMu.Lock()
	defer singBoxRuleSetMu.Unlock()
	directory := filepath.Join(settingsDirectory(), "runtime", "rule-sets")
	data, err := os.ReadFile(filepath.Join(directory, singBoxRuleSetManifestName))
	if errors.Is(err, os.ErrNotExist) {
		return commit()
	}
	if err != nil {
		return fmt.Errorf("读取 sing-box 规则集清单失败：%w", err)
	}
	var manifest singBoxRuleSetManifest
	if err := json.Unmarshal(data, &manifest); err != nil ||
		(manifest.Version != 1 && manifest.Version != singBoxRuleSetManifestVersion) {
		return fmt.Errorf("sing-box 规则集清单无效")
	}
	_, rollback, err := writeSingBoxRuleSetPlanLocked(
		rules, manifest.Outbounds, false, manifest.UsesFakeIP, replaceSingBoxRuleSet,
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

func singBoxRuleSetRestartRequirement(rules []RoutingRule) (bool, string) {
	singBoxRuleSetMu.Lock()
	defer singBoxRuleSetMu.Unlock()
	directory := filepath.Join(settingsDirectory(), "runtime", "rule-sets")
	data, err := os.ReadFile(filepath.Join(directory, singBoxRuleSetManifestName))
	if err != nil {
		return false, ""
	}
	var manifest singBoxRuleSetManifest
	if json.Unmarshal(data, &manifest) != nil || manifest.Version != singBoxRuleSetManifestVersion {
		return false, ""
	}
	available := make(map[string]struct{}, len(manifest.Outbounds))
	for _, outbound := range manifest.Outbounds {
		available[outbound] = struct{}{}
	}
	for _, rule := range rules {
		if _, ok := available[rule.Outbound]; !ok {
			return true, "outbound_changed"
		}
	}
	if !manifest.UsesFakeIP {
		for _, rule := range rules {
			if rule.MatchType == MatchDomain {
				return true, "enable_fakeip"
			}
		}
	}
	return false, ""
}

func normalizedRuleSetOutbounds(outbounds []string, rules []RoutingRule) []string {
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
		if _, exists := seen[rule.Outbound]; exists {
			continue
		}
		seen[rule.Outbound] = struct{}{}
		missing = append(missing, rule.Outbound)
	}
	sort.Strings(missing)
	return append(result, missing...)
}

func buildSingBoxRuleSetBindings(directory string, outbounds []string) []singBoxRuleSetBinding {
	bindings := make([]singBoxRuleSetBinding, 0, len(outbounds)*3)
	for _, scope := range []string{ruleSetScopeProcess, ruleSetScopeDomain, ruleSetScopeIP} {
		for _, outbound := range outbounds {
			bindings = append(bindings, newSingBoxRuleSetBinding(directory, scope, outbound))
		}
	}
	// Literal adapter pinning must be evaluated before the third-party proxy
	// compatibility bypass. Keep it in its own hot-reloadable rule-set layer.
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

func buildSingBoxSourceRules(rules []RoutingRule, binding singBoxRuleSetBinding) []any {
	candidates := rules
	matchType := binding.Scope
	if binding.Scope == ruleSetScopeEarlyIP {
		matchType = MatchIP
		candidates = make([]RoutingRule, 0, len(rules))
		for _, rule := range rules {
			if rule.MatchType == MatchIP && strings.HasPrefix(rule.Outbound, "nic_") {
				candidates = append(candidates, rule)
			}
		}
	}
	result := []any{}
	for index, rule := range candidates {
		if rule.MatchType != matchType || rule.Outbound != binding.Outbound {
			continue
		}
		base := singBoxHeadlessRule(rule)
		exclusions := []any{}
		for _, earlier := range candidates[:index] {
			if earlier.MatchType == rule.MatchType && earlier.Outbound != rule.Outbound &&
				routingRulesCanOverlap(earlier, rule) {
				exclusions = append(exclusions, singBoxHeadlessRule(earlier))
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
