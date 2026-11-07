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

const (
	// blockedDomainExpiryTTL matches the engine's quarantine lifetime.
	blockedDomainExpiryTTL = 30 * time.Minute
	// A deadline further away than this can only come from a period with
	// automatic expiry disabled, which records a date 100 years ahead.
	blockedDomainPermanentHorizon = 24 * time.Hour
)

type BlockedDomainService struct {
	mu       sync.Mutex
	path     string
	settings *SettingsService
	now      func() time.Time
	entries  map[string]map[string]float64
	// removals counts manual removals so a telemetry sample requested before
	// one of them can be recognized and dropped instead of restoring the record.
	removals uint64
	// evictRuntime runs commit after removing the records from a running engine.
	// It is set once while EngineService is constructed.
	evictRuntime func(params map[string]any, commit func() error) error
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

// expiryChange is a record's state before enforceExpiryLocked changed it.
type expiryChange struct {
	adapter string
	domain  string
	expiry  float64
}

func (s *BlockedDomainService) List() (BlockedDomainSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	changes := s.enforceExpiryLocked()
	if len(changes) > 0 {
		if err := s.saveLocked(); err != nil {
			s.restoreExpiryLocked(changes)
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
	return s.commitRemoval(map[string]any{
		"remove": []map[string]string{{"adapter": adapter, "domain": domain}},
	}, func() {
		if domains := s.entries[adapter]; domains != nil {
			delete(domains, domain)
			if len(domains) == 0 {
				delete(s.entries, adapter)
			}
		}
	})
}

func (s *BlockedDomainService) Clear() error {
	return s.commitRemoval(map[string]any{"clear": true}, func() {
		s.entries = map[string]map[string]float64{}
	})
}

// commitRemoval tells a running engine first. A sample taken after that cannot
// contain the removed records, and one taken earlier is dropped by the removals
// count, so old telemetry cannot write them back.
func (s *BlockedDomainService) commitRemoval(params map[string]any, remove func()) error {
	commit := func() error {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.removals++
		remove()
		return s.saveLocked()
	}
	if s.evictRuntime == nil {
		return commit()
	}
	return s.evictRuntime(params, commit)
}

// runtimeSampleToken must be read before requesting the telemetry passed to
// replaceRuntimeSample.
func (s *BlockedDomainService) runtimeSampleToken() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.removals
}

func (s *BlockedDomainService) replaceRuntimeSample(token uint64, entries []BlockedDomainEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if token != s.removals {
		return nil
	}
	return s.replaceRuntimeLocked(entries)
}

func (s *BlockedDomainService) ReplaceRuntime(entries []BlockedDomainEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.replaceRuntimeLocked(entries)
}

func (s *BlockedDomainService) replaceRuntimeLocked(entries []BlockedDomainEntry) error {
	useExpiry := s.settings.Get().BlockedDomainExpiry
	now := s.now()
	next := make(map[string]map[string]float64)
	if !useExpiry {
		// Runtime telemetry describes only the current engine run, not the
		// lifetime of persisted records. Expiry under the old start-time
		// settings, recovery, or an unselected adapter must not erase records
		// while automatic expiry is disabled.
		for adapter, domains := range s.entries {
			next[adapter] = make(map[string]float64, len(domains))
			for domain, expiry := range domains {
				next[adapter][domain] = expiry
			}
		}
	}
	for _, entry := range entries {
		adapter := strings.TrimSpace(entry.Adapter)
		domain := normalizeDomain(entry.Domain)
		if adapter == "" || domain == "" || entry.ExpiresAt.IsZero() {
			continue
		}
		if useExpiry && !entry.ExpiresAt.After(now) {
			continue
		}
		if next[adapter] == nil {
			next[adapter] = map[string]float64{}
		}
		expiry := float64(entry.ExpiresAt.Unix())
		if useExpiry && entry.ExpiresAt.After(now.Add(blockedDomainPermanentHorizon)) {
			// The engine has not received the policy yet. Keep an already limited
			// deadline so repeated samples neither extend it nor rewrite the file.
			expiry = float64(now.Add(blockedDomainExpiryTTL).Unix())
			if existing, ok := s.entries[adapter][domain]; ok && existing > float64(now.Unix()) && existing <= expiry {
				expiry = existing
			}
		}
		next[adapter][domain] = expiry
	}
	if blockedDomainMapsEqual(s.entries, next) {
		return nil
	}
	previous := s.entries
	s.entries = next
	if err := s.saveLocked(); err != nil {
		// Keep the last persisted state so an identical telemetry sample can
		// retry the write instead of being mistaken for a no-op.
		s.entries = previous
		return err
	}
	return nil
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
	s.enforceExpiryLocked()
	return nil
}

// enforceExpiryLocked applies automatic expiry to the stored records: past
// deadlines are removed, and records made permanent while expiry was disabled
// get the normal lifetime from now on. It returns what it changed.
func (s *BlockedDomainService) enforceExpiryLocked() []expiryChange {
	if !s.settings.Get().BlockedDomainExpiry {
		return nil
	}
	current := s.now()
	now := float64(current.Unix())
	horizon := float64(current.Add(blockedDomainPermanentHorizon).Unix())
	limited := float64(current.Add(blockedDomainExpiryTTL).Unix())
	var changes []expiryChange
	for adapter, domains := range s.entries {
		for domain, expiry := range domains {
			switch {
			case expiry <= now:
				delete(domains, domain)
			case expiry > horizon:
				domains[domain] = limited
			default:
				continue
			}
			changes = append(changes, expiryChange{adapter: adapter, domain: domain, expiry: expiry})
		}
		if len(domains) == 0 {
			delete(s.entries, adapter)
		}
	}
	return changes
}

func (s *BlockedDomainService) restoreExpiryLocked(changes []expiryChange) {
	for _, entry := range changes {
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
