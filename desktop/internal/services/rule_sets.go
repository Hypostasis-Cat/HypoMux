package services

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	// RuleSetMaxCount bounds how many external rule-sets a profile may hold.
	// Each one adds a rule-set definition to the pinned sing-box configuration,
	// so the limit keeps a mistaken subscription loop from growing startup cost.
	RuleSetMaxCount    = 16
	ruleSetNameMaxLen  = 64
	ruleSetURLMaxLen   = 2048
	ruleSetPriorityMin = 0
	ruleSetPriorityMax = 999
)

const (
	RuleSetFormatSingBoxSource = "singbox-source"
	RuleSetFormatClashProvider = "clash-provider"
)

// newRuleSetID mints an opaque identifier. It never encodes user text, so a
// rename cannot move the derived tag and watched file paths.
func newRuleSetID() string {
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		return fmt.Sprintf("rs-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(random[:])
}

// RuleSet is one external domain category list bound to a single outbound. It
// stays one row in the UI: its entries never expand into RoutingRule values.
// Fields below UpdatedAt are ingestion status, not user intent, and are kept in
// settings so a restart can show them without a second persistence path.
type RuleSet struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	URL      string `json:"url"`
	Outbound string `json:"outbound"`
	Priority int    `json:"priority,omitempty"`
	Disabled bool   `json:"disabled,omitempty"`

	UpdatedAt     int64  `json:"updated_at,omitempty"`
	ETag          string `json:"etag,omitempty"`
	LastModified  string `json:"last_modified,omitempty"`
	ContentSHA256 string `json:"content_sha256,omitempty"`
	Format        string `json:"format,omitempty"`
	EntryCount    int    `json:"entry_count,omitempty"`
	IgnoredCount  int    `json:"ignored_count,omitempty"`
	LastError     string `json:"last_error,omitempty"`
}

func validateRuleSets(sets []RuleSet) error {
	if len(sets) > RuleSetMaxCount {
		return fmt.Errorf("外部规则集最多 %d 个，当前 %d 个", RuleSetMaxCount, len(sets))
	}
	seen := make(map[string]struct{}, len(sets))
	for index, set := range sets {
		if strings.TrimSpace(set.ID) == "" {
			return fmt.Errorf("第 %d 个外部规则集缺少标识", index+1)
		}
		if _, exists := seen[set.ID]; exists {
			return fmt.Errorf("外部规则集标识重复：%s", set.ID)
		}
		seen[set.ID] = struct{}{}
		name := strings.TrimSpace(set.Name)
		if name == "" || len([]rune(name)) > ruleSetNameMaxLen {
			return fmt.Errorf("外部规则集名称必须是 1–%d 个字符", ruleSetNameMaxLen)
		}
		if err := validateRuleSetSourceURL(set.URL); err != nil {
			return fmt.Errorf("外部规则集 %s：%w", name, err)
		}
		if !isValidOutbound(set.Outbound) {
			return fmt.Errorf("外部规则集 %s 引用了未知出口通道", name)
		}
		if set.Priority < ruleSetPriorityMin || set.Priority > ruleSetPriorityMax {
			return fmt.Errorf("外部规则集 %s 的优先级必须是 %d–%d 的整数", name, ruleSetPriorityMin, ruleSetPriorityMax)
		}
	}
	return nil
}

// validateRuleSetOutbounds is the start-time twin of validateRoutingOutbounds:
// an enabled list bound to a deselected adapter would otherwise emit a route
// rule pointing at an outbound the generated configuration never declares.
func validateRuleSetOutbounds(sets []RuleSet, adapters []AdapterView) error {
	available := availableRoutingOutbounds(adapters)
	for index, set := range sets {
		if set.Disabled {
			continue
		}
		if _, ok := available[set.Outbound]; !ok {
			return fmt.Errorf("第 %d 个外部规则集引用了未启用或不可用的网卡出口：%s", index+1, set.Outbound)
		}
	}
	return nil
}

// externalRuleSetBinding derives the sing-box rule-set tag and watched file path
// for a set. Both come from a digest of the stable id and never from user text,
// so neither a rename nor a crafted id can escape the rule-set directory or move
// a reference off a set that is already declared in the running configuration.
func externalRuleSetBinding(set RuleSet) (string, string) {
	return externalRuleSetBindingIn(ruleSetDirectory(), set)
}

func externalRuleSetBindingIn(directory string, set RuleSet) (string, string) {
	digest := sha256.Sum256([]byte("hypomux-ext\x00" + strings.TrimSpace(set.ID)))
	suffix := hex.EncodeToString(digest[:8])
	return "hypomux-ext-" + suffix, filepath.Join(directory, "ext-"+suffix+".json")
}

