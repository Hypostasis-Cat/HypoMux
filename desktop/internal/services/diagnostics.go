package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/Hypostasis-Cat/HypoMux/desktop/internal/platform"
)

const diagnosticTargetIPv4 = "223.5.5.5"
const diagnosticTargetIPv6 = "2400:3200::1"

type DiagnosticCheck struct {
	Key    string `json:"key"`
	Level  string `json:"level"`
	Detail string `json:"detail"`
	Mode   string `json:"mode,omitempty"`
}

type DiagnosticResult struct {
	AdapterID      string            `json:"adapter_id"`
	Name           string            `json:"name"`
	Address        string            `json:"address"`
	Status         string            `json:"status"`
	LossRate       int               `json:"loss_rate"` // -1 means no valid ICMP loss measurement.
	AvgLatencyMS   int               `json:"avg_latency_ms"`
	JitterMS       int               `json:"jitter_ms"`
	Sent           int               `json:"sent"`
	Received       int               `json:"received"`
	TargetIP       string            `json:"target_ip"`
	Note           string            `json:"note,omitempty"`
	BoundTCPOK     bool              `json:"bound_tcp_ok"`
	BoundTCPDetail string            `json:"bound_tcp_detail"`
	Checks         []DiagnosticCheck `json:"checks"`
	CompletedAt    time.Time         `json:"completed_at"`
}

type DiagnosticSnapshot struct {
	State       string             `json:"state"`
	RunID       string             `json:"run_id,omitempty"`
	TargetIP    string             `json:"target_ip"`
	StartedAt   time.Time          `json:"started_at,omitempty"`
	CompletedAt time.Time          `json:"completed_at,omitempty"`
	Total       int                `json:"total"`
	Completed   int                `json:"completed"`
	Results     []DiagnosticResult `json:"results"`
	Error       string             `json:"error,omitempty"`
}

type icmpProbeResult struct {
	Status       string
	LossRate     int
	AvgLatencyMS int
	JitterMS     int
	Sent         int
	Received     int
	Note         string
}

type diagnosticProbe interface {
	ICMP(context.Context, string, string) icmpProbeResult
	BoundEgress(context.Context, AdapterView) diagnosticEgressResult
}

type DiagnosticsService struct {
	mu           sync.Mutex
	settings     *SettingsService
	adapters     *AdapterService
	desktop      platform.DesktopHost
	logs         *SupportLogStore
	probe        diagnosticProbe
	listAdapters func() ([]AdapterView, error)
	cancel       context.CancelFunc
	natCancel    context.CancelFunc
	natRunning   bool
	natRunGuard  func() error
	detectNAT    func(context.Context, AdapterView, []NATServer) NATDetectionResult
	natServers   *natServerStore
	latest       DiagnosticSnapshot
	natLatest    NATDetectionResult
}

func NewDiagnosticsService(
	settings *SettingsService,
	adapters *AdapterService,
	desktop platform.DesktopHost,
	logs *SupportLogStore,
	natRunGuard func() error,
) *DiagnosticsService {
	return &DiagnosticsService{
		settings: settings, adapters: adapters, desktop: desktop, logs: logs,
		probe:        newDiagnosticProbe(),
		natRunGuard:  natRunGuard,
		detectNAT:    detectAdapterNAT,
		natServers:   newDefaultNATServerStore(),
		listAdapters: adapters.List,
		latest:       DiagnosticSnapshot{State: "idle", TargetIP: diagnosticTargetIPv4, Results: []DiagnosticResult{}},
		natLatest:    NATDetectionResult{State: "idle"},
	}
}

func (s *DiagnosticsService) Latest() DiagnosticSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneDiagnosticSnapshot(s.latest)
}

func (s *DiagnosticsService) NATLatest() NATDetectionResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.natLatest
}

func (s *DiagnosticsService) NATServers() NATServerSnapshot {
	return s.natServerStore().snapshot()
}

func (s *DiagnosticsService) SelectNATServer(id string) (NATServerSnapshot, error) {
	return s.natServerStore().selectServer(id)
}

