package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"

	desktopplatform "github.com/Hypostasis-Cat/HypoMux/desktop/internal/platform"
)

const (
	AdapterWeightMin     = 1
	AdapterWeightMax     = 100
	AdapterWeightStep    = 1
	AdapterWeightDefault = 1
)

type AppSettings struct {
	Mode                string                `json:"mode"`
	Language            string                `json:"language"`
	SOCKSPort           int                   `json:"socks_port"`
	HTTPPort            int                   `json:"http_port"`
	Weighted            bool                  `json:"weighted"`
	StrictRoute         bool                  `json:"strict_route"`
	WFPCompatibility    WFPCompatibilityState `json:"wfp_compatibility_state,omitempty"`
	ForceTUNBypass      bool                  `json:"force_tun_connectivity_bypass"`
	ShowVirtualAdapters bool                  `json:"show_virtual_adapters"`
	BlockedDomainBypass bool                  `json:"blocked_domain_bypass"`
	BlockedDomainExpiry bool                  `json:"blocked_domain_expiry"`
	CloseToTray         bool                  `json:"close_to_tray"`
	Autostart           bool                  `json:"autostart"`
	AutoStartEngine     bool                  `json:"auto_start_engine"`
	DNSServer           string                `json:"dns_server"`
	DNSPolicy           string                `json:"dns_policy"`
	DNSEgressMode       string                `json:"dns_egress_mode"`
	DNSAdapterID        string                `json:"dns_adapter_id,omitempty"`
	SelectedAdapterIDs  []string              `json:"selected_adapter_ids"`
	AdapterWeights      map[string]int        `json:"adapter_weights"`
	RoutingRules        []RoutingRule         `json:"routing_rules"`
}

