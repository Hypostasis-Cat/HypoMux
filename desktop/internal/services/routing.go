package services

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Hypostasis-Cat/HypoMux/desktop/internal/platform"
	"golang.org/x/net/idna"
)

const (
	MatchProcess = "process"
	MatchDomain  = "domain"
	MatchIP      = "ip"

	RoutingBackupFormat  = "hypomux-routing-rules"
	RoutingBackupVersion = 2
)

type RoutingRule struct {
	MatchType string `json:"match_type"`
	Value     string `json:"value"`
	Outbound  string `json:"outbound"`
}

func (r *RoutingRule) UnmarshalJSON(data []byte) error {
	rules, err := expandRoutingRule(data)
	if err != nil {
		return err
	}
	if len(rules) != 1 {
		return fmt.Errorf("一条 JSON 规则包含 %d 个匹配值；请通过旧版规则迁移器展开", len(rules))
	}
	*r = rules[0]
	return nil
}

func expandRoutingRule(data []byte) ([]RoutingRule, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	var matchType, outbound string
	_ = json.Unmarshal(raw["match_type"], &matchType)
	_ = json.Unmarshal(raw["outbound"], &outbound)
	if matchType == "" {
		present := []string{}
		if raw["process_name"] != nil {
			present = append(present, MatchProcess)
		}
		if raw["domain"] != nil || raw["domain_suffix"] != nil {
			present = append(present, MatchDomain)
		}
		if raw["ip_cidr"] != nil || raw["ip"] != nil {
			present = append(present, MatchIP)
		}
		if len(present) != 1 {
			return nil, fmt.Errorf("旧版规则必须且只能包含一种匹配类型")
		}
		matchType = present[0]
	}
	matchType = canonicalMatchType(matchType)
	values := []string{}
	if payload := raw["value"]; payload != nil {
		var value string
		if err := json.Unmarshal(payload, &value); err != nil {
			return nil, fmt.Errorf("规则 value 必须是字符串")
		}
		values = append(values, value)
	} else {
		fields := map[string][]string{
			MatchProcess: {"process_name"},
			MatchDomain:  {"domain", "domain_suffix"},
			MatchIP:      {"ip_cidr", "ip"},
		}[matchType]
		for _, field := range fields {
			payload := raw[field]
			if payload == nil {
				continue
			}
			var many []string
			if err := json.Unmarshal(payload, &many); err == nil {
				values = append(values, many...)
				continue
			}
			var one string
			if err := json.Unmarshal(payload, &one); err != nil {
				return nil, fmt.Errorf("规则字段 %s 必须是字符串或字符串数组", field)
			}
			values = append(values, one)
		}
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("规则没有匹配值")
	}
	rules := make([]RoutingRule, 0, len(values))
	for _, value := range values {
		rules = append(rules, RoutingRule{
			MatchType: matchType,
			Value:     value,
			Outbound:  strings.TrimSpace(outbound),
		})
	}
	return rules, nil
}

type RoutingValidation struct {
	Valid     bool        `json:"valid"`
	Rule      RoutingRule `json:"rule"`
	Message   string      `json:"message,omitempty"`
	Duplicate bool        `json:"duplicate"`
}

type RoutingSnapshot struct {
	Rules     []RoutingRule `json:"rules"`
	Outbounds []Outbound    `json:"outbounds"`
}

type Outbound struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type RoutingRuleService struct {
	settings *SettingsService
	adapters *AdapterService
	desktop  platform.DesktopHost
}

func NewRoutingRuleService(settings *SettingsService, adapters *AdapterService, desktop platform.DesktopHost) *RoutingRuleService {
	return &RoutingRuleService{settings: settings, adapters: adapters, desktop: desktop}
}

func (s *RoutingRuleService) Snapshot() (RoutingSnapshot, error) {
	rules, err := normalizeRules(s.settings.Get().RoutingRules)
	if err != nil {
		return RoutingSnapshot{}, err
	}
	return RoutingSnapshot{Rules: rules, Outbounds: s.availableOutbounds()}, nil
}

