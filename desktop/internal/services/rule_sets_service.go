package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// RuleSetService owns the subscribed domain-category lists behind issue #62:
// instead of typing domains one by one, a whole category is bound to one
// outbound through an external rule-set. The service persists the list, fetches
// and normalizes subscriptions, and keeps the watched sing-box files in sync so
// content updates hot-reload.
type RuleSetService struct {
	settings *SettingsService
	adapters *AdapterService
	fetch    func(context.Context, RuleSet) (ruleSetFetchResult, error)
	now      func() time.Time
	updateMu sync.Mutex // Serialize downloads without blocking routing edits.
}

func NewRuleSetService(settings *SettingsService, adapters *AdapterService) *RuleSetService {
	return &RuleSetService{
		settings: settings,
		adapters: adapters,
		fetch:    fetchRuleSet,
		now:      time.Now,
	}
}

func (s *RuleSetService) List() []RuleSet {
	// Always a non-nil slice: an empty list must serialize as `[]` for the
	// panel, not as `null`.
	sets := s.settings.Get().RuleSets
	result := make([]RuleSet, len(sets))
	copy(result, sets)
	return result
}

type RuleSetEntry struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

type RuleSetEntries struct {
	Entries    []RuleSetEntry `json:"entries"`
	Total      int            `json:"total"`
	Downloaded bool           `json:"downloaded"`
}

// Entries reads the last downloaded source, including disabled subscriptions.
// Only configured IDs are accepted; callers cannot supply a filesystem path.
// Filtering and pagination bound the response sent to the desktop UI.
func (s *RuleSetService) Entries(id, query string, offset, limit int) (RuleSetEntries, error) {
	singBoxRuleSetMu.Lock()
	defer singBoxRuleSetMu.Unlock()
	result := RuleSetEntries{Entries: []RuleSetEntry{}}
	var selected *RuleSet
	for _, set := range s.List() {
		if set.ID == id {
			selected = &set
			break
		}
	}
	if selected == nil {
		return result, errors.New("外部规则集不存在或已被删除")
	}
	data, err := os.ReadFile(externalRuleSetSourcePathIn(ruleSetDirectory(), *selected))
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return result, fmt.Errorf("读取规则集内容失败：%w", err)
	}
	rules, err := decodeExternalRuleSetPayload(data)
	if err != nil {
		return result, err
	}
	result.Downloaded = true
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	query = strings.ToLower(strings.TrimSpace(query))
	for _, rule := range rules {
		for _, kind := range externalRuleSetKinds {
			values, _ := rule[kind].([]any)
			for _, raw := range values {
				value, ok := raw.(string)
				if !ok || !strings.Contains(strings.ToLower(value), query) {
					continue
				}
				if result.Total >= offset && len(result.Entries) < limit {
					result.Entries = append(result.Entries, RuleSetEntry{Kind: kind, Value: value})
				}
				result.Total++
			}
		}
	}
	return result, nil
}

// Save replaces the whole list. Because entries never expand into routing
// rules, this stays cheap; the refresh only recomposes watched files whose
// carve-outs depend on priorities and outbounds.
func (s *RuleSetService) Save(sets []RuleSet) ([]RuleSet, error) {
	routingMutationMu.Lock()
	defer routingMutationMu.Unlock()
	normalized, err := s.normalize(sets)
	if err != nil {
		return nil, err
	}
	settings := s.settings.Get()
	// Ingestion status belongs to the service, not a possibly stale UI snapshot.
	for i, requested := range normalized {
		status := RuleSet{}
		for _, existing := range settings.RuleSets {
			if existing.ID == requested.ID {
				status = existing
				if existing.URL != requested.URL {
					status.ETag, status.LastModified = "", ""
				}
				break
			}
		}
		status.ID, status.Name, status.URL = requested.ID, requested.Name, requested.URL
		status.Outbound, status.Priority, status.Disabled = requested.Outbound, requested.Priority, requested.Disabled
		normalized[i] = status
	}
	if err := refreshSingBoxRuleSetsAndCommit(settings.RoutingRules, normalized, func() error {
		return s.settings.saveRuleSets(normalized)
	}); err != nil {
		return nil, fmt.Errorf("保存外部规则集失败；系统已尝试恢复原文件：%w", err)
	}
	return s.List(), nil
}

