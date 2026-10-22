package services

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Credentials are passed on stdin and stored only in user-encrypted preferences,
// never on the command line, in telemetry, or in general HypoMux settings.
type HotspotConfig struct {
	AdapterID string `json:"adapter_id,omitempty"`
	SSID      string `json:"ssid"`
	Password  string `json:"password"`
	Band      string `json:"band"`
}

type HotspotStatus struct {
	HotspotAdapterID      string          `json:"hotspot_adapter_id,omitempty"`
	HotspotAdapter        string          `json:"hotspot_adapter,omitempty"`
	SessionID             string          `json:"session_id,omitempty"`
	ConfiguredBand        string          `json:"configured_band,omitempty"`
	BandFallback          string          `json:"band_fallback,omitempty"`
	TransmitLinkMbps      float64         `json:"transmit_link_mbps,omitempty"`
	ReceiveLinkMbps       float64         `json:"receive_link_mbps,omitempty"`
	Devices               []HotspotDevice `json:"devices,omitempty"`
	DevicesAvailable      bool            `json:"devices_available"`
	UpdatedAt             string          `json:"updated_at,omitempty"`
	State                 string          `json:"state"`
	SSID                  string          `json:"ssid"`
	Band                  string          `json:"band"`
	Clients               int             `json:"clients"`
	SharedAdapter         string          `json:"shared_adapter"`
	SharingVerified       bool            `json:"sharing_verified"`
	CleanupComplete       bool            `json:"cleanup_complete"`
	HotspotOffConfirmed   *bool           `json:"hotspot_off_confirmed,omitempty"`
	ConfigurationRestored *bool           `json:"configuration_restored,omitempty"`
	CleanupError          string          `json:"cleanup_error,omitempty"`
	Ready                 bool            `json:"ready"`
	Message               string          `json:"message,omitempty"`
	Diagnostics           string          `json:"diagnostics,omitempty"`
	GatewayAddress        string          `json:"gateway_address,omitempty"`
	// IdleTimeout reports Windows' no-client power saving handling;
	// StopReason "windows_off" marks a hotspot Windows turned off by itself.
	IdleTimeout string `json:"idle_timeout,omitempty"`
	StopReason  string `json:"stop_reason,omitempty"`
}

var errHotspotStartCanceled = errors.New("已取消开启热点")

// hotspotStart lets a stop request cancel a startup that holds lifecycleGate.
type hotspotStart struct{ cancel context.CancelFunc }

type HotspotDevice struct {
	MAC   string   `json:"mac"`
	Hosts []string `json:"hosts"`
}

type HotspotAdapter struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Status      string `json:"status"`
	Upstream    bool   `json:"upstream"`
}

var hotspotAdapterGUID = regexp.MustCompile(`^(?:\{[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\}|[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})$`)

// This list includes disconnected physical radios, allowing a dedicated AP
// without first connecting it as an upstream. No network scan is performed.
func (s *EngineService) HotspotAdapters() ([]HotspotAdapter, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	adapters, err := listHotspotAdapters(ctx)
	if err != nil {
		return nil, err
	}
	if s.settings != nil {
		selected := s.settings.Get().SelectedAdapterIDs
		for i := range adapters {
			for _, name := range selected {
				if strings.EqualFold(name, adapters[i].Name) {
					adapters[i].Upstream = true
					break
				}
			}
		}
	}
	return adapters, nil
}

func validateHotspotConfig(config HotspotConfig) error {
	if config.AdapterID != "" && !hotspotAdapterGUID.MatchString(config.AdapterID) {
		return errors.New("热点发射网卡标识无效，请重新选择网卡")
	}
	if !utf8.ValidString(config.SSID) || strings.TrimSpace(config.SSID) == "" || len(config.SSID) > 32 || strings.ContainsAny(config.SSID, "\x00\r\n") {
		return errors.New("热点名称不能为空，且不能超过 32 个 UTF-8 字节或包含换行")
	}
	if len(config.Password) < 8 || len(config.Password) > 63 {
		return errors.New("热点密码需要 8–63 个 ASCII 字符")
	}
	for _, c := range config.Password {
		if c < 32 || c > 126 {
			return errors.New("热点密码仅支持可打印 ASCII 字符")
		}
	}
	if config.Band != "auto" && config.Band != "2.4" && config.Band != "5" {
		return errors.New("不支持的热点频段")
	}
	return nil
}

