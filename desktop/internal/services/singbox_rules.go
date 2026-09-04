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
	singBoxRuleSetManifestName = "sing-box-rule-sets.json"
	singBoxRuleSetVersion      = 3

	ruleSetScopeEarlyIP = "early-ip"
	ruleSetScopeProcess = "process"
	ruleSetScopeDomain  = "domain"
	ruleSetScopeIP      = "ip"
)

var singBoxRuleSetMu sync.Mutex

type singBoxRuleSetManifest struct {
	Version   int      `json:"version"`
	Outbounds []string `json:"outbounds"`
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
func writeSingBoxRuleSetPlan(rules []RoutingRule, outbounds []string) (singBoxRuleSetPlan, error) {
	singBoxRuleSetMu.Lock()
	defer singBoxRuleSetMu.Unlock()
	return writeSingBoxRuleSetPlanLocked(rules, outbounds, true)
}

func writeSingBoxRuleSetPlanLocked(
	rules []RoutingRule,
	outbounds []string,
	writeManifest bool,
) (singBoxRuleSetPlan, error) {
	directory := filepath.Join(settingsDirectory(), "runtime", "rule-sets")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return singBoxRuleSetPlan{}, fmt.Errorf("创建 sing-box 规则集目录失败：%w", err)
	}
	outbounds = normalizedRuleSetOutbounds(outbounds, rules)
	bindings := buildSingBoxRuleSetBindings(directory, outbounds)
	plan := singBoxRuleSetPlan{
		Definitions:     make([]any, 0, len(bindings)),
		EarlyRouteRules: make([]any, 0, len(outbounds)),
		UserRouteRules:  make([]any, 0, len(outbounds)*3),
	}
	for _, binding := range bindings {
		sourceRules := buildSingBoxSourceRules(rules, binding)
		payload, err := json.MarshalIndent(map[string]any{
			"version": singBoxRuleSetVersion,
			"rules":   sourceRules,
		}, "", "  ")
		if err != nil {
			return singBoxRuleSetPlan{}, fmt.Errorf("编码 sing-box 规则集失败：%w", err)
		}
		payload = append(payload, '\n')
		if err := replaceFileAtomically(binding.Path, payload, 0o600); err != nil {
			return singBoxRuleSetPlan{}, fmt.Errorf("更新 sing-box 规则集 %s 失败：%w", binding.Tag, err)
		}
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
			Version: 1, Outbounds: outbounds,
		}, "", "  ")
		if err != nil {
			return singBoxRuleSetPlan{}, fmt.Errorf("编码 sing-box 规则集清单失败：%w", err)
		}
		manifest = append(manifest, '\n')
		if err := replaceFileAtomically(
			filepath.Join(directory, singBoxRuleSetManifestName), manifest, 0o600,
		); err != nil {
			return singBoxRuleSetPlan{}, fmt.Errorf("更新 sing-box 规则集清单失败：%w", err)
		}
	}
	return plan, nil
}

// refreshSingBoxRuleSets updates the files referenced by the currently active
// TUN configuration. If TUN has never been started there is no manifest and the
// next start will create the rule-sets from the persisted settings.
func refreshSingBoxRuleSets(rules []RoutingRule) error {
	singBoxRuleSetMu.Lock()
	defer singBoxRuleSetMu.Unlock()
	directory := filepath.Join(settingsDirectory(), "runtime", "rule-sets")
	data, err := os.ReadFile(filepath.Join(directory, singBoxRuleSetManifestName))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("读取 sing-box 规则集清单失败：%w", err)
	}
	var manifest singBoxRuleSetManifest
	if err := json.Unmarshal(data, &manifest); err != nil || manifest.Version != 1 {
		return fmt.Errorf("sing-box 规则集清单无效")
	}
	_, err = writeSingBoxRuleSetPlanLocked(rules, manifest.Outbounds, false)
	return err
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