func (s *DiagnosticsService) AddNATServer(name string, address string) (NATServerSnapshot, error) {
	return s.natServerStore().add(name, address)
}

func (s *DiagnosticsService) RemoveNATServer(id string) (NATServerSnapshot, error) {
	return s.natServerStore().remove(id)
}

func (s *DiagnosticsService) ResetNATServers() (NATServerSnapshot, error) {
	return s.natServerStore().reset()
}

func (s *DiagnosticsService) NATFirewallState() NATFirewallState {
	return currentNATFirewallState()
}

func (s *DiagnosticsService) AllowNATFirewallTraffic() (NATFirewallState, error) {
	return allowNATFirewallTraffic()
}

func (s *DiagnosticsService) natServerStore() *natServerStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.natServers == nil {
		s.natServers = newDefaultNATServerStore()
	}
	return s.natServers
}

func (s *DiagnosticsService) RunNAT(adapterID string, serverID string) (NATDetectionResult, error) {
	s.mu.Lock()
	if s.natRunning {
		s.mu.Unlock()
		return NATDetectionResult{}, errors.New("NAT 类型检测已在运行")
	}
	guard := s.natRunGuard
	s.mu.Unlock()
	if guard != nil {
		if err := guard(); err != nil {
			return NATDetectionResult{}, err
		}
	}
	s.mu.Lock()
	if s.natRunning {
		s.mu.Unlock()
		return NATDetectionResult{}, errors.New("NAT 类型检测已在运行")
	}
	s.natRunning = true
	s.mu.Unlock()
	reserved := true
	defer func() {
		if !reserved {
			return
		}
		s.mu.Lock()
		s.natRunning = false
		s.mu.Unlock()
	}()
	servers, err := s.natServerStore().selectedServers(serverID)
	if err != nil {
		return NATDetectionResult{}, err
	}

	available, err := s.listAdapters()
	if err != nil {
		return NATDetectionResult{}, fmt.Errorf("扫描网络适配器失败：%w", err)
	}
	var selected AdapterView
	for _, adapter := range available {
		if adapter.ID == adapterID && adapter.Operational && adapter.Address != "" {
			selected = adapter
			break
		}
	}
	if selected.ID == "" {
		return NATDetectionResult{}, errors.New("请选择一张拥有有效 IPv4 的活动网卡")
	}

	ctx, cancel := context.WithCancel(context.Background())
	started := time.Now()
	s.mu.Lock()
	s.natCancel = cancel
	s.natLatest = NATDetectionResult{
		State: "running", AdapterID: selected.ID, Name: selected.Name,
		Address: selected.Address, StartedAt: started,
	}
	s.mu.Unlock()

	detector := s.detectNAT
	if detector == nil {
		detector = detectAdapterNAT
	}
	result := detector(ctx, selected, servers)
	if result.AdapterID == "" {
		result.AdapterID, result.Name, result.Address = selected.ID, selected.Name, selected.Address
	}
	if result.StartedAt.IsZero() {
		result.StartedAt = started
	}
	if result.CompletedAt.IsZero() {
		result.CompletedAt = time.Now()
	}
	if result.DurationMS == 0 {
		result.DurationMS = result.CompletedAt.Sub(result.StartedAt).Milliseconds()
	}
	if ctx.Err() != nil {
		result.State = "cancelled"
		result.Detail = "NAT detection cancelled"
	}
	cancel()

	s.mu.Lock()
	s.natCancel = nil
	s.natRunning = false
	s.natLatest = result
	s.mu.Unlock()
	reserved = false
	s.logs.RecordEvent("nat_detection", result.State, map[string]any{
		"adapter": selected.Name, "source_ip": selected.Address,
		"nat_type": result.NATType, "mapping": result.MappingBehavior,
		"filtering": result.FilteringBehavior, "public_endpoint": result.PublicEndpoint,
		"server": result.Server, "duration_ms": result.DurationMS, "detail": result.Detail,
	})
	return result, nil
}