// The worker owns a single Windows tethering session, including its original
// configuration. Closing stdin asks it to stop and restore that configuration;
// desktop crashes also close the pipe, without a persisted session lease.
type hotspotSession struct {
	id            string
	config        HotspotConfig
	onStatus      func(HotspotStatus)
	mu            sync.Mutex
	status        HotspotStatus
	input         io.WriteCloser
	done          chan struct{}
	stopOnce      sync.Once
	stopRequested bool
}

func (h *hotspotSession) snapshot() HotspotStatus {
	h.mu.Lock()
	defer h.mu.Unlock()
	status := h.status
	status.SessionID = h.id
	return status
}

// Keep credentials out of telemetry and AI tools. The desktop must name the
// observed session so a delayed request cannot read a different hotspot's key.
func (s *EngineService) HotspotSessionConfig(sessionID string) (HotspotConfig, error) {
	s.mu.Lock()
	h := s.hotspot
	s.mu.Unlock()
	if h == nil || sessionID == "" || h.id != sessionID || h.snapshot().State != "running" {
		return HotspotConfig{}, errors.New("热点会话已变化，请刷新后重试")
	}
	return h.config, nil
}

func (h *hotspotSession) requestStop() {
	h.stopOnce.Do(func() {
		h.mu.Lock()
		h.stopRequested = true
		if h.status.State == "running" || h.status.State == "starting" {
			h.status.State = "stopping"
			h.status.SharingVerified = false
		}
		h.mu.Unlock()
		_ = h.input.Close()
	})
}

func (h *hotspotSession) stop(ctx context.Context) error {
	h.requestStop()
	select {
	case <-h.done:
		if status := h.snapshot(); !status.CleanupComplete {
			return errors.New(status.Message)
		}
		return nil
	case <-ctx.Done():
		return errors.New("热点清理尚未完成；请在 Windows 设置中检查移动热点状态")
	}
}

type hotspotSharingConnection struct {
	GUID string `json:"guid"`
	Name string `json:"name"`
	Role int    `json:"role"`
}

type hotspotSharingReply struct {
	Connections []hotspotSharingConnection `json:"connections"`
	Error       string                     `json:"error,omitempty"`
}

func launchHotspot(ctx context.Context, command *exec.Cmd, config HotspotConfig, inspectors ...func() hotspotSharingReply) (*hotspotSession, error) {
	return launchHotspotObserved(ctx, command, config, nil, inspectors...)
}