func (s *RoutingRuleService) availableOutbounds() []Outbound {
	outbounds := []Outbound{
		{ID: "aggregation", Label: "多网卡聚合"},
		{ID: "direct", Label: "直连 / 绕过"},
	}
	adapters, listErr := s.adapters.List()
	if listErr == nil {
		for _, adapter := range adapters {
			if !adapter.Selected {
				continue
			}
			outbounds = append(outbounds, Outbound{ID: "nic_" + adapter.ID, Label: adapter.Name})
		}
	}
	return outbounds
}

func (s *RoutingRuleService) Validate(rule RoutingRule, existing []RoutingRule) RoutingValidation {
	normalized, err := normalizeRule(rule)
	if err != nil {
		return RoutingValidation{Valid: false, Rule: rule, Message: err.Error()}
	}
	if err := s.validateSelectedOutbounds([]RoutingRule{normalized}); err != nil {
		return RoutingValidation{Valid: false, Rule: normalized, Message: err.Error()}
	}
	identity := ruleIdentity(normalized)
	for _, candidate := range existing {
		value, candidateErr := normalizeRule(candidate)
		if candidateErr == nil && ruleIdentity(value) == identity {
			return RoutingValidation{
				Valid: false, Rule: normalized, Duplicate: true,
				Message: "相同类型和匹配值的规则已存在",
			}
		}
	}
	return RoutingValidation{Valid: true, Rule: normalized}
}

func (s *RoutingRuleService) Save(rules []RoutingRule) (RoutingSnapshot, error) {
	normalized, err := normalizeRulesStrict(rules)
	if err != nil {
		return RoutingSnapshot{}, err
	}
	if err := s.validateSelectedOutbounds(normalized); err != nil {
		return RoutingSnapshot{}, err
	}
	if err := s.settings.saveRoutingRules(normalized); err != nil {
		return RoutingSnapshot{}, err
	}
	return s.Snapshot()
}

func (s *RoutingRuleService) ListProcesses() ([]string, error) {
	return listRunningProcesses()
}

func (s *RoutingRuleService) Import() (RoutingSnapshot, error) {
	path, err := s.desktop.OpenJSONFile("导入 HypoMux 分流规则")
	if err != nil {
		return RoutingSnapshot{}, fmt.Errorf("打开导入文件失败：%w", err)
	}
	if path == "" {
		return s.Snapshot()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return RoutingSnapshot{}, fmt.Errorf("读取规则文件失败：%w", err)
	}
	rules, err := parseRoutingBackup(data)
	if err != nil {
		return RoutingSnapshot{}, err
	}
	return RoutingSnapshot{Rules: rules, Outbounds: s.availableOutbounds()}, nil
}

func (s *RoutingRuleService) Export(rules []RoutingRule) (string, error) {
	normalized, err := normalizeRulesStrict(rules)
	if err != nil {
		return "", err
	}
	path, err := s.desktop.SaveJSONFile("导出 HypoMux 分流规则", "hypomux-rules.json")
	if err != nil {
		return "", fmt.Errorf("打开导出位置失败：%w", err)
	}
	if path == "" {
		return "", nil
	}
	if filepath.Ext(path) == "" {
		path += ".json"
	}
	payload := struct {
		Format     string        `json:"format"`
		Version    int           `json:"version"`
		ExportedAt string        `json:"exported_at"`
		Rules      []RoutingRule `json:"rules"`
	}{RoutingBackupFormat, RoutingBackupVersion, time.Now().Format(time.RFC3339), normalized}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", fmt.Errorf("生成规则备份失败：%w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", fmt.Errorf("导出规则失败：%w", err)
	}
	return path, nil
}

