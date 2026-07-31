package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Hypostasis-Cat/HypoMux/desktop/internal/engineclient"
)

type engineState struct {
	State          string    `json:"state"`
	Sequence       uint64    `json:"sequence"`
	StateChangedAt time.Time `json:"state_changed_at"`
	Reason         string    `json:"reason,omitempty"`
}

type engineStatusResult struct {
	Engine engineState `json:"engine"`
}

type engineEndpoints struct {
	SOCKS    string            `json:"socks,omitempty"`
	HTTP     string            `json:"http,omitempty"`
	Channels map[string]string `json:"channels,omitempty"`
}

type engineStartResult struct {
	State     engineState     `json:"state"`
	Mode      string          `json:"mode"`
	Endpoints engineEndpoints `json:"endpoints"`
}

type tunStatus struct {
	State     string `json:"state"`
	LastError string `json:"last_error,omitempty"`
}

type tunLifecycleResult struct {
	Accepted bool      `json:"accepted"`
	Tun      tunStatus `json:"tun"`
}

type adapterTelemetry struct {
	Name        string `json:"name"`
	Connections int    `json:"connections"`
	BytesUp     int64  `json:"bytes_up"`
	BytesDown   int64  `json:"bytes_down"`
	HealthState string `json:"health_state"`
	HealthFails int64  `json:"health_failures"`
	HealthOK    int64  `json:"health_successes"`
}

type connectionTelemetry struct {
	ID        uint64    `json:"id"`
	Protocol  string    `json:"protocol"`
	Channel   string    `json:"channel,omitempty"`
	Client    string    `json:"client"`
	Listener  string    `json:"listener,omitempty"`
	Target    string    `json:"target,omitempty"`
	Remote    string    `json:"remote,omitempty"`
	Adapter   string    `json:"adapter,omitempty"`
	StartedAt time.Time `json:"started_at"`
	BytesUp   int64     `json:"bytes_up"`
	BytesDown int64     `json:"bytes_down"`
}

type telemetryResult struct {
	StartedAt         time.Time             `json:"started_at"`
	SampledAt         time.Time             `json:"sampled_at"`
	Adapters          []adapterTelemetry    `json:"adapters"`
	Connections       []connectionTelemetry `json:"active_connections"`
	DomainQuarantines []struct {
		Adapter   string    `json:"adapter"`
		Domain    string    `json:"domain"`
		Evidence  int       `json:"evidence"`
		ExpiresAt time.Time `json:"expires_at"`
	} `json:"domain_quarantines"`
	Total struct {
		Connections int   `json:"connections"`
		BytesUp     int64 `json:"bytes_up"`
		BytesDown   int64 `json:"bytes_down"`
	} `json:"total"`
}

type AdapterRuntime struct {
	ID          string  `json:"id"`
	DownloadBPS float64 `json:"download_bps"`
	UploadBPS   float64 `json:"upload_bps"`
	Connections int     `json:"connections"`
	BytesDown   int64   `json:"bytes_down"`
	BytesUp     int64   `json:"bytes_up"`
	HealthState string  `json:"health_state"`
}

type EngineSnapshot struct {
	Phase         string           `json:"phase"`
	Mode          string           `json:"mode"`
	Weighted      bool             `json:"weighted"`
	Reason        string           `json:"reason,omitempty"`
	CoreConnected bool             `json:"core_connected"`
	CoreVersion   string           `json:"core_version,omitempty"`
	CoreElevated  bool             `json:"core_elevated"`
	DownloadBPS   float64          `json:"download_bps"`
	UploadBPS     float64          `json:"upload_bps"`
	Connections   int              `json:"connections"`
	SessionBytes  int64            `json:"session_bytes"`
	Adapters      []AdapterRuntime `json:"adapters"`
	SampledAt     time.Time        `json:"sampled_at"`
}

type WFPRepairResult struct {
	Elevated        bool   `json:"elevated"`
	BFERunning      bool   `json:"bfe_running"`
	EngineReady     bool   `json:"engine_ready"`
	RepairAttempted bool   `json:"repair_attempted"`
	Repaired        bool   `json:"repaired"`
	Detail          string `json:"detail,omitempty"`
}