func (s *DiagnosticsService) CancelNAT() NATDetectionResult {
	s.mu.Lock()
	cancel := s.natCancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return s.NATLatest()
}

func (s *DiagnosticsService) Run(adapterIDs []string) (DiagnosticSnapshot, error) {
	s.mu.Lock()
	if s.latest.State == "running" {
		s.mu.Unlock()
		return DiagnosticSnapshot{}, errors.New("网络体检已在运行")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.cancel = cancel
	// Reserve the run before adapter discovery, so concurrent callers cannot
	// replace each other's cancellation handle or results.
	s.latest.State = "running"
	s.mu.Unlock()

	available, err := s.listAdapters()
	if err != nil {
		return s.failRun(fmt.Errorf("扫描网络适配器失败：%w", err))
	}
	if ctx.Err() != nil {
		return s.failRun(errors.New("网络体检已取消"))
	}
	wanted := make(map[string]struct{}, len(adapterIDs))
	for _, id := range adapterIDs {
		if id != "" {
			wanted[id] = struct{}{}
		}
	}
	selected := make([]AdapterView, 0, len(wanted))
	for _, adapter := range available {
		if _, ok := wanted[adapter.ID]; ok && adapter.Operational && (adapter.Address != "" || adapter.SourceIPv6 != "") {
			selected = append(selected, adapter)
		}
	}
	if len(selected) == 0 {
		return s.failRun(errors.New("请至少选择一张拥有有效 IPv4 或 IPv6 的活动网卡"))
	}

	started := time.Now()
	targetIP := diagnosticTargetIPv4
	if !selectedAdaptersHaveIPv4(selected) {
		targetIP = diagnosticTargetIPv6
	}
	runID := fmt.Sprintf("diag-%x", started.UnixNano())
	s.mu.Lock()
	s.latest = DiagnosticSnapshot{
		State: "running", RunID: runID, TargetIP: targetIP,
		StartedAt: started, Total: len(selected), Results: []DiagnosticResult{},
	}
	s.mu.Unlock()

	names := make([]string, 0, len(selected))
	for _, adapter := range selected {
		names = append(names, adapter.Name)
	}
	logOwned := s.logs.Start("adapter-diagnostic", names, map[string]any{
		"target_ip":     targetIP,
		"adapter_count": len(selected),
	})
	s.logs.RecordEvent("adapter_diagnostic", "started", map[string]any{
		"run_id": runID, "adapters": names, "target_ip": targetIP,
	})

	for _, adapter := range selected {
		select {
		case <-ctx.Done():
			return s.completeRun("cancelled", "", logOwned), nil
		default:
		}
		result := s.runAdapter(ctx, adapter)
		if ctx.Err() != nil {
			return s.completeRun("cancelled", "", logOwned), nil
		}
		current, scanErr := s.listAdapters()
		if scanErr != nil || !diagnosticAdapterUnchanged(adapter, current) {
			result.Status = "unverified"
			result.Checks = append(result.Checks, DiagnosticCheck{Key: "environment", Level: "warn", Detail: "体检期间网卡地址、接口或路由配置变化，或无法复查；本次结果仅供参考，请重新体检"})
		}
		s.mu.Lock()
		if s.latest.RunID == runID {
			s.latest.Results = append(s.latest.Results, result)
			s.latest.Completed = len(s.latest.Results)
		}
		s.mu.Unlock()
		s.logs.RecordEvent("adapter_diagnostic", "result", map[string]any{
			"adapter":   adapter.Name,
			"source_ip": result.Address,
			"target_ip": result.TargetIP,
			"status":    result.Status,
			"packets": map[string]any{
				"sent": result.Sent, "received": result.Received, "loss_rate": result.LossRate,
			},
			"latency_ms": map[string]any{
				"average": result.AvgLatencyMS, "jitter": result.JitterMS,
			},
			"bound_tcp":            result.BoundTCPDetail,
			"configuration_checks": result.Checks,
			"note":                 result.Note,
		})
	}
	return s.completeRun("completed", "", logOwned), nil
}

func (s *DiagnosticsService) Cancel() DiagnosticSnapshot {
	s.mu.Lock()
	cancel := s.cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return s.Latest()
}

func (s *DiagnosticsService) Logs() SupportLogSnapshot {
	return s.logs.Snapshot()
}

func (s *DiagnosticsService) ExportLogs() (string, error) {
	data, err := s.logs.Raw()
	if err != nil {
		return "", fmt.Errorf("读取诊断日志失败：%w", err)
	}
	path, err := s.desktop.SaveTextFile("导出 HypoMux 诊断日志", "hypomux-support.log")
	if err != nil {
		return "", fmt.Errorf("打开导出位置失败：%w", err)
	}
	if path == "" {
		return "", nil
	}
	if filepath.Ext(path) == "" {
		path += ".log"
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", fmt.Errorf("写入诊断日志失败：%w", err)
	}
	return path, nil
}

func (s *DiagnosticsService) OpenLogDirectory() error {
	if err := os.MkdirAll(s.logs.Directory(), 0o755); err != nil {
		return fmt.Errorf("创建日志目录失败：%w", err)
	}
	return s.desktop.OpenDirectory(s.logs.Directory())
}

func (s *DiagnosticsService) Shutdown() {
	s.mu.Lock()
	cancel := s.cancel
	natCancel := s.natCancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if natCancel != nil {
		natCancel()
	}
}

func (s *DiagnosticsService) runAdapter(ctx context.Context, adapter AdapterView) DiagnosticResult {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	source, target := adapter.Address, diagnosticTargetIPv4
	if source == "" && adapter.SourceIPv6 != "" {
		source, target = adapter.SourceIPv6, diagnosticTargetIPv6
	}
	var icmp icmpProbeResult
	var v4, v6 diagnosticEgressResult
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); icmp = s.probe.ICMP(ctx, source, target) }()
	if adapter.Address != "" {
		wg.Add(1)
		go func() { defer wg.Done(); v4 = s.probe.BoundEgress(ctx, adapter) }()
	}
	if adapter.SourceIPv6 != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ipv6 := adapter
			ipv6.Address = ""
			v6 = s.probe.BoundEgress(ctx, ipv6)
		}()
	}
	wg.Wait()
	tcpOK := v4.TCP || v6.TCP
	var details []string
	for _, detail := range []string{v4.Detail, v6.Detail} {
		if detail != "" {
			details = append(details, detail)
		}
	}
	result := DiagnosticResult{
		AdapterID: adapter.ID, Name: adapter.Name, Address: source,
		Status: icmp.Status, LossRate: icmp.LossRate,
		AvgLatencyMS: icmp.AvgLatencyMS, JitterMS: icmp.JitterMS,
		Sent: icmp.Sent, Received: icmp.Received, TargetIP: target,
		Note: icmp.Note, BoundTCPOK: tcpOK, BoundTCPDetail: strings.Join(details, " | "),
		CompletedAt: time.Now(),
	}
	// A finite probe cannot prove general unavailability. ICMP is quality
	// evidence only; a TCP accept alone may come from a local TUN stack.
	if v4.TLS || v6.TLS {
		result.Status = "available"
	} else if tcpOK || icmp.Received > 0 {
		result.Status = "limited"
	} else {
		result.Status = "unverified"
	}
	result.Checks = buildDiagnosticChecks(adapter, result)
	for _, family := range []struct {
		key, source string
		probe       diagnosticEgressResult
	}{
		{"ipv4_connectivity", adapter.Address, v4}, {"ipv6_connectivity", adapter.SourceIPv6, v6},
	} {
		if family.source == "" {
			continue
		}
		level := "warn"
		if family.probe.TLS {
			level = "pass"
		}
		result.Checks = append(result.Checks, DiagnosticCheck{Key: family.key, Level: level, Detail: family.probe.Detail})
	}
	result.Checks = append(result.Checks, DiagnosticCheck{Key: "scope", Level: "info", Detail: "结果仅验证指定目标的接口绑定 TCP/TLS；不验证 DNS、所有网站或聚合转发。TUN/VPN/防火墙可能影响探测路径。ICMP 未回应率仅针对所示目标。"})
	return result
}