// externalRuleSetSourcePath is the normalized subscription payload an update
// published. The watched rule-set file above is recomposed from it on every plan
// write so manual-rule carve-outs stay in sync without refetching.
func externalRuleSetSourcePathIn(directory string, set RuleSet) string {
	_, path := externalRuleSetBindingIn(directory, set)
	return strings.TrimSuffix(path, ".json") + ".source.json"
}

// liveExternalRuleSets keeps the sets that can take effect right now: enabled and
// with a source payload already published by a successful update. Referencing a
// set whose payload is absent would leave a dangling rule_set tag in the
// configuration sing-box is watching, which is why "never fetched" and "deleted"
// are handled by the same absence check.
func liveExternalRuleSets(directory string, sets []RuleSet) []RuleSet {
	live := make([]RuleSet, 0, len(sets))
	for _, set := range sets {
		if set.Disabled {
			continue
		}
		if info, err := os.Stat(externalRuleSetSourcePathIn(directory, set)); err != nil || info.IsDir() {
			continue
		}
		live = append(live, set)
	}
	return live
}

// externalRuleSetRecord is the declaration the pinned main configuration was
// built with: one row per referenced set. Membership, outbound and priority all
// feed the restart check — the first two change the pinned route rules, and the
// last one only reorders content-level carve-outs, so a priority edit stays hot.
type externalRuleSetRecord struct {
	Tag      string `json:"tag"`
	Outbound string `json:"outbound"`
	Priority int    `json:"priority"`
}

func externalRuleSetRecords(sets []RuleSet) []externalRuleSetRecord {
	records := make([]externalRuleSetRecord, 0, len(sets))
	for _, set := range sets {
		tag, _ := externalRuleSetBinding(set)
		records = append(records, externalRuleSetRecord{
			Tag: tag, Outbound: set.Outbound, Priority: set.Priority,
		})
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Tag < records[j].Tag })
	return records
}

func sameExternalRuleSetRecords(left, right []externalRuleSetRecord) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		// Priority is recorded for observability but deliberately not compared:
		// it only orders content-level carve-outs, which hot reload applies, so
		// a priority edit must never pin the engine to a restart.
		if left[index].Tag != right[index].Tag || left[index].Outbound != right[index].Outbound {
			return false
		}
	}
	return true
}

// validateRuleSetSourceURL only checks the address shape. Host resolution and
// the private-address guard happen at dial time so a rebinding response cannot
// pass a stored-configuration check and then point somewhere internal.
func validateRuleSetSourceURL(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New("订阅地址不能为空")
	}
	if len(value) > ruleSetURLMaxLen {
		return fmt.Errorf("订阅地址不能超过 %d 个字符", ruleSetURLMaxLen)
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return errors.New("订阅地址无法解析")
	}
	if parsed.Scheme != "https" {
		return errors.New("订阅地址必须使用 HTTPS")
	}
	if parsed.Host == "" || parsed.Opaque != "" || parsed.User != nil || parsed.Fragment != "" {
		return errors.New("订阅地址必须是干净的 HTTPS 地址，不能包含凭据或片段")
	}
	return nil
}

// decodeExternalRuleSetPayload parses a normalized subscription payload the
// ingestion step published. Only flat domain/CIDR headless rules survive here;
// anything else would either reference another rule-set — which sing-box
// rejects inside rule-set files — or silently change the list's meaning.
func decodeExternalRuleSetPayload(data []byte) ([]map[string]any, error) {
	var source struct {
		Version int              `json:"version"`
		Rules   []map[string]any `json:"rules"`
	}
	if err := json.Unmarshal(data, &source); err != nil {
		return nil, fmt.Errorf("外部规则集内容无效：%w", err)
	}
	if source.Version < 1 || source.Version > singBoxRuleSetVersion {
		return nil, errors.New("外部规则集内容版本不受支持")
	}
	rules := make([]map[string]any, 0, len(source.Rules))
	for _, rule := range source.Rules {
		if isPlainExternalRuleSetEntry(rule) {
			rules = append(rules, rule)
		}
	}
	return rules, nil
}

func isPlainExternalRuleSetEntry(rule map[string]any) bool {
	for key := range rule {
		switch key {
		case "domain", "domain_suffix", "domain_keyword", "domain_regex", "ip_cidr":
		default:
			return false
		}
	}
	return len(rule) > 0
}