func launchHotspotObserved(ctx context.Context, command *exec.Cmd, config HotspotConfig, onCreated func(*hotspotSession), inspectors ...func() hotspotSharingReply) (*hotspotSession, error) {
	input, err := command.StdinPipe()
	if err != nil {
		return nil, err
	}
	output, err := command.StdoutPipe()
	if err != nil {
		_ = input.Close()
		return nil, err
	}
	// The script emits sanitized JSON failures. Discard raw PowerShell error
	// records, which may include configuration objects and passwords.
	command.Stderr = io.Discard
	configureBackgroundCommand(command)
	if err := command.Start(); err != nil {
		_ = input.Close()
		_ = output.Close()
		return nil, err
	}
	h := &hotspotSession{id: rand.Text(), config: config, input: input, done: make(chan struct{}), status: HotspotStatus{State: "starting", SSID: config.SSID, Band: config.Band, HotspotAdapterID: config.AdapterID}}
	if onCreated != nil {
		onCreated(h)
	}
	ready := make(chan struct{})
	var inputMu sync.Mutex
	go func() {
		defer close(h.done)
		defer input.Close()
		signalled := false
		scanner := bufio.NewScanner(output)
		for scanner.Scan() {
			var request struct {
				Kind string `json:"kind"`
			}
			if json.Unmarshal(scanner.Bytes(), &request) == nil && request.Kind == "inspect_sharing" {
				reply := hotspotSharingReply{Connections: []hotspotSharingConnection{}, Error: "管理员共享检查不可用，请更新 Core"}
				if len(inspectors) > 0 {
					reply = inspectors[0]()
				}
				inputMu.Lock()
				err := json.NewEncoder(input).Encode(reply)
				inputMu.Unlock()
				if err != nil {
					_ = input.Close()
				}
				continue
			}
			var status HotspotStatus
			if json.Unmarshal(scanner.Bytes(), &status) != nil {
				continue
			}
			h.mu.Lock()
			if h.stopRequested && status.CleanupComplete {
				status.State = "stopped"
				status.Message = ""
				status.SharingVerified = false
			} else if h.stopRequested && (status.State == "running" || status.State == "starting") {
				// A queued worker update must not revert the UI to running after
				// the user has requested shutdown.
				status.State = "stopping"
				status.SharingVerified = false
			}
			h.status = status
			h.mu.Unlock()
			if h.onStatus != nil {
				h.onStatus(status)
			}
			// A working AP is distinct from verified aggregation egress. Windows
			// can omit legacy ICS entries; keep that state explicitly unverified.
			if status.State == "running" && (status.SharingVerified || status.GatewayAddress != "") && !signalled {
				close(ready)
				signalled = true
			}
		}
		waitErr := command.Wait()
		h.mu.Lock()
		if h.status.State != "stopped" && h.status.State != "failed" {
			h.status.State = "failed"
			h.status.SharingVerified = false
			h.status.Message = "热点控制进程意外退出；请检查 Windows 移动热点状态"
		} else if waitErr != nil && h.status.State == "stopped" {
			h.status.State = "failed"
			h.status.Message = "热点控制进程未正常结束"
		}
		finalStatus := h.status
		h.mu.Unlock()
		if h.onStatus != nil {
			h.onStatus(finalStatus)
		}
	}()
	inputMu.Lock()
	encodeErr := json.NewEncoder(input).Encode(config)
	inputMu.Unlock()
	if err := encodeErr; err != nil {
		_ = input.Close()
		return h, fmt.Errorf("发送热点配置失败：%w", err)
	}
	select {
	case <-ready:
		return h, nil
	case <-h.done:
		message := h.snapshot().Message
		if message == "" {
			message = "热点在完成启动前停止"
		}
		return h, errors.New(message)
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.Canceled) {
			// A user stop: the worker rolls back and later statuses read stopping.
			h.requestStop()
			return h, errHotspotStartCanceled
		}
		_ = input.Close()
		return h, errors.New("启动热点超时，正在撤销共享；请稍后检查状态")
	}
}

func (s *EngineService) HotspotStatus() HotspotStatus {
	s.mu.Lock()
	h := s.hotspot
	ready := !s.closing && s.tunAggregationEndpoint != "" && s.transitionPhase == ""
	s.mu.Unlock()
	status := HotspotStatus{State: "stopped"}
	if h != nil {
		status = h.snapshot()
	}
	status.Ready = ready && hotspotSupported()
	if !hotspotSupported() {
		status.Message = "聚合热点目前仅支持 Windows 10/11"
	}
	return status
}

func (s *EngineService) StartHotspot(config HotspotConfig) (HotspotStatus, error) {
	if err := validateHotspotConfig(config); err != nil {
		return s.HotspotStatus(), err
	}
	// Allow profile discovery, configuration, startup, private-network readiness
	// and bounded broker inspection retries to finish within their deadlines.
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	start := &hotspotStart{cancel: cancel}
	s.mu.Lock()
	s.hotspotStart = start
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		if s.hotspotStart == start {
			s.hotspotStart = nil
		}
		s.mu.Unlock()
	}()
	status, err := s.startWithLifecycle(ctx, config)
	if err != nil && errors.Is(ctx.Err(), context.Canceled) {
		err = errHotspotStartCanceled
	}
	return status, err
}

func (s *EngineService) startWithLifecycle(ctx context.Context, config HotspotConfig) (HotspotStatus, error) {
	if err := s.acquireLifecycle(ctx); err != nil {
		return s.HotspotStatus(), err
	}
	defer s.releaseLifecycle()
	return s.startHotspotLocked(ctx, config)
}

// cancelHotspotStart must run before waiting for lifecycleGate: an in-flight
// StartHotspot holds that gate for up to its full startup budget.
func (s *EngineService) cancelHotspotStart() {
	s.mu.Lock()
	start := s.hotspotStart
	s.mu.Unlock()
	if start != nil {
		start.cancel()
	}
}