func diagnosticAdapterUnchanged(before AdapterView, current []AdapterView) bool {
	for _, after := range current {
		if after.ID != before.ID {
			continue
		}
		return after.Operational && before.Address == after.Address && before.SourceIPv6 == after.SourceIPv6 &&
			before.IfIndex == after.IfIndex && before.IPv6IfIndex == after.IPv6IfIndex &&
			before.Gateway == after.Gateway && before.IPv6Gateway == after.IPv6Gateway &&
			before.Metric == after.Metric && before.IPv6Metric == after.IPv6Metric && reflect.DeepEqual(before.DNSServers, after.DNSServers)
	}
	return false
}

func buildDiagnosticChecks(adapter AdapterView, result DiagnosticResult) []DiagnosticCheck {
	sourceLevel := "pass"
	sourceDetail := adapter.Address
	if sourceDetail == "" {
		sourceDetail = adapter.SourceIPv6
	}
	if result.BoundTCPDetail != "" {
		sourceDetail = result.BoundTCPDetail
	}
	if !result.BoundTCPOK {
		sourceLevel = "warn"
		sourceDetail = result.BoundTCPDetail
	}
	dns := ""
	for _, server := range adapter.DNSServers {
		if server == "" {
			continue
		}
		if dns != "" {
			dns += ", "
		}
		dns += server
	}
	metric, autoMetric, gateway := adapter.Metric, adapter.AutoMetric, adapter.Gateway
	if adapter.Address == "" && adapter.SourceIPv6 != "" {
		metric, autoMetric, gateway = adapter.IPv6Metric, adapter.IPv6AutoMetric, adapter.IPv6Gateway
	}
	metricLevel, metricDetail := "pass", fmt.Sprintf("%d", metric)
	if metric < 0 {
		metricLevel, metricDetail = "warn", ""
	}
	mode := "fixed"
	if autoMetric {
		mode = "auto"
	}
	return []DiagnosticCheck{
		{Key: "source_binding", Level: sourceLevel, Detail: sourceDetail},
		{Key: "gateway", Level: levelForValue(gateway), Detail: gateway},
		{Key: "dns", Level: levelForValue(dns), Detail: dns},
		{Key: "metric", Level: metricLevel, Detail: metricDetail, Mode: mode},
	}
}