type telemetrySample struct {
	at       time.Time
	down     int64
	up       int64
	adapters map[string][2]int64
}

type EngineService struct {
	mu                  sync.Mutex
	client              *engineclient.Client
	settings            *SettingsService
	adapters            *AdapterService
	logs                *SupportLogStore
	tun                 *TunService
	last                telemetrySample
	lastTUNHealthCheck  time.Time
	tunHealthFailures   int
	watchdogStopping    bool
	blockedDomains      *BlockedDomainService
	dnsFallbackApplied  bool
	wfpFallbackApplied  bool
	compatRestarting    bool
	compatibilityNotice string
	closing             bool
}

type dnsFallbackEvent struct {
	Adapter string `json:"adapter"`
	Policy  string `json:"policy"`
	Reason  string `json:"reason"`
}

func NewEngineService(settings *SettingsService, adapters *AdapterService, logs ...*SupportLogStore) *EngineService {
	return newEngineService(settings, adapters, nil, logs...)
}

func NewEngineServiceWithDomains(
	settings *SettingsService,
	adapters *AdapterService,
	blockedDomains *BlockedDomainService,
	logs ...*SupportLogStore,
) *EngineService {
	return newEngineService(settings, adapters, blockedDomains, logs...)
}

func newEngineService(
	settings *SettingsService,
	adapters *AdapterService,
	blockedDomains *BlockedDomainService,
	logs ...*SupportLogStore,
) *EngineService {
	_ = restoreSystemProxy()
	var supportLogs *SupportLogStore
	if len(logs) > 0 {
		supportLogs = logs[0]
	}
	service := &EngineService{
		client: engineclient.New(), settings: settings, adapters: adapters,
		logs: supportLogs, tun: NewTunService(settings, adapters),
		blockedDomains: blockedDomains,
	}
	go service.consumeCoreEvents()
	return service
}

func (s *EngineService) consumeCoreEvents() {
	for event := range s.client.Events() {
		switch event.Name {
		case "dns.fallback_required":
			var fallback dnsFallbackEvent
			if json.Unmarshal(event.Data, &fallback) == nil {
				s.handleDNSFallback(fallback)
			}
		case "tun.state_changed":
			var status tunStatus
			if json.Unmarshal(event.Data, &status) == nil &&
				status.State == "failed" && isWFPCompatibilityError(status.LastError) {
				s.handleWFPCompatibility(status)
			}
		}
	}
}

func (s *EngineService) handleDNSFallback(event dnsFallbackEvent) {
	s.mu.Lock()
	settings := s.settings.Get()
	if s.closing || settings.Mode != "tun" || s.dnsFallbackApplied || s.compatRestarting {
		s.mu.Unlock()
		return
	}
	s.dnsFallbackApplied = true
	s.compatRestarting = true
	s.compatibilityNotice = ""
	if s.logs != nil {
		s.logs.RecordEvent("dns_compatibility", "restart_requested", map[string]any{
			"adapter": event.Adapter, "policy": event.Policy, "reason": event.Reason,
		})
	}
	s.mu.Unlock()

	_, stopErr := s.Stop()
	_, startErr := s.Start("tun")

	s.mu.Lock()
	defer s.mu.Unlock()
	s.compatRestarting = false
	if stopErr != nil || startErr != nil {
		s.compatibilityNotice = "提示：DoH 兼容模式重启失败，聚合已停止；请检查日志后重试"
		if s.logs != nil {
			s.logs.RecordEvent("dns_compatibility", "restart_failed", map[string]any{
				"stop_error": errorText(stopErr), "start_error": errorText(startErr),
			})
		}
		return
	}
	s.compatibilityNotice = "提示：检测到当前网络的 DoH 不兼容，已自动切换传统 DNS 并完成一次受控重启"
	if s.logs != nil {
		s.logs.RecordEvent("dns_compatibility", "restart_completed", map[string]any{
			"adapter": event.Adapter, "previous_policy": event.Policy,
		})
	}
}