// Update refetches one subscription, republishes its normalized payload and
// records the ingestion status for the UI. A failed update keeps the previously
// published payload routing; only the error message moves.
func (s *RuleSetService) Update(id string) ([]RuleSet, error) {
	s.updateMu.Lock()
	defer s.updateMu.Unlock()
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, errors.New("缺少规则集标识")
	}
	ctx, cancel := context.WithTimeout(context.Background(), ruleSetFetchTimeout+10*time.Second)
	defer cancel()

	current := s.settings.Get().RuleSets
	index := -1
	for position, set := range current {
		if set.ID == id {
			index = position
			break
		}
	}
	if index < 0 {
		return nil, errors.New("外部规则集不存在或已被删除")
	}
	set := current[index]
	// A conditional response is useful only while its local payload still exists.
	if data, readErr := os.ReadFile(externalRuleSetSourcePathIn(ruleSetDirectory(), set)); readErr != nil {
		set.ETag, set.LastModified = "", ""
	} else if _, decodeErr := decodeExternalRuleSetPayload(data); decodeErr != nil {
		set.ETag, set.LastModified = "", ""
	}

	fetched, err := s.fetch(ctx, set)
	if err != nil {
		s.recordIngestionFailure(set, err)
		return nil, fmt.Errorf("更新外部规则集 %s 失败：%w", set.Name, err)
	}
	if fetched.NotModified && set.ETag == "" && set.LastModified == "" {
		return nil, errors.New("订阅服务器返回未修改，但没有可用的本地缓存")
	}
	var parsed ruleSetParseResult
	var payload []byte
	var entryCount int
	if !fetched.NotModified {
		parsed, err = parseRuleSetSubscription(fetched.Body)
		if err != nil {
			s.recordIngestionFailure(set, err)
			return nil, fmt.Errorf("解析外部规则集 %s 失败：%w", set.Name, err)
		}
		payload, entryCount, err = canonicalExternalRuleSetPayload(parsed.Rules)
		if err != nil {
			s.recordIngestionFailure(set, err)
			return nil, err
		}
	}
	// Re-read after the network request: it must never resurrect a deleted set
	// or overwrite an edit made while the download was in flight.
	routingMutationMu.Lock()
	defer routingMutationMu.Unlock()
	updated := s.settings.Get().RuleSets
	index = -1
	for i, latest := range updated {
		if latest.ID == id && latest.URL == set.URL {
			index = i
			break
		}
	}
	if index < 0 {
		return nil, errors.New("规则集已删除或订阅地址已更改，请重新更新")
	}
	// Source, watched files and settings form one transaction. In particular a
	// failed commit must not leak new source data into the next manual-rule save.
	singBoxRuleSetMu.Lock()
	defer singBoxRuleSetMu.Unlock()
	rollbackSource := func() error { return nil }
	if !fetched.NotModified {
		if err := os.MkdirAll(ruleSetDirectory(), 0o700); err != nil {
			return nil, err
		}
		rollbackSource, err = publishRuleSetFiles([]ruleSetFile{{Path: externalRuleSetSourcePathIn(ruleSetDirectory(), set), Data: payload, Mode: 0o600}}, replaceFileAtomically)
		if err != nil {
			return nil, err
		}
		updated[index].ContentSHA256 = ruleSetPayloadDigest(payload)
		updated[index].ETag = fetched.ETag
		updated[index].LastModified = fetched.LastModified
		updated[index].Format = parsed.Format
		updated[index].EntryCount = entryCount
		updated[index].IgnoredCount = parsed.Ignored
	}
	updated[index].UpdatedAt = s.now().Unix()
	updated[index].LastError = ""
	if err := refreshSingBoxRuleSetsAndCommitLocked(s.settings.Get().RoutingRules, updated, func() error { return s.settings.saveRuleSets(updated) }); err != nil {
		return nil, errors.Join(err, rollbackSource())
	}
	return s.List(), nil
}

func (s *RuleSetService) recordIngestionFailure(set RuleSet, failure error) {
	routingMutationMu.Lock()
	defer routingMutationMu.Unlock()
	current := s.settings.Get().RuleSets
	updated := append([]RuleSet(nil), current...)
	for index := range updated {
		if updated[index].ID != set.ID || updated[index].URL != set.URL {
			continue
		}
		updated[index].UpdatedAt = s.now().Unix()
		updated[index].LastError = failure.Error()
		_ = s.settings.saveRuleSets(updated)
		return
	}
}

func (s *RuleSetService) normalize(sets []RuleSet) ([]RuleSet, error) {
	normalized := make([]RuleSet, 0, len(sets))
	for _, set := range sets {
		set.ID = strings.TrimSpace(set.ID)
		if set.ID == "" {
			set.ID = newRuleSetID()
		}
		set.Name = strings.TrimSpace(set.Name)
		set.URL = strings.TrimSpace(set.URL)
		set.Outbound = strings.TrimSpace(set.Outbound)
		normalized = append(normalized, set)
	}
	if err := validateRuleSets(normalized); err != nil {
		return nil, err
	}
	adapters, err := s.adapters.List()
	if err != nil {
		return nil, fmt.Errorf("读取可用网卡失败：%w", err)
	}
	if err := validateRuleSetOutbounds(normalized, adapters); err != nil {
		return nil, err
	}
	return normalized, nil
}