func levelForValue(value string) string {
	if value == "" {
		return "warn"
	}
	return "pass"
}

func (s *DiagnosticsService) completeRun(state string, message string, logOwned bool) DiagnosticSnapshot {
	s.mu.Lock()
	s.latest.State = state
	s.latest.Error = message
	s.latest.CompletedAt = time.Now()
	s.cancel = nil
	snapshot := cloneDiagnosticSnapshot(s.latest)
	s.mu.Unlock()
	s.logs.RecordEvent("adapter_diagnostic", state, map[string]any{
		"run_id": snapshot.RunID, "completed": snapshot.Completed, "total": snapshot.Total,
	})
	if logOwned {
		s.logs.Finish("diagnostic_" + state)
	}
	return snapshot
}

func (s *DiagnosticsService) failRun(err error) (DiagnosticSnapshot, error) {
	s.mu.Lock()
	s.latest = DiagnosticSnapshot{
		State: "error", TargetIP: diagnosticTargetIPv4,
		CompletedAt: time.Now(), Error: err.Error(), Results: []DiagnosticResult{},
	}
	s.cancel = nil
	snapshot := cloneDiagnosticSnapshot(s.latest)
	s.mu.Unlock()
	return snapshot, err
}

func cloneDiagnosticSnapshot(value DiagnosticSnapshot) DiagnosticSnapshot {
	value.Results = append([]DiagnosticResult(nil), value.Results...)
	for index := range value.Results {
		value.Results[index].Checks = append([]DiagnosticCheck(nil), value.Results[index].Checks...)
	}
	return value
}