func (s *EngineService) handleWFPCompatibility(status tunStatus) {
	s.mu.Lock()
	settings := s.settings.Get()
	if s.closing || settings.Mode != "tun" || !settings.StrictRoute ||
		s.wfpFallbackApplied || s.compatRestarting {
		s.mu.Unlock()
		return
	}
	s.wfpFallbackApplied = true
	s.compatRestarting = true
	s.compatibilityNotice = ""
	_ = s.settings.RememberWFPCompatibilityFailure(status.LastError)
	if s.logs != nil {
		s.logs.RecordEvent("wfp_compatibility", "restart_requested", map[string]any{
			"reason": status.LastError,
		})
	}
	s.mu.Unlock()

	_, stopErr := s.Stop()
	_, startErr := s.Start("tun")

	s.mu.Lock()
	defer s.mu.Unlock()
	s.compatRestarting = false
	if stopErr != nil || startErr != nil {
		s.compatibilityNotice = "提示：WFP 兼容模式重启失败，聚合已停止；请检查日志后重试"
		if s.logs != nil {
			s.logs.RecordEvent("wfp_compatibility", "restart_failed", map[string]any{
				"stop_error": errorText(stopErr), "start_error": errorText(startErr),
			})
		}
		return
	}
	s.compatibilityNotice = "提示：检测到当前设备的 WFP 严格路由不兼容，本次已降级兼容 TUN 并完成一次受控重启"
	if s.logs != nil {
		s.logs.RecordEvent("wfp_compatibility", "restart_completed", map[string]any{
			"reason": status.LastError,
		})
	}
}

func isWFPCompatibilityError(message string) bool {
	value := strings.ToLower(message)
	for _, marker := range []string{
		"wfp", "bfe", "fwpm", "filtering platform", "strict route", "dns leak",
	} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}

func (s *EngineService) Snapshot() (EngineSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	hello, err := s.client.Ensure(ctx)
	if err != nil {
		return EngineSnapshot{}, err
	}
	var status engineStatusResult
	if err := s.client.Request(ctx, "engine.status", nil, &status); err != nil {
		return EngineSnapshot{}, fmt.Errorf("读取聚合核心状态失败：%w", err)
	}
	settings := s.settings.Get()
	snapshot := EngineSnapshot{
		Phase: status.Engine.State, Mode: settings.Mode, Weighted: settings.Weighted, Reason: status.Engine.Reason,
		CoreConnected: true, CoreVersion: hello.EngineVersion, CoreElevated: hello.Elevated,
		SampledAt: time.Now(),
	}
	if s.compatibilityNotice != "" {
		snapshot.Reason = s.compatibilityNotice
	}
	if status.Engine.State != "running" {
		s.last = telemetrySample{}
		return snapshot, nil
	}
	var telemetry telemetryResult
	if err := s.client.Request(ctx, "engine.telemetry", map[string]any{"include_connections": true}, &telemetry); err != nil {
		return EngineSnapshot{}, fmt.Errorf("读取聚合遥测失败：%w", err)
	}
	snapshot.SampledAt = telemetry.SampledAt
	snapshot.Connections = telemetry.Total.Connections
	snapshot.SessionBytes = telemetry.Total.BytesDown + telemetry.Total.BytesUp
	elapsed := telemetry.SampledAt.Sub(s.last.at).Seconds()
	if elapsed > 0 && elapsed < 30 {
		snapshot.DownloadBPS = float64(max64(0, telemetry.Total.BytesDown-s.last.down)) / elapsed
		snapshot.UploadBPS = float64(max64(0, telemetry.Total.BytesUp-s.last.up)) / elapsed
	}
	current := telemetrySample{
		at: telemetry.SampledAt, down: telemetry.Total.BytesDown, up: telemetry.Total.BytesUp,
		adapters: map[string][2]int64{},
	}
	for _, item := range telemetry.Adapters {
		runtime := AdapterRuntime{
			ID: item.Name, Connections: item.Connections, BytesDown: item.BytesDown,
			BytesUp: item.BytesUp, HealthState: item.HealthState,
		}
		if previous, ok := s.last.adapters[item.Name]; ok && elapsed > 0 && elapsed < 30 {
			runtime.DownloadBPS = float64(max64(0, item.BytesDown-previous[0])) / elapsed
			runtime.UploadBPS = float64(max64(0, item.BytesUp-previous[1])) / elapsed
		}
		current.adapters[item.Name] = [2]int64{item.BytesDown, item.BytesUp}
		snapshot.Adapters = append(snapshot.Adapters, runtime)
	}
	s.last = current
	if s.blockedDomains != nil && settings.BlockedDomainBypass {
		runtimeEntries := make([]BlockedDomainEntry, 0, len(telemetry.DomainQuarantines))
		for _, entry := range telemetry.DomainQuarantines {
			runtimeEntries = append(runtimeEntries, BlockedDomainEntry{
				Adapter: entry.Adapter, Domain: entry.Domain, ExpiresAt: entry.ExpiresAt,
			})
		}
		_ = s.blockedDomains.ReplaceRuntime(runtimeEntries)
	}
	if settings.Mode == "tun" && !settings.ForceTUNBypass &&
		time.Since(s.lastTUNHealthCheck) >= 30*time.Second {
		s.lastTUNHealthCheck = time.Now()
		probeContext, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		_, tunErr := probeTUNConnectivity(probeContext)
		cancel()
		if tunErr == nil {
			s.tunHealthFailures = 0
		} else {
			physicalOK := false
			available, listErr := s.adapters.List()
			if listErr == nil {
				probe := newDiagnosticProbe()
				physicalContext, physicalCancel := context.WithTimeout(context.Background(), 8*time.Second)
				for _, adapter := range available {
					if !adapter.Selected {
						continue
					}
					if ok, _ := probe.BoundTCP(physicalContext, adapter); ok {
						physicalOK = true
						break
					}
				}
				physicalCancel()
			}
			if physicalOK {
				s.tunHealthFailures++
			} else {
				s.tunHealthFailures = 0
			}
			if s.tunHealthFailures >= 3 && !s.watchdogStopping {
				s.tunHealthFailures = 0
				s.watchdogStopping = true
				snapshot.Phase = "failed"
				snapshot.Reason = "检测到物理网络正常但虚拟网卡连续无法联网，正在自动停止并恢复网络设置"
				if s.logs != nil {
					s.logs.RecordEvent("tun_watchdog", "rollback", map[string]any{
						"message": tunErr.Error(),
					})
				}
				go func() {
					_, _ = s.Stop()
					s.mu.Lock()
					s.watchdogStopping = false
					s.mu.Unlock()
				}()
			}
		}
	}
	return snapshot, nil
}

