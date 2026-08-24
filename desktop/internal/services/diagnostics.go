package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Hypostasis-Cat/HypoMux/desktop/internal/platform"
)

const diagnosticTargetIPv4 = "223.5.5.5"

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
	LossRate       int               `json:"loss_rate"`
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
	BoundTCP(context.Context, AdapterView) (bool, string)
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
) *DiagnosticsService {
	return &DiagnosticsService{
		settings: settings, adapters: adapters, desktop: desktop, logs: logs,
		probe:        newDiagnosticProbe(),
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
		if _, ok := wanted[adapter.ID]; ok && adapter.Operational && adapter.Address != "" {
			selected = append(selected, adapter)
		}
	}
	if len(selected) == 0 {
		return s.failRun(errors.New("请至少选择一张拥有有效 IPv4 的活动网卡"))
	}

	started := time.Now()
	runID := fmt.Sprintf("diag-%x", started.UnixNano())
	s.mu.Lock()
	s.latest = DiagnosticSnapshot{
		State: "running", RunID: runID, TargetIP: diagnosticTargetIPv4,
		StartedAt: started, Total: len(selected), Results: []DiagnosticResult{},
	}
	s.mu.Unlock()

	names := make([]string, 0, len(selected))
	for _, adapter := range selected {
		names = append(names, adapter.Name)
	}
	logOwned := s.logs.Start("adapter-diagnostic", names, map[string]any{
		"target_ip":     diagnosticTargetIPv4,
		"adapter_count": len(selected),
	})
	s.logs.RecordEvent("adapter_diagnostic", "started", map[string]any{
		"run_id": runID, "adapters": names, "target_ip": diagnosticTargetIPv4,
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
		s.mu.Lock()
		if s.latest.RunID == runID {
			s.latest.Results = append(s.latest.Results, result)
			s.latest.Completed = len(s.latest.Results)
		}
		s.mu.Unlock()
		s.logs.RecordEvent("adapter_diagnostic", "result", map[string]any{
			"adapter":   adapter.Name,
			"source_ip": adapter.Address,
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
	icmp := s.probe.ICMP(ctx, adapter.Address, diagnosticTargetIPv4)
	tcpOK, tcpDetail := s.probe.BoundTCP(ctx, adapter)
	result := DiagnosticResult{
		AdapterID: adapter.ID, Name: adapter.Name, Address: adapter.Address,
		Status: icmp.Status, LossRate: icmp.LossRate,
		AvgLatencyMS: icmp.AvgLatencyMS, JitterMS: icmp.JitterMS,
		Sent: icmp.Sent, Received: icmp.Received, TargetIP: diagnosticTargetIPv4,
		Note: icmp.Note, BoundTCPOK: tcpOK, BoundTCPDetail: tcpDetail,
		CompletedAt: time.Now(),
	}
	// v2.2.0 treats selected-interface TCP as the authoritative multihomed
	// result. ICMP remains visible as latency/jitter evidence.
	if tcpOK {
		result.Status = "available"
		result.LossRate = 0
	} else {
		result.Status = "unavailable"
		result.LossRate = 100
	}
	result.Checks = buildDiagnosticChecks(adapter, result)
	return result
}

func buildDiagnosticChecks(adapter AdapterView, result DiagnosticResult) []DiagnosticCheck {
	sourceLevel := "pass"
	sourceDetail := adapter.Address
	if !result.BoundTCPOK {
		sourceLevel = "fail"
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
	metricLevel, metricDetail := "pass", fmt.Sprintf("%d", adapter.Metric)
	if adapter.Metric < 0 {
		metricLevel, metricDetail = "warn", ""
	}
	mode := "fixed"
	if adapter.AutoMetric {
		mode = "auto"
	}
	return []DiagnosticCheck{
		{Key: "source_binding", Level: sourceLevel, Detail: sourceDetail},
		{Key: "gateway", Level: levelForValue(adapter.Gateway), Detail: adapter.Gateway},
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
