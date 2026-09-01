package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type BlockedDomainEntry struct {
	Adapter          string    `json:"adapter"`
	Domain           string    `json:"domain"`
	ExpiresAt        time.Time `json:"expires_at"`
	RemainingSeconds int64     `json:"remaining_seconds"`
	Permanent        bool      `json:"permanent"`
}

type BlockedDomainSnapshot struct {
	Enabled   bool                 `json:"enabled"`
	UseExpiry bool                 `json:"use_expiry"`
	Entries   []BlockedDomainEntry `json:"entries"`
}

type BlockedDomainService struct {
	mu       sync.Mutex
	path     string
	settings *SettingsService
	now      func() time.Time
	entries  map[string]map[string]float64
}

func NewBlockedDomainService(settings *SettingsService) *BlockedDomainService {
	service := &BlockedDomainService{
		path:     filepath.Join(settingsDirectory(), "blocked_domains.json"),
		settings: settings,
		now:      time.Now,
		entries:  map[string]map[string]float64{},
	}
	_ = service.load()
	return service
}

type purgedDomain struct {
	adapter string
	domain  string
	expiry  float64
}

func (s *BlockedDomainService) List() (BlockedDomainSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	purged := s.purgeExpiredLocked()
	if len(purged) > 0 {
		if err := s.saveLocked(); err != nil {
			s.restorePurgedLocked(purged)
		}
	}
	preferences := s.settings.Get()
	snapshot := BlockedDomainSnapshot{
		Enabled: preferences.BlockedDomainBypass, UseExpiry: preferences.BlockedDomainExpiry,
		Entries: []BlockedDomainEntry{},
	}
	now := s.now()
	for adapter, domains := range s.entries {
		for domain, expiry := range domains {
			remaining := int64(expiry - float64(now.Unix()))
			if remaining < 0 {
				remaining = 0
			}
			snapshot.Entries = append(snapshot.Entries, BlockedDomainEntry{
				Adapter: adapter, Domain: domain, ExpiresAt: time.Unix(int64(expiry), 0),
				RemainingSeconds: remaining, Permanent: !preferences.BlockedDomainExpiry,
			})
		}
	}
	sort.Slice(snapshot.Entries, func(i, j int) bool {
		if snapshot.Entries[i].Adapter == snapshot.Entries[j].Adapter {
			return snapshot.Entries[i].Domain < snapshot.Entries[j].Domain
		}
		return snapshot.Entries[i].Adapter < snapshot.Entries[j].Adapter
	})
	return snapshot, nil
}

func (s *BlockedDomainService) Remove(adapter string, domain string) error {
	adapter = strings.TrimSpace(adapter)
	domain = normalizeDomain(domain)
	if adapter == "" || domain == "" {
		return errors.New("网卡与域名不能为空")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if domains := s.entries[adapter]; domains != nil {
		delete(domains, domain)
		if len(domains) == 0 {
			delete(s.entries, adapter)
		}
	}
	return s.saveLocked()
}

func (s *BlockedDomainService) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = map[string]map[string]float64{}
	return s.saveLocked()
}

func (s *BlockedDomainService) ReplaceRuntime(entries []BlockedDomainEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := make(map[string]map[string]float64)
	for _, entry := range entries {
		adapter := strings.TrimSpace(entry.Adapter)
		domain := normalizeDomain(entry.Domain)
		if adapter == "" || domain == "" || !entry.ExpiresAt.After(s.now()) {
			continue
		}
		if next[adapter] == nil {
			next[adapter] = map[string]float64{}
		}
		next[adapter][domain] = float64(entry.ExpiresAt.Unix())
	}
	if blockedDomainMapsEqual(s.entries, next) {
		return nil
	}
	s.entries = next
	return s.saveLocked()
}

func (s *BlockedDomainService) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		legacy, legacyErr := legacyBlockedDomainsPath()
		if legacyErr == nil {
			data, err = os.ReadFile(legacy)
		}
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("读取域名隔离记录失败：%w", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("域名隔离记录格式无效：%w", err)
	}
	now := s.now().Add(30 * time.Minute).Unix()
	for adapter, payload := range raw {
		var current map[string]float64
		if json.Unmarshal(payload, &current) == nil {
			s.entries[adapter] = current
			continue
		}
		var legacy []string
		if json.Unmarshal(payload, &legacy) == nil {
			s.entries[adapter] = map[string]float64{}
			for _, domain := range legacy {
				if normalized := normalizeDomain(domain); normalized != "" {
					s.entries[adapter][normalized] = float64(now)
				}
			}
		}
	}
	s.purgeExpiredLocked()
	return nil
}

func (s *BlockedDomainService) purgeExpiredLocked() []purgedDomain {
	if !s.settings.Get().BlockedDomainExpiry {
		return nil
	}
	now := float64(s.now().Unix())
	var purged []purgedDomain
	for adapter, domains := range s.entries {
		for domain, expiry := range domains {
			if expiry <= now {
				delete(domains, domain)
				purged = append(purged, purgedDomain{adapter: adapter, domain: domain, expiry: expiry})
			}
		}
		if len(domains) == 0 {
			delete(s.entries, adapter)
		}
	}
	return purged
}

func (s *BlockedDomainService) restorePurgedLocked(purged []purgedDomain) {
	for _, entry := range purged {
		if s.entries[entry.adapter] == nil {
			s.entries[entry.adapter] = map[string]float64{}
		}
		s.entries[entry.adapter][entry.domain] = entry.expiry
	}
}

func (s *BlockedDomainService) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("创建域名隔离配置目录失败：%w", err)
	}
	data, err := json.MarshalIndent(s.entries, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化域名隔离记录失败：%w", err)
	}
	temporary := s.path + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return fmt.Errorf("写入域名隔离记录失败：%w", err)
	}
	if err := os.Rename(temporary, s.path); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("提交域名隔离记录失败：%w", err)
	}
	return nil
}

func normalizeDomain(value string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(value), "."))
}

func blockedDomainMapsEqual(left map[string]map[string]float64, right map[string]map[string]float64) bool {
	if len(left) != len(right) {
		return false
	}
	for adapter, leftDomains := range left {
		rightDomains, exists := right[adapter]
		if !exists || len(leftDomains) != len(rightDomains) {
			return false
		}
		for domain, expiry := range leftDomains {
			if rightDomains[domain] != expiry {
				return false
			}
		}
	}
	return true
}

func legacyBlockedDomainsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".hypomux", "blocked_domains.json"), nil
}
