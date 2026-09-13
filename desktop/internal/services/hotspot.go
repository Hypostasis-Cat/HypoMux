package services

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Credentials are passed on stdin and stored only in user-encrypted preferences,
// never on the command line, in telemetry, or in general HypoMux settings.
type HotspotConfig struct {
	SSID     string `json:"ssid"`
	Password string `json:"password"`
	Band     string `json:"band"`
}

type HotspotStatus struct {
	State           string `json:"state"`
	SSID            string `json:"ssid"`
	Band            string `json:"band"`
	Clients         int    `json:"clients"`
	SharedAdapter   string `json:"shared_adapter"`
	SharingVerified bool   `json:"sharing_verified"`
	CleanupComplete bool   `json:"cleanup_complete"`
	Ready           bool   `json:"ready"`
	Message         string `json:"message,omitempty"`
	Diagnostics     string `json:"diagnostics,omitempty"`
	GatewayAddress  string `json:"gateway_address,omitempty"`
}

func validateHotspotConfig(config HotspotConfig) error {
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
	mu       sync.Mutex
	status   HotspotStatus
	input    io.WriteCloser
	done     chan struct{}
	stopOnce sync.Once
}

func (h *hotspotSession) snapshot() HotspotStatus {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.status
}

func (h *hotspotSession) stop(ctx context.Context) error {
	h.stopOnce.Do(func() { _ = h.input.Close() })
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
	h := &hotspotSession{input: input, done: make(chan struct{}), status: HotspotStatus{State: "starting", SSID: config.SSID, Band: config.Band}}
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
			h.status = status
			h.mu.Unlock()
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
		h.mu.Unlock()
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
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := s.acquireLifecycle(ctx); err != nil {
		return s.HotspotStatus(), err
	}
	defer s.releaseLifecycle()
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
	h, err := launchHotspot(ctx, command, config, func() hotspotSharingReply {
		inspectionCtx, inspectionCancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer inspectionCancel()
		reply := hotspotSharingReply{Connections: []hotspotSharingConnection{}}
		if err := s.client.Request(inspectionCtx, "hotspot.inspect", nil, &reply); err != nil {
			reply.Error = err.Error()
		}
		return reply
	})
	if h != nil {
		s.mu.Lock()
		s.hotspot = h
		s.mu.Unlock()
	}
	return s.HotspotStatus(), err
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
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := s.acquireLifecycle(ctx); err != nil {
		return s.HotspotStatus(), err
	}
	defer s.releaseLifecycle()
	err := s.stopHotspot(ctx)
	return s.HotspotStatus(), err
}