func parseRoutingBackup(data []byte) ([]RoutingRule, error) {
	var raw json.RawMessage = data
	var envelope struct {
		Format       string          `json:"format"`
		Version      json.RawMessage `json:"version"`
		Rules        json.RawMessage `json:"rules"`
		RoutingRules json.RawMessage `json:"routing_rules"`
	}
	var rulesData json.RawMessage
	if len(data) > 0 && data[0] == '[' {
		rulesData = raw
	} else if json.Unmarshal(data, &envelope) == nil {
		if envelope.Format != "" && envelope.Format != RoutingBackupFormat {
			return nil, fmt.Errorf("不支持的备份格式：%s", envelope.Format)
		}
		if envelope.Format == RoutingBackupFormat {
			version := 0
			if len(envelope.Version) > 0 {
				_ = json.Unmarshal(envelope.Version, &version)
				if version == 0 {
					var text string
					if json.Unmarshal(envelope.Version, &text) == nil {
						version, _ = strconv.Atoi(text)
					}
				}
			}
			if version != 1 && version != RoutingBackupVersion {
				return nil, fmt.Errorf("不支持的规则备份版本：%d", version)
			}
		}
		rulesData = envelope.Rules
		if len(rulesData) == 0 {
			rulesData = envelope.RoutingRules
		}
	}
	if len(rulesData) == 0 {
		return nil, fmt.Errorf("规则文件必须包含 rules 数组")
	}
	return parseRoutingRulesJSON(rulesData)
}

func parseRoutingRulesJSON(data []byte) ([]RoutingRule, error) {
	var entries []json.RawMessage
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("规则必须是 JSON 数组：%w", err)
	}
	expanded := make([]RoutingRule, 0, len(entries))
	for index, entry := range entries {
		rules, err := expandRoutingRule(entry)
		if err != nil {
			return nil, fmt.Errorf("第 %d 条旧版规则无效：%w", index+1, err)
		}
		expanded = append(expanded, rules...)
	}
	return normalizeRulesStrict(expanded)
}

func normalizeRulesStrict(rules []RoutingRule) ([]RoutingRule, error) {
	result := make([]RoutingRule, 0, len(rules))
	seen := map[string]struct{}{}
	for index, raw := range rules {
		rule, err := normalizeRule(raw)
		if err != nil {
			return nil, fmt.Errorf("第 %d 条规则无效：%w", index+1, err)
		}
		identity := ruleIdentity(rule)
		if _, duplicate := seen[identity]; duplicate {
			continue
		}
		seen[identity] = struct{}{}
		result = append(result, rule)
	}
	sortRules(result)
	return result, nil
}

func normalizeRules(rules []RoutingRule) ([]RoutingRule, error) {
	result := make([]RoutingRule, 0, len(rules))
	seen := map[string]struct{}{}
	for _, raw := range rules {
		rule, err := normalizeRule(raw)
		if err != nil {
			continue
		}
		identity := ruleIdentity(rule)
		if _, duplicate := seen[identity]; duplicate {
			continue
		}
		seen[identity] = struct{}{}
		result = append(result, rule)
	}
	sortRules(result)
	return result, nil
}

func (s *RoutingRuleService) validateSelectedOutbounds(rules []RoutingRule) error {
	adapters, err := s.adapters.List()
	if err != nil {
		return fmt.Errorf("读取可用网卡失败：%w", err)
	}
	return validateRoutingOutbounds(rules, adapters)
}

func validateRoutingOutbounds(rules []RoutingRule, adapters []AdapterView) error {
	available := map[string]struct{}{"aggregation": {}, "direct": {}}
	for _, adapter := range adapters {
		if adapter.Selected && adapter.Operational {
			available["nic_"+adapter.ID] = struct{}{}
		}
	}
	for index, rule := range rules {
		if _, ok := available[rule.Outbound]; !ok {
			return fmt.Errorf("第 %d 条分流规则引用了未启用或不可用的网卡出口：%s", index+1, rule.Outbound)
		}
	}
	return nil
}