func (s *EngineService) startHotspotLocked(ctx context.Context, config HotspotConfig) (HotspotStatus, error) {
	s.mu.Lock()
	closing, endpoint, previous := s.closing, s.tunAggregationEndpoint, s.hotspot
	s.mu.Unlock()
	if closing {
		return s.HotspotStatus(), errors.New("HypoMux 正在退出")
	}
	if previous != nil {
		select {
		case <-previous.done:
			// A terminated worker cannot retry cleanup. Re-run the new worker's
			// live Off/ICS checks instead of permanently latching a stale failure.
			// Those checks still refuse to take over any active sharing session.
		default:
			return s.HotspotStatus(), errors.New("热点仍在运行或清理中，请先关闭后重试")
		}
	}
	if endpoint == "" {
		return s.HotspotStatus(), errors.New("请先在首页以 TUN 模式启动聚合，再开启热点")
	}
	var tun tunStatus
	if err := s.client.Request(ctx, "tun.status", nil, &tun); err != nil {
		return s.HotspotStatus(), err
	}
	if tun.State != "running" {
		return s.HotspotStatus(), errors.New("TUN 尚未运行，无法共享聚合出口")
	}
	if !slices.Contains(s.client.Hello().Capabilities, "hotspot.inspect") {
		return s.HotspotStatus(), errors.New("当前 Core 不支持热点共享检查，请安装新版客户端并重启 Core 服务")
	}
	command, err := hotspotCommand()
	if err != nil {
		return s.HotspotStatus(), err
	}
	if err := saveHotspotPreferences(config); err != nil {
		return s.HotspotStatus(), err
	}
	_, err = launchHotspotObserved(ctx, command, config, func(h *hotspotSession) {
		h.onStatus = newHotspotStatusLogger(s.logs)
		s.mu.Lock()
		s.hotspot = h
		s.mu.Unlock()
	}, func() hotspotSharingReply {
		inspectionCtx, inspectionCancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer inspectionCancel()
		reply := hotspotSharingReply{Connections: []hotspotSharingConnection{}}
		if err := s.client.Request(inspectionCtx, "hotspot.inspect", nil, &reply); err != nil {
			reply.Error = err.Error()
		}
		return reply
	})
	return s.HotspotStatus(), err
}

// Worker callbacks are serialized. Keep the log useful without recording SSIDs,
// passwords, client identities or repeatedly emitting the same polling result.
func newHotspotStatusLogger(logs *SupportLogStore) func(HotspotStatus) {
	var previous string
	return func(status HotspotStatus) {
		if logs == nil {
			return
		}
		fields := hotspotPerformanceFields(status)
		fields["cleanup_complete"] = status.CleanupComplete
		encoded, _ := json.Marshal(fields)
		if string(encoded) == previous {
			return
		}
		previous = string(encoded)
		logs.RecordEvent("hotspot", "status_changed", fields)
	}
}

func hotspotPerformanceFields(status HotspotStatus) map[string]any {
	return map[string]any{
		"state": status.State, "requested_band": status.Band,
		"configured_band": status.ConfiguredBand, "band_fallback": status.BandFallback,
		"clients": status.Clients, "sharing_verified": status.SharingVerified,
		"transmit_link_mbps": status.TransmitLinkMbps, "receive_link_mbps": status.ReceiveLinkMbps,
		"idle_timeout": status.IdleTimeout, "stop_reason": status.StopReason,
	}
}

func (s *EngineService) stopHotspot(ctx context.Context) error {
	s.mu.Lock()
	h := s.hotspot
	s.mu.Unlock()
	if h == nil {
		return nil
	}
	return h.stop(ctx)
}

func (s *EngineService) StopHotspot() (HotspotStatus, error) {
	s.cancelHotspotStart()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := s.acquireLifecycle(ctx); err != nil {
		return s.HotspotStatus(), err
	}
	defer s.releaseLifecycle()
	s.mu.Lock()
	if s.runtimeBindingRecovery != nil {
		s.runtimeBindingRecovery.hotspot = nil
	}
	s.mu.Unlock()
	err := s.stopHotspot(ctx)
	if err == nil {
		s.mu.Lock()
		s.hotspot = nil
		s.mu.Unlock()
	}
	return s.HotspotStatus(), err
}