type WFPCompatibilityState struct {
	Status      string `json:"status,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
	Detail      string `json:"detail,omitempty"`
}

func DefaultSettings() AppSettings {
	return AppSettings{
		Mode:                "tun",
		Language:            "zh",
		SOCKSPort:           10800,
		HTTPPort:            10801,
		StrictRoute:         true,
		BlockedDomainExpiry: true,
		CloseToTray:         false,
		DNSServer:           "223.5.5.5",
		DNSPolicy:           "auto",
		DNSEgressMode:       DNSEgressAuto,
		AdapterWeights:      map[string]int{},
		RoutingRules:        []RoutingRule{},
	}
}

type SettingsService struct {
	mu               sync.RWMutex
	path             string
	settings         AppSettings
	migration        ConfigMigrationStatus
	loadErr          error
	setAutostart     func(bool) error
	autostartEnabled func() (bool, error)
}

type ConfigMigrationStatus struct {
	LegacyFound bool   `json:"legacy_found"`
	Applied     bool   `json:"applied"`
	LegacyPath  string `json:"legacy_path"`
	BackupPath  string `json:"backup_path,omitempty"`
	Message     string `json:"message"`
}

func NewSettingsService() *SettingsService {
	directory := settingsDirectory()
	service := &SettingsService{
		path:             filepath.Join(directory, "settings.json"),
		setAutostart:     desktopplatform.SetAutostart,
		autostartEnabled: desktopplatform.AutostartEnabled,
	}
	service.settings = DefaultSettings()
	if err := service.reload(); err != nil {
		service.loadErr = err
	}
	service.inspectLegacyConfig()
	return service
}

// StartupError reports a settings load failure without replacing the unreadable
// file with defaults. The desktop entry point treats this as fatal so the user
// can repair or back up the original configuration safely.
func (s *SettingsService) StartupError() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.loadErr
}

func settingsDirectory() string {
	if configured := os.Getenv("HYPOMUX_DATA_DIR"); configured != "" {
		if expanded, err := filepath.Abs(os.ExpandEnv(configured)); err == nil {
			return expanded
		}
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		if root, configErr := os.UserConfigDir(); configErr == nil && root != "" {
			return filepath.Join(root, "HypoMux")
		}
		return filepath.Join(os.TempDir(), "HypoMux")
	}
	return filepath.Join(home, ".hypomux")
}

func (s *SettingsService) Get() AppSettings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := cloneSettings(s.settings)
	if enabled, err := s.autostartEnabled(); err == nil {
		result.Autostart = enabled
		if !enabled {
			result.AutoStartEngine = false
		}
	}
	return result
}

func (s *SettingsService) ConfigPath() string {
	return s.path
}

func (s *SettingsService) MigrationStatus() ConfigMigrationStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.migration
}

func (s *SettingsService) MigrateLegacy() (AppSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	legacyPath, err := legacyConfigPath()
	if err != nil {
		return AppSettings{}, err
	}
	data, err := os.ReadFile(legacyPath)
	if err != nil {
		return AppSettings{}, fmt.Errorf("读取旧版配置失败：%w", err)
	}
	migrated, err := migrateLegacySettings(data)
	if err != nil {
		return AppSettings{}, err
	}
	if current, readErr := os.ReadFile(s.path); readErr == nil {
		backup := filepath.Join(settingsDirectory(), "settings.before-legacy-migration.json")
		if writeErr := os.WriteFile(backup, current, 0o600); writeErr != nil {
			return AppSettings{}, fmt.Errorf("备份新版配置失败：%w", writeErr)
		}
		s.migration.BackupPath = backup
	}
	if err := s.commitLocked(migrated); err != nil {
		return AppSettings{}, err
	}
	s.migration.LegacyFound = true
	s.migration.Applied = true
	s.migration.LegacyPath = legacyPath
	s.migration.Message = "旧版网络配置与分流规则已迁移；界面偏好按设计恢复默认，原文件保持不变"
	return cloneSettings(s.settings), nil
}

func (s *SettingsService) RollbackLegacyMigration() (AppSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.migration.Applied {
		return AppSettings{}, errors.New("当前没有可回滚的旧配置迁移")
	}
	var restored AppSettings
	if s.migration.BackupPath != "" {
		data, err := os.ReadFile(s.migration.BackupPath)
		if err != nil {
			return AppSettings{}, fmt.Errorf("读取迁移前备份失败：%w", err)
		}
		if err := json.Unmarshal(data, &restored); err != nil {
			return AppSettings{}, fmt.Errorf("迁移前备份格式无效：%w", err)
		}
		if restored.DNSEgressMode == "" {
			restored.DNSEgressMode = DNSEgressAuto
		}
	} else {
		restored = DefaultSettings()
	}
	if err := s.commitLocked(restored); err != nil {
		return AppSettings{}, err
	}
	s.migration.Applied = false
	s.migration.Message = "已回滚迁移结果；旧版配置文件未删除"
	return cloneSettings(s.settings), nil
}

// Update persists every ordinary setting exposed by the Settings page. Home
// selection and routing data are submitted with the latest snapshot so a
// settings-only save cannot silently discard them.
func (s *SettingsService) Update(next AppSettings) (AppSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Keep full-replace writes from older UI bindings backward-compatible.
	if next.DNSEgressMode == "" {
		next.DNSEgressMode = DNSEgressAuto
	}
	if err := validateSettings(next); err != nil {
		return AppSettings{}, err
	}
	// The compatibility result is device-owned state, not a user-editable
	// preference. Ordinary settings writes must not erase or forge it.
	next.WFPCompatibility = s.settings.WFPCompatibility
	if !s.settings.StrictRoute && next.StrictRoute {
		next.WFPCompatibility = WFPCompatibilityState{}
	}
	next.SelectedAdapterIDs = uniqueNonEmpty(next.SelectedAdapterIDs)
	next.DNSAdapterID = strings.TrimSpace(next.DNSAdapterID)
	next.AdapterWeights = cloneWeights(next.AdapterWeights)
	next.RoutingRules = append([]RoutingRule(nil), next.RoutingRules...)
	if next.AdapterWeights == nil {
		next.AdapterWeights = map[string]int{}
	}
	if next.RoutingRules == nil {
		next.RoutingRules = []RoutingRule{}
	}
	if err := s.commitLocked(next); err != nil {
		return AppSettings{}, err
	}
	return cloneSettings(s.settings), nil
}

func (s *SettingsService) SetAutostart(enabled bool) (AppSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	previousEnabled, err := s.autostartEnabled()
	if err != nil {
		return AppSettings{}, err
	}
	if err := s.setAutostart(enabled); err != nil {
		return AppSettings{}, err
	}
	next := cloneSettings(s.settings)
	next.Autostart = enabled
	if !enabled {
		next.AutoStartEngine = false
	}
	if err := s.commitLocked(next); err != nil {
		if rollbackErr := s.setAutostart(previousEnabled); rollbackErr != nil {
			return AppSettings{}, fmt.Errorf("%w；恢复开机自启状态失败：%v", err, rollbackErr)
		}
		return AppSettings{}, err
	}
	return cloneSettings(s.settings), nil
}

func (s *SettingsService) SetAutoStartEngine(enabled bool) (AppSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if enabled {
		autostartEnabled, err := s.autostartEnabled()
		if err != nil {
			return AppSettings{}, err
		}
		if !autostartEnabled {
			return AppSettings{}, errors.New("请先开启开机自动启动")
		}
	}
	next := cloneSettings(s.settings)
	if enabled {
		next.Autostart = true
	}
	next.AutoStartEngine = enabled
	if err := s.commitLocked(next); err != nil {
		return AppSettings{}, err
	}
	return cloneSettings(s.settings), nil
}

func (s *SettingsService) UpdateHome(
	mode string,
	weighted bool,
	selectedIDs []string,
	weights map[string]int,
) (AppSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if mode != "proxy" && mode != "tun" {
		return AppSettings{}, fmt.Errorf("不支持的运行模式：%s", mode)
	}
	cleanWeights := make(map[string]int, len(weights))
	for id, weight := range weights {
		if weight < AdapterWeightMin || weight > AdapterWeightMax {
			return AppSettings{}, fmt.Errorf("网卡 %s 的调度权重必须在 %d–%d 之间", id, AdapterWeightMin, AdapterWeightMax)
		}
		cleanWeights[id] = weight
	}
	next := cloneSettings(s.settings)
	next.Mode = mode
	next.Weighted = weighted
	next.SelectedAdapterIDs = uniqueNonEmpty(selectedIDs)
	next.AdapterWeights = cleanWeights
	if err := s.commitLocked(next); err != nil {
		return AppSettings{}, err
	}
	return cloneSettings(s.settings), nil
}

func (s *SettingsService) saveRoutingRules(rules []RoutingRule) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := cloneSettings(s.settings)
	next.RoutingRules = append([]RoutingRule(nil), rules...)
	return s.commitLocked(next)
}

func (s *SettingsService) reload() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		legacyPath, pathErr := legacyConfigPath()
		if pathErr != nil {
			return nil
		}
		legacyData, legacyErr := os.ReadFile(legacyPath)
		if legacyErr != nil {
			return nil
		}
		migrated, migrationErr := migrateLegacySettings(legacyData)
		if migrationErr != nil {
			return migrationErr
		}
		if err := s.commitLocked(migrated); err != nil {
			return err
		}
		s.migration = ConfigMigrationStatus{
			LegacyFound: true, Applied: true, LegacyPath: legacyPath,
			Message: "首次启动已迁移旧版网络配置与分流规则；界面偏好按设计恢复默认，原文件保持不变",
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("读取设置失败：%w", err)
	}
	var loaded AppSettings
	if err := json.Unmarshal(data, &loaded); err != nil {
		return fmt.Errorf("设置文件格式无效：%w", err)
	}
	defaults := DefaultSettings()
	if loaded.Mode != "proxy" && loaded.Mode != "tun" {
		loaded.Mode = defaults.Mode
	}
	if loaded.SOCKSPort == 0 {
		loaded.SOCKSPort = defaults.SOCKSPort
	}
	if loaded.HTTPPort == 0 {
		loaded.HTTPPort = defaults.HTTPPort
	}
	if loaded.DNSServer == "" {
		loaded.DNSServer = defaults.DNSServer
	}
	if loaded.DNSPolicy == "" {
		loaded.DNSPolicy = defaults.DNSPolicy
	}
	if loaded.DNSEgressMode == "" {
		loaded.DNSEgressMode = defaults.DNSEgressMode
	}
	loaded.DNSAdapterID = strings.TrimSpace(loaded.DNSAdapterID)
	if loaded.Language != "zh" && loaded.Language != "en" {
		loaded.Language = defaults.Language
	}
	if loaded.AdapterWeights == nil {
		loaded.AdapterWeights = map[string]int{}
	}
	if loaded.RoutingRules == nil {
		loaded.RoutingRules = []RoutingRule{}
	}
	if loaded.WFPCompatibility.Status != "failed" &&
		loaded.WFPCompatibility.Status != "healthy" {
		loaded.WFPCompatibility = WFPCompatibilityState{}
	} else {
		loaded.WFPCompatibility.Fingerprint = limitSettingText(
			loaded.WFPCompatibility.Fingerprint,
			512,
		)
		loaded.WFPCompatibility.Detail = limitSettingText(
			loaded.WFPCompatibility.Detail,
			1024,
		)
	}
	for id, weight := range loaded.AdapterWeights {
		if weight < AdapterWeightMin || weight > AdapterWeightMax {
			loaded.AdapterWeights[id] = AdapterWeightDefault
		}
	}
	s.settings = loaded
	return nil
}

func (s *SettingsService) inspectLegacyConfig() {
	legacyPath, err := legacyConfigPath()
	if err != nil {
		return
	}
	if info, statErr := os.Stat(legacyPath); statErr == nil && !info.IsDir() {
		s.mu.Lock()
		s.migration.LegacyFound = true
		s.migration.LegacyPath = legacyPath
		if s.migration.Message == "" {
			s.migration.Message = "检测到 HypoMux v2.x 配置，可手动迁移"
		}
		s.mu.Unlock()
	}
}

func legacyConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".hypomux", "config.json"), nil
}

func migrateLegacySettings(data []byte) (AppSettings, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return AppSettings{}, fmt.Errorf("旧版配置格式无效：%w", err)
	}
	result := DefaultSettings()
	decode := func(key string, target any) {
		if payload := raw[key]; payload != nil {
			_ = json.Unmarshal(payload, target)
		}
	}
	decode("run_mode", &result.Mode)
	decode("selected_adapters", &result.SelectedAdapterIDs)
	decode("socks_port", &result.SOCKSPort)
	decode("http_port", &result.HTTPPort)
	decode("weighted_scheduler", &result.Weighted)
	decode("wfp_strict_route", &result.StrictRoute)
	decode("wfp_compatibility_state", &result.WFPCompatibility)
	decode("force_tun_connectivity_bypass", &result.ForceTUNBypass)
	decode("blocked_domain_bypass", &result.BlockedDomainBypass)
	decode("blocked_domain_expiry", &result.BlockedDomainExpiry)
	decode("dns_server", &result.DNSServer)
	decode("doh_provider", &result.DNSPolicy)
	decode("nic_bandwidth_limits", &result.AdapterWeights)
	if payload := raw["routing_rules"]; payload != nil {
		rules, err := parseRoutingRulesJSON(payload)
		if err != nil {
			return AppSettings{}, fmt.Errorf("旧版分流规则迁移失败：%w", err)
		}
		result.RoutingRules = rules
	}
	if err := validateSettings(result); err != nil {
		return AppSettings{}, fmt.Errorf("旧版配置校验失败：%w", err)
	}
	return result, nil
}

func (s *SettingsService) rememberedWFPCompatibilityFailure() (bool, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	state := s.settings.WFPCompatibility
	if state.Status != "failed" || state.Fingerprint == "" ||
		state.Fingerprint != currentWFPFingerprint() {
		return false, ""
	}
	return true, state.Detail
}

func (s *SettingsService) RememberWFPCompatibilityFailure(detail string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := cloneSettings(s.settings)
	next.WFPCompatibility = WFPCompatibilityState{
		Status:      "failed",
		Fingerprint: currentWFPFingerprint(),
		Detail:      limitSettingText(detail, 1024),
	}
	return s.commitLocked(next)
}

func (s *SettingsService) ClearWFPCompatibilityFailure() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := cloneSettings(s.settings)
	next.WFPCompatibility = WFPCompatibilityState{
		Status:      "healthy",
		Fingerprint: currentWFPFingerprint(),
	}
	return s.commitLocked(next)
}

func validateSettings(value AppSettings) error {
	if value.Mode != "proxy" && value.Mode != "tun" {
		return fmt.Errorf("不支持的运行模式：%s", value.Mode)
	}
	if value.Language != "zh" && value.Language != "en" {
		return fmt.Errorf("不支持的界面语言：%s", value.Language)
	}
	if value.AutoStartEngine && !value.Autostart {
		return errors.New("开机自动启动加速需要先开启开机自启")
	}
	if value.SOCKSPort < 1 || value.SOCKSPort > 65534 {
		return errors.New("SOCKS5 端口必须在 1–65534 之间")
	}
	if value.HTTPPort < 1 || value.HTTPPort > 65534 {
		return errors.New("HTTP 端口必须在 1–65534 之间")
	}
	if value.SOCKSPort == value.HTTPPort {
		return errors.New("SOCKS5 与 HTTP 端口不能相同")
	}
	ip := net.ParseIP(value.DNSServer)
	if ip == nil || ip.To4() == nil {
		return errors.New("DNS 地址格式无效，请输入合法 IPv4 地址")
	}
	switch value.DNSPolicy {
	case "auto", "off", "system", "alidns", "dnspod", "google":
	default:
		return fmt.Errorf("不支持的 DoH 解析策略：%s", value.DNSPolicy)
	}
	switch value.DNSEgressMode {
	case DNSEgressAuto, DNSEgressSystem:
	case DNSEgressAdapter:
		if strings.TrimSpace(value.DNSAdapterID) == "" {
			return errors.New("指定 DNS 出口网卡时必须选择一张网卡")
		}
	default:
		return fmt.Errorf("不支持的 DNS 出口模式：%s", value.DNSEgressMode)
	}
	if len(value.DNSAdapterID) > 256 || strings.ContainsRune(value.DNSAdapterID, '\x00') {
		return errors.New("DNS 出口网卡标识无效")
	}
	for id, weight := range value.AdapterWeights {
		if weight < AdapterWeightMin || weight > AdapterWeightMax {
			return fmt.Errorf("网卡 %s 的调度权重必须在 %d–%d 之间", id, AdapterWeightMin, AdapterWeightMax)
		}
	}
	return nil
}

func limitSettingText(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit]
}

func (s *SettingsService) commitLocked(next AppSettings) error {
	if s.loadErr != nil {
		return fmt.Errorf("设置文件尚未成功加载，拒绝覆盖原文件：%w", s.loadErr)
	}
	next = cloneSettings(next)
	if err := writeSettingsFile(s.path, next); err != nil {
		return err
	}
	s.settings = next
	return nil
}

func writeSettingsFile(path string, settings AppSettings) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("创建设置目录失败：%w", err)
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化设置失败：%w", err)
	}
	temporary := path + ".tmp"
	file, err := os.OpenFile(temporary, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("写入设置失败：%w", err)
	}
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(temporary)
		}
	}()
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return fmt.Errorf("写入设置失败：%w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("同步设置失败：%w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("关闭设置文件失败：%w", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("提交设置失败：%w", err)
	}
	removeTemporary = false
	return nil
}

func cloneSettings(value AppSettings) AppSettings {
	result := value
	result.SelectedAdapterIDs = append([]string(nil), value.SelectedAdapterIDs...)
	result.AdapterWeights = cloneWeights(value.AdapterWeights)
	result.RoutingRules = append([]RoutingRule(nil), value.RoutingRules...)
	return result
}

func cloneWeights(value map[string]int) map[string]int {
	result := make(map[string]int, len(value))
	for key, weight := range value {
		result[key] = weight
	}
	return result
}

func uniqueNonEmpty(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