func normalizeRule(rule RoutingRule) (RoutingRule, error) {
	rule.MatchType = canonicalMatchType(rule.MatchType)
	rule.Outbound = strings.TrimSpace(rule.Outbound)
	if rule.MatchType != MatchProcess && rule.MatchType != MatchDomain && rule.MatchType != MatchIP {
		return RoutingRule{}, fmt.Errorf("未知匹配类型")
	}
	if !isValidOutbound(rule.Outbound) {
		return RoutingRule{}, fmt.Errorf("未知出口通道")
	}
	value := strings.TrimSpace(rule.Value)
	switch rule.MatchType {
	case MatchProcess:
		if value == "" || len(value) > 260 || strings.ContainsAny(value, "/\\:\x00") {
			return RoutingRule{}, fmt.Errorf("进程名不能为空，且不能包含路径或冒号")
		}
	case MatchDomain:
		value = strings.ToLower(strings.TrimSuffix(value, "."))
		value = strings.TrimPrefix(strings.TrimPrefix(value, "*."), ".")
		if value == "" || len(value) > 253 || strings.ContainsAny(value, "/\\:\x00?#@ ") || net.ParseIP(value) != nil {
			return RoutingRule{}, fmt.Errorf("请输入有效域名，不要包含协议、端口或路径")
		}
		ascii, err := idna.Lookup.ToASCII(value)
		if err != nil || len(ascii) > 253 {
			return RoutingRule{}, fmt.Errorf("域名格式无效")
		}
		for _, label := range strings.Split(ascii, ".") {
			if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
				return RoutingRule{}, fmt.Errorf("域名标签格式无效")
			}
		}
		value = strings.ToLower(ascii)
	case MatchIP:
		if value == "" || len(value) > 64 {
			return RoutingRule{}, fmt.Errorf("请输入有效 IP 或 CIDR")
		}
		if !strings.Contains(value, "/") {
			ip := net.ParseIP(value)
			if ip == nil {
				return RoutingRule{}, fmt.Errorf("请输入有效 IP 或 CIDR")
			}
			if ip.To4() != nil {
				value += "/32"
			} else {
				value += "/128"
			}
		}
		ip, network, err := net.ParseCIDR(value)
		if err != nil {
			return RoutingRule{}, fmt.Errorf("请输入有效 IP 或 CIDR")
		}
		network.IP = ip.Mask(network.Mask)
		value = network.String()
	}
	rule.Value = value
	return rule, nil
}

func canonicalMatchType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "process", "process_name":
		return MatchProcess
	case "domain", "domain_suffix":
		return MatchDomain
	case "ip", "ip_cidr":
		return MatchIP
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func isValidOutbound(value string) bool {
	return value == "aggregation" || value == "direct" || value == "nic_ethernet" ||
		value == "nic_wifi" || (strings.HasPrefix(value, "nic_") && len(value) > 4)
}

func ruleIdentity(rule RoutingRule) string {
	return rule.MatchType + "\x00" + strings.ToLower(rule.Value)
}

func sortRules(rules []RoutingRule) {
	rank := map[string]int{MatchProcess: 0, MatchDomain: 1, MatchIP: 2}
	sort.SliceStable(rules, func(i, j int) bool {
		if rank[rules[i].MatchType] != rank[rules[j].MatchType] {
			return rank[rules[i].MatchType] < rank[rules[j].MatchType]
		}
		if rules[i].MatchType == MatchIP {
			_, ni, _ := net.ParseCIDR(rules[i].Value)
			_, nj, _ := net.ParseCIDR(rules[j].Value)
			oi, _ := ni.Mask.Size()
			oj, _ := nj.Mask.Size()
			if oi != oj {
				return oi > oj
			}
		}
		return strings.ToLower(rules[i].Value) < strings.ToLower(rules[j].Value)
	})
}
