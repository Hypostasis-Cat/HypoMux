package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
	return append([]RuleSet(nil), s.settings.Get().RuleSets...)
}

// Save replaces the whole list. Because entries never expand into routing
// rules, this stays cheap; the refresh only recomposes watched files whose
// carve-outs depend on priorities and outbounds.
func (s *RuleSetService) Save(sets []RuleSet) ([]RuleSet, error) {
	normalized, err := s.normalize(sets)
	if err != nil {
		return nil, err
	}
	settings := s.settings.Get()
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

	fetched, err := s.fetch(ctx, set)
	if err != nil {
		s.recordIngestionFailure(set, err)
		return nil, fmt.Errorf("更新外部规则集 %s 失败：%w", set.Name, err)
	}
	if fetched.NotModified {
		updated := current
		updated[index].UpdatedAt = s.now().Unix()
		updated[index].LastError = ""
		if _, saveErr := s.commitRuleSets(updated); saveErr != nil {
			return nil, saveErr
		}
		return s.List(), nil
	}
	parsed, err := parseRuleSetSubscription(fetched.Body)
	if err != nil {
		s.recordIngestionFailure(set, err)
		return nil, fmt.Errorf("解析外部规则集 %s 失败：%w", set.Name, err)
	}
	payload, entryCount, err := canonicalExternalRuleSetPayload(parsed.Rules)
	if err != nil {
		s.recordIngestionFailure(set, err)
		return nil, err
	}
	contentSHA256, err := publishExternalRuleSetSource(ruleSetDirectory(), current[index], payload)
	if err != nil {
		s.recordIngestionFailure(set, err)
		return nil, err
	}

	updated := append([]RuleSet(nil), current...)
	updated[index].UpdatedAt = s.now().Unix()
	updated[index].ETag = fetched.ETag
	updated[index].LastModified = fetched.LastModified
	updated[index].ContentSHA256 = contentSHA256
	updated[index].Format = parsed.Format
	updated[index].EntryCount = entryCount
	updated[index].IgnoredCount = parsed.Ignored
	updated[index].LastError = ""
	if _, err := s.commitRuleSets(updated); err != nil {
		return nil, err
	}
	return s.List(), nil
}

func (s *RuleSetService) recordIngestionFailure(set RuleSet, failure error) {
	current := s.settings.Get().RuleSets
	updated := append([]RuleSet(nil), current...)
	for index := range updated {
		if updated[index].ID != set.ID {
			continue
		}
		updated[index].UpdatedAt = s.now().Unix()
		updated[index].LastError = failure.Error()
		break
	}
	if err := s.settings.saveRuleSets(updated); err != nil {
		return
	}
}

// commitRuleSets publishes the watched files for the updated list before
// committing settings, reusing the routing save machinery's rollback so a
// failed publish never leaves files and settings disagreeing.
func (s *RuleSetService) commitRuleSets(sets []RuleSet) ([]RuleSet, error) {
	settings := s.settings.Get()
	if err := refreshSingBoxRuleSetsAndCommit(settings.RoutingRules, sets, func() error {
		return s.settings.saveRuleSets(sets)
	}); err != nil {
		return nil, fmt.Errorf("提交外部规则集失败；系统已尝试恢复原文件：%w", err)
	}
	return s.List(), nil
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