// RepairWFP is an explicit settings action. It never elevates the WebView2
// host: the installed service is preferred, with the authenticated runas Core
// retained as the development fallback.
func (s *EngineService) RepairWFP() (WFPRepairResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return WFPRepairResult{}, errors.New("HypoMux 正在退出")
	}
	if hello := s.client.Hello(); hello.ProtocolVersion != 0 {
		var status engineStatusResult
		statusCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		statusErr := s.client.Request(statusCtx, "engine.status", nil, &status)
		cancel()
		if statusErr == nil && status.Engine.State != "stopped" && status.Engine.State != "failed" {
			return WFPRepairResult{}, errors.New("请先停止聚合，再修复 WFP/BFE 组件")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	hello, err := s.client.EnsureElevated(ctx)
	if err != nil {
		return WFPRepairResult{}, err
	}
	if !hello.Elevated {
		return WFPRepairResult{}, errors.New("独立聚合核心未获得管理员权限")
	}
	var result WFPRepairResult
	if err := s.client.Request(ctx, "wfp.inspect", map[string]any{"repair": true}, &result); err != nil {
		_ = s.settings.RememberWFPCompatibilityFailure(err.Error())
		return result, fmt.Errorf("WFP/BFE 修复失败：%w", err)
	}
	if result.EngineReady {
		_ = s.settings.ClearWFPCompatibilityFailure()
	} else {
		_ = s.settings.RememberWFPCompatibilityFailure(result.Detail)
	}
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	s.client.Shutdown(shutdownCtx)
	shutdownCancel()
	return result, nil
}

func (s *EngineService) Start(mode string) (snapshot EngineSnapshot, returnErr error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if mode != "proxy" && mode != "tun" {
		return EngineSnapshot{}, fmt.Errorf("不支持的运行模式：%s", mode)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	available, err := s.adapters.List()
	if err != nil {
		return EngineSnapshot{}, err
	}
	selected := make([]AdapterView, 0, len(available))
	for _, adapter := range available {
		if adapter.Selected {
			selected = append(selected, adapter)
		}
	}
	if len(selected) == 0 {
		return EngineSnapshot{}, errors.New("请至少选择一张活动网卡")
	}
	settings := s.settings.Get()
	if !s.compatRestarting {
		s.dnsFallbackApplied = false
		s.wfpFallbackApplied = false
		s.compatibilityNotice = ""
	}
	effectiveStrictRoute := settings.StrictRoute && !s.wfpFallbackApplied
	effectiveDNSPolicy := settings.DNSPolicy
	if mode == "tun" && s.dnsFallbackApplied {
		effectiveDNSPolicy = "off"
	}
	logOwned := false
	if s.logs != nil {
		names := make([]string, 0, len(selected))
		for _, adapter := range selected {
			names = append(names, adapter.Name)
		}
		logOwned = s.logs.Start(mode, names, map[string]any{
			"socks_port": settings.SOCKSPort,
			"http_port":  settings.HTTPPort,
			"weighted":   settings.Weighted,
			"dns_policy": settings.DNSPolicy,
		})
		s.logs.RecordEvent("engine", "start_requested", map[string]any{
			"mode": mode, "adapters": names,
		})
		defer func() {
			if returnErr == nil {
				return
			}
			s.logs.RecordEvent("engine", "start_failed", map[string]any{"message": returnErr.Error()})
			if logOwned {
				s.logs.Finish("start_failed")
			}
		}()
	}
	if mode == "tun" {
		if s.tun == nil {
			return EngineSnapshot{}, errors.New("TUN 启动前检查服务未注册；系统网络未修改")
		}
		preflight := s.tun.checkSelected(selected)
		effectiveStrictRoute = preflight.EffectiveStrictRoute && !s.wfpFallbackApplied
		if s.logs != nil {
			s.logs.RecordEvent("tun_preflight", "completed", map[string]any{
				"ready":                      preflight.Ready,
				"host_elevated":              preflight.HostElevated,
				"privilege_broker_available": preflight.PrivilegeBrokerAvailable,
				"wfp_ready":                  preflight.WFPReady,
				"strict_route_requested":     preflight.StrictRouteRequested,
				"effective_strict_route":     preflight.EffectiveStrictRoute,
				"foreign_tun":                preflight.ForeignTUN,
				"shared_gateway_risks":       preflight.SharedGatewayRisks,
				"issues":                     preflight.Issues,
			})
		}
		if blocker := firstTunBlocker(preflight); blocker != nil {
			return EngineSnapshot{}, fmt.Errorf("TUN 预检阻止启动：%w", blocker)
		}
	}
	var hello engineclient.Hello
	if mode == "tun" {
		hello, err = s.client.EnsureElevated(ctx)
	} else {
		hello, err = s.client.Ensure(ctx)
	}
	if err != nil {
		return EngineSnapshot{}, err
	}
	if settings.Mode != mode {
		settings.Mode = mode
		_, err = s.settings.UpdateHome(mode, settings.Weighted, settings.SelectedAdapterIDs, settings.AdapterWeights)
		if err != nil {
			return EngineSnapshot{}, err
		}
	}
	var status engineStatusResult
	if err := s.client.Request(ctx, "engine.status", nil, &status); err == nil && status.Engine.State == "failed" {
		var ignored any
		_ = s.client.Request(ctx, "engine.stop", nil, &ignored)
	}
	startPayload := map[string]any{
		"mode": mode, "listen_host": "127.0.0.1", "weighted": settings.Weighted,
		"connect_timeout_ms": 6000,
		"dns": map[string]any{
			"policy": effectiveDNSPolicy, "legacy_servers": []string{settings.DNSServer},
			"cache_ttl_ms": 60000, "query_timeout_ms": 4000,
		},
		"adapters":                engineAdapters(selected),
		"domain_isolation":        settings.BlockedDomainBypass,
		"domain_isolation_expiry": settings.BlockedDomainExpiry,
	}
	if settings.BlockedDomainBypass && s.blockedDomains != nil {
		if snapshot, snapshotErr := s.blockedDomains.List(); snapshotErr == nil {
			seeds := make([]map[string]any, 0, len(snapshot.Entries))
			for _, entry := range snapshot.Entries {
				expiresAt := entry.ExpiresAt
				if entry.Permanent {
					expiresAt = time.Now().AddDate(100, 0, 0)
				}
				seeds = append(seeds, map[string]any{
					"adapter": entry.Adapter, "domain": entry.Domain, "expires_at": expiresAt,
				})
			}
			startPayload["domain_quarantines"] = seeds
		}
	}
	if mode == "proxy" {
		startPayload["mode"] = "proxy"
		startPayload["socks_port"] = settings.SOCKSPort
		startPayload["http_port"] = settings.HTTPPort
	} else {
		startPayload["mode"] = "tun_tcp_pool"
		startPayload["channels"] = engineChannels(selected)
	}
	var started engineStartResult
	if err := s.client.Request(ctx, "engine.start", startPayload, &started); err != nil {
		return EngineSnapshot{}, fmt.Errorf("启动聚合核心失败：%w", err)
	}
	rollback := func(cause error) (EngineSnapshot, error) {
		var ignored any
		var tunIgnored tunLifecycleResult
		_ = s.client.Request(ctx, "tun.deactivate", nil, &tunIgnored)
		_ = s.client.Request(ctx, "engine.stop", nil, &ignored)
		_ = restoreSystemProxy()
		return EngineSnapshot{}, cause
	}
	if mode == "proxy" {
		if err := enableSystemProxy(settings.HTTPPort, settings.SOCKSPort); err != nil {
			return rollback(err)
		}
	} else {
		var dnsResult dnsResolveResult
		if err := s.client.Request(ctx, "dns.resolve", map[string]any{
			"domain": "www.msftconnecttest.com", "adapter": selected[0].Name, "record_type": "A",
		}, &dnsResult); err != nil {
			return rollback(fmt.Errorf("TUN 启动前 DNS 验证失败：%w", err))
		}
		rules, normalizeErr := normalizeRulesStrict(settings.RoutingRules)
		if normalizeErr != nil {
			return rollback(normalizeErr)
		}
		singBox, configPath, configErr := writeSingBoxConfig(started.Endpoints.Channels, selected[0], dnsResult, rules, effectiveStrictRoute)
		if configErr != nil {
			return rollback(configErr)
		}
		var activated tunLifecycleResult
		if err := s.client.Request(ctx, "tun.activate", map[string]any{
			"executable": singBox, "config_path": configPath, "startup_timeout_ms": 1500,
			"strict_route": effectiveStrictRoute,
		}, &activated); err != nil {
			if !hello.Elevated {
				return rollback(fmt.Errorf("TUN 需要管理员权限的独立聚合核心：%w", err))
			}
			return rollback(fmt.Errorf("启动 TUN 侧车失败：%w", err))
		}
		if activated.Tun.State != "running" {
			return rollback(fmt.Errorf("TUN 未进入稳定运行状态：%s", activated.Tun.LastError))
		}
		if effectiveStrictRoute {
			_ = s.settings.ClearWFPCompatibilityFailure()
		}
		if !settings.ForceTUNBypass {
			detail, validationErr := probeTUNConnectivity(ctx)
			if validationErr != nil {
				return rollback(fmt.Errorf(
					"虚拟网卡联网验证失败，已自动停止并恢复网络设置：%w",
					validationErr,
				))
			}
			if s.logs != nil {
				s.logs.RecordEvent("tun_connectivity", "startup_validated", map[string]any{
					"detail": detail,
				})
			}
		} else if s.logs != nil {
			s.logs.RecordEvent("tun_connectivity", "startup_bypassed", nil)
		}
	}
	s.last = telemetrySample{}
	s.lastTUNHealthCheck = time.Now()
	s.tunHealthFailures = 0
	if s.logs != nil {
		s.logs.RecordEvent("engine", "started", map[string]any{
			"mode": mode, "core_version": hello.EngineVersion, "elevated": hello.Elevated,
			"effective_dns_policy": effectiveDNSPolicy,
		})
	}
	return EngineSnapshot{
		Phase: "running", Mode: mode, Weighted: settings.Weighted, CoreConnected: true,
		CoreVersion: hello.EngineVersion, CoreElevated: hello.Elevated, SampledAt: time.Now(),
	}, nil
}

func (s *EngineService) Stop() (EngineSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.logs != nil {
		s.logs.RecordEvent("engine", "stop_requested", nil)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	hello, ensureErr := s.client.Ensure(ctx)
	var firstError error
	if ensureErr == nil {
		var tunResult tunLifecycleResult
		if err := s.client.Request(ctx, "tun.deactivate", nil, &tunResult); err != nil {
			var remote *engineclient.RemoteError
			if !errors.As(err, &remote) || remote.Code != "invalid_state" {
				firstError = fmt.Errorf("停止 TUN 失败：%w", err)
			}
		}
		var stopResult any
		if err := s.client.Request(ctx, "engine.stop", nil, &stopResult); err != nil {
			var remote *engineclient.RemoteError
			if !errors.As(err, &remote) || remote.Code != "invalid_state" {
				if firstError == nil {
					firstError = fmt.Errorf("停止聚合核心失败：%w", err)
				}
			}
		}
	}
	if err := restoreSystemProxy(); err != nil && firstError == nil {
		firstError = err
	}
	s.last = telemetrySample{}
	if !s.compatRestarting {
		s.dnsFallbackApplied = false
		s.wfpFallbackApplied = false
		s.compatibilityNotice = ""
	}
	if ensureErr != nil && firstError == nil {
		firstError = ensureErr
	}
	snapshot := EngineSnapshot{
		Phase: "stopped", Mode: s.settings.Get().Mode, Weighted: s.settings.Get().Weighted, CoreConnected: ensureErr == nil,
		CoreVersion: hello.EngineVersion, CoreElevated: hello.Elevated, SampledAt: time.Now(),
	}
	if s.logs != nil {
		fields := map[string]any{}
		reason := "stopped"
		if firstError != nil {
			fields["message"] = firstError.Error()
			reason = "stop_error"
		}
		s.logs.RecordEvent("engine", reason, fields)
		s.logs.Finish(reason)
	}
	return snapshot, firstError
}

func (s *EngineService) Shutdown() {
	s.mu.Lock()
	s.closing = true
	s.mu.Unlock()
	_, _ = s.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s.client.Shutdown(ctx)
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func engineAdapters(adapters []AdapterView) []map[string]any {
	result := make([]map[string]any, 0, len(adapters))
	for _, adapter := range adapters {
		result = append(result, map[string]any{
			"name": adapter.Name, "source_ip": adapter.Address, "if_index": adapter.IfIndex,
			"source_ipv6": adapter.SourceIPv6, "ipv6_if_index": adapter.IPv6IfIndex,
			"weight": adapter.Weight, "dns_servers": adapter.DNSServers,
		})
	}
	return result
}

func engineChannels(adapters []AdapterView) []map[string]any {
	all, wired, wireless := []string{}, []string{}, []string{}
	for _, adapter := range adapters {
		all = append(all, adapter.Name)
		if adapter.Kind == "wifi" {
			wireless = append(wireless, adapter.Name)
		} else {
			wired = append(wired, adapter.Name)
		}
	}
	if len(wired) == 0 {
		wired = append([]string(nil), all...)
	}
	if len(wireless) == 0 {
		wireless = append([]string(nil), all...)
	}
	channels := []map[string]any{
		{"name": "nic_ethernet", "port": 0, "adapter_names": wired},
		{"name": "nic_wifi", "port": 0, "adapter_names": wireless},
		{"name": "aggregation", "port": 0, "adapter_names": all},
		{"name": "direct", "port": 0, "adapter_names": []string{}},
	}
	for _, adapter := range adapters {
		channels = append(channels, map[string]any{
			"name": "nic_" + adapter.Name, "port": 0, "adapter_names": []string{adapter.Name},
		})
	}
	return channels
}

func max64(a int64, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
