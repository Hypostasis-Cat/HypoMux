package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"time"

	api "github.com/Hypostasis-Cat/HypoMux/engine/internal/api/v1"
	"github.com/Hypostasis-Cat/HypoMux/engine/internal/diagnostic"
	"github.com/Hypostasis-Cat/HypoMux/engine/internal/dns"
	"github.com/Hypostasis-Cat/HypoMux/engine/internal/platform"
	"github.com/Hypostasis-Cat/HypoMux/engine/internal/protocol"
	"github.com/Hypostasis-Cat/HypoMux/engine/internal/proxy"
	engineRuntime "github.com/Hypostasis-Cat/HypoMux/engine/internal/runtime"
	"github.com/Hypostasis-Cat/HypoMux/engine/internal/tun"
	"github.com/Hypostasis-Cat/HypoMux/engine/internal/wfp"
)

type Metadata struct {
	Name                string
	Version             string
	Commit              string
	TunExecutable       string
	TunExecutableSHA256 string
}

type tunController interface {
	Activate(context.Context, tun.Config) (tun.Status, error)
	Stop(context.Context) (tun.Status, error)
	Status() tun.Status
	SetHandlers(func(string), func(tun.Status))
}

type Server struct {
	input               io.Reader
	encoder             *json.Encoder
	metadata            Metadata
	identity            platform.Identity
	runtime             *engineRuntime.Runtime
	proxy               *proxy.Server
	tun                 tunController
	adapters            []wfp.Adapter
	dnsExemption        wfp.DNSExemption
	mode                string
	activeTunGeneration uint64
	startedAt           time.Time
	started             time.Time
	writeMu             sync.Mutex
	lifecycleMu         sync.Mutex
	eventSeq            uint64
}

func New(input io.Reader, output io.Writer, metadata Metadata) *Server {
	now := time.Now()
	server := &Server{
		input:     input,
		encoder:   json.NewEncoder(output),
		metadata:  metadata,
		identity:  platform.CurrentIdentity(),
		runtime:   engineRuntime.New(time.Now),
		startedAt: now.UTC(),
		started:   now,
		tun:       tun.NewSupervisor(),
	}
	server.tun.SetHandlers(server.handleTunLog, server.handleTunUnexpectedExit)
	return server
}

// Run serves protocol-v1 requests as newline-delimited JSON. Standard output is
// reserved for protocol messages; callers must send human-readable logs to
// standard error.
func (s *Server) Run(ctx context.Context) error {
	defer s.stopProxyForHostExit()
	scanner := bufio.NewScanner(s.input)
	scanner.Buffer(make([]byte, 64*1024), protocol.MaxMessageBytes)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}

		stateBefore := s.runtime.Snapshot()
		tunBefore := s.tun.Status()
		response, shutdown := s.handle(ctx, line)
		if err := s.writeMessage(response); err != nil {
			return fmt.Errorf("write response: %w", err)
		}
		stateAfter := s.runtime.Snapshot()
		if stateAfter.Sequence != stateBefore.Sequence {
			if err := s.emitEvent(api.EventEngineStateChanged, stateAfter); err != nil {
				return fmt.Errorf("write state event: %w", err)
			}
		}
		tunAfter := s.tun.Status()
		if tunAfter.State != tunBefore.State || tunAfter.PID != tunBefore.PID {
			if err := s.emitEvent(api.EventTunStateChanged, tunAfter); err != nil {
				return fmt.Errorf("write TUN state event: %w", err)
			}
		}
		if shutdown {
			if err := s.emitEvent(api.EventHostExiting, api.HostExitingData{
				Reason: "requested",
			}); err != nil {
				return fmt.Errorf("write shutdown event: %w", err)
			}
			return nil
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read request: %w", err)
	}
	return nil
}

func (s *Server) handle(ctx context.Context, line []byte) (protocol.Response, bool) {
	var request protocol.Request
	decoder := json.NewDecoder(bytes.NewReader(line))
	if err := decoder.Decode(&request); err != nil {
		return protocol.Failure("", "invalid_json", "request is not valid JSON", nil), false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return protocol.Failure(request.ID, "invalid_json", "request contains trailing JSON", nil), false
	}
	if request.Protocol != protocol.Version {
		return protocol.Failure(
			request.ID,
			"unsupported_protocol",
			"unsupported protocol version",
			map[string]any{"supported": []int{protocol.Version}},
		), false
	}
	if strings.TrimSpace(request.ID) == "" {
		return protocol.Failure("", "invalid_request", "request id is required", nil), false
	}
	if strings.TrimSpace(request.Method) == "" {
		return protocol.Failure(request.ID, "invalid_request", "request method is required", nil), false
	}

	switch request.Method {
	case api.MethodEngineHello:
		return protocol.Result(request.ID, s.hello()), false
	case api.MethodEngineStatus:
		return protocol.Result(request.ID, s.status()), false
	case api.MethodEngineStart:
		return s.startProxy(request), false
	case api.MethodEngineStop:
		return s.stopProxy(request.ID), false
	case api.MethodEngineTelemetry:
		return s.proxyTelemetry(request), false
	case api.MethodTunActivate:
		return s.activateTun(ctx, request), false
	case api.MethodTunStatus:
		return protocol.Result(request.ID, s.tun.Status()), false
	case api.MethodTunDeactivate:
		return s.deactivateTun(ctx, request.ID), false
	case api.MethodDNSResolve:
		return s.resolveDNS(ctx, request), false
	case api.MethodDNSStatus:
		return s.dnsStatus(request.ID), false
	case api.MethodHealthCheck:
		return protocol.Result(request.ID, api.HealthResult{
			OK:           true,
			State:        s.runtime.Snapshot().State,
			HostUptimeMS: s.uptimeMilliseconds(),
		}), false
	case api.MethodDiagnosticRun:
		var params api.DiagnosticRunParams
		if len(request.Params) > 0 {
			if err := json.Unmarshal(request.Params, &params); err != nil {
				return protocol.Failure(request.ID, "invalid_params", "diagnostic params are not valid JSON", nil), false
			}
		}
		result := diagnostic.Run(ctx, params.Config())
		return protocol.Result(request.ID, result), false
	case api.MethodWFPInspect:
		var params api.WFPInspectParams
		if len(request.Params) > 0 {
			if err := json.Unmarshal(request.Params, &params); err != nil {
				return protocol.Failure(request.ID, "invalid_params", "WFP params are not valid JSON", nil), false
			}
		}
		result, err := platform.InspectWFP(params.Repair)
		if err != nil {
			code := "wfp_unavailable"
			if params.Repair && !s.identity.Elevated {
				code = "elevation_required"
			}
			return protocol.Failure(request.ID, code, err.Error(), map[string]any{
				"status": result,
			}), false
		}
		return protocol.Result(request.ID, result), false
	case api.MethodHostShutdown:
		_ = s.stopProxy(request.ID)
		return protocol.Result(request.ID, api.ShutdownResult{Accepted: true}), true
	default:
		return protocol.Failure(
			request.ID,
			"method_not_found",
			"unknown method",
			map[string]any{"method": request.Method},
		), false
	}
}

func (s *Server) hello() api.HelloResult {
	return api.NewHelloResult(
		s.metadata.Name,
		s.metadata.Version,
		s.metadata.Commit,
		s.identity.ProcessID,
		s.identity.Elevated,
		s.startedAt,
	)
}

func (s *Server) status() api.StatusResult {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	result := api.StatusResult{
		Engine:       s.runtime.Snapshot(),
		HostUptimeMS: s.uptimeMilliseconds(),
	}
	if s.proxy != nil {
		result.Proxy = &api.ProxyStatus{
			Mode:      s.mode,
			Running:   s.proxy.Running(),
			Endpoints: s.proxy.Endpoints(),
			Telemetry: s.proxy.Snapshot(false),
		}
	}
	tunStatus := s.tun.Status()
	if tunStatus.State != tun.StateStopped {
		result.Tun = &tunStatus
	}
	return result
}

func (s *Server) startProxy(request protocol.Request) protocol.Response {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	state := s.runtime.Snapshot().State
	if state != engineRuntime.StateStopped && state != engineRuntime.StateFailed {
		return protocol.Failure(
			request.ID,
			"invalid_state",
			"engine must be stopped before it can start",
			map[string]any{"state": state},
		)
	}
	var params api.EngineStartParams
	if err := json.Unmarshal(request.Params, &params); err != nil {
		return protocol.Failure(request.ID, "invalid_params", "engine start params are not valid JSON", nil)
	}
	mode := params.Mode
	if mode == "" {
		mode = "proxy"
	}
	if mode != "proxy" && mode != "tun_tcp_pool" {
		return protocol.Failure(
			request.ID,
			"unsupported_mode",
			"unsupported engine mode",
			map[string]any{"mode": params.Mode},
		)
	}
	if mode == "proxy" && len(params.Channels) != 0 {
		return protocol.Failure(
			request.ID,
			"invalid_params",
			"proxy mode cannot configure TUN channels",
			nil,
		)
	}
	if mode == "tun_tcp_pool" && len(params.Channels) == 0 {
		return protocol.Failure(
			request.ID,
			"invalid_params",
			"tun_tcp_pool mode requires channels",
			nil,
		)
	}
	if _, err := s.runtime.Transition(engineRuntime.StateStarting, mode+" start requested"); err != nil {
		return protocol.Failure(request.ID, "invalid_state", err.Error(), nil)
	}
	// A new proxy-pool transaction must never inherit ownership from a TUN
	// process whose delayed cleanup callback is still in flight.
	s.activeTunGeneration = 0
	proxyServer, err := proxy.New(params.ProxyConfig())
	if err == nil {
		proxyServer.SetDNSFallbackHandler(s.handleDNSFallback)
		var endpoints proxy.Endpoints
		endpoints, err = proxyServer.Start()
		if err == nil {
			s.proxy = proxyServer
			s.mode = mode
			s.adapters = make([]wfp.Adapter, 0, len(params.Adapters))
			for _, adapter := range params.Adapters {
				if adapter.IfIndex <= 0 {
					continue
				}
				s.adapters = append(s.adapters, wfp.Adapter{
					Name:     adapter.Name,
					SourceIP: adapter.SourceIP,
					IfIndex:  uint32(adapter.IfIndex),
				})
			}
			_, _ = s.runtime.Transition(engineRuntime.StateRunning, mode+" listeners ready")
			return protocol.Result(request.ID, api.EngineStartResult{
				State:     s.runtime.Snapshot(),
				Mode:      mode,
				Endpoints: endpoints,
			})
		}
	}
	_, _ = s.runtime.Transition(engineRuntime.StateFailed, "proxy start failed")
	return protocol.Failure(
		request.ID,
		"start_failed",
		"could not start proxy engine",
		map[string]any{"message": err.Error()},
	)
}

func (s *Server) stopProxy(requestID string) protocol.Response {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	return s.stopProxyLocked(requestID)
}

func (s *Server) stopProxyLocked(requestID string) protocol.Response {
	state := s.runtime.Snapshot().State
	if state == engineRuntime.StateStopped {
		s.activeTunGeneration = 0
		return protocol.Result(requestID, api.EngineStopResult{
			Accepted: false,
			State:    s.runtime.Snapshot(),
		})
	}
	if state == engineRuntime.StateFailed {
		stopCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		_, tunErr := s.tun.Stop(stopCtx)
		cancel()
		wfpErr := s.closeDNSExemption()
		var proxyErr error
		if s.proxy != nil {
			proxyCtx, proxyCancel := context.WithTimeout(
				context.Background(),
				5*time.Second,
			)
			proxyErr = s.proxy.Stop(proxyCtx)
			proxyCancel()
		}
		s.proxy = nil
		s.mode = ""
		s.adapters = nil
		s.activeTunGeneration = 0
		_, _ = s.runtime.Transition(engineRuntime.StateStopped, "failed proxy cleared")
		if err := errors.Join(tunErr, wfpErr, proxyErr); err != nil {
			return protocol.Failure(
				requestID,
				"stop_failed",
				"could not clear failed engine",
				map[string]any{"message": err.Error()},
			)
		}
		return protocol.Result(requestID, api.EngineStopResult{
			Accepted: true,
			State:    s.runtime.Snapshot(),
		})
	}
	if state != engineRuntime.StateRunning && state != engineRuntime.StateDegraded {
		return protocol.Failure(
			requestID,
			"invalid_state",
			"engine cannot stop from its current state",
			map[string]any{"state": state},
		)
	}
	_, _ = s.runtime.Transition(engineRuntime.StateStopping, "proxy stop requested")
	stopCtx, stopCancel := context.WithTimeout(
		context.Background(),
		20*time.Second,
	)
	_, tunErr := s.tun.Stop(stopCtx)
	stopCancel()
	wfpErr := s.closeDNSExemption()
	var proxyErr error
	if s.proxy != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		proxyErr = s.proxy.Stop(ctx)
		cancel()
	}
	if err := errors.Join(tunErr, wfpErr, proxyErr); err != nil {
		_, _ = s.runtime.Transition(engineRuntime.StateFailed, "engine stop failed")
		return protocol.Failure(
			requestID,
			"stop_failed",
			"could not stop engine transaction",
			map[string]any{"message": err.Error()},
		)
	}
	s.proxy = nil
	s.mode = ""
	s.adapters = nil
	s.activeTunGeneration = 0
	_, _ = s.runtime.Transition(engineRuntime.StateStopped, "proxy stopped")
	return protocol.Result(requestID, api.EngineStopResult{
		Accepted: true,
		State:    s.runtime.Snapshot(),
	})
}

func (s *Server) proxyTelemetry(request protocol.Request) protocol.Response {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.proxy == nil || !s.proxy.Running() {
		return protocol.Failure(request.ID, "invalid_state", "proxy engine is not running", nil)
	}
	var params api.EngineTelemetryParams
	if len(request.Params) > 0 {
		if err := json.Unmarshal(request.Params, &params); err != nil {
			return protocol.Failure(request.ID, "invalid_params", "telemetry params are not valid JSON", nil)
		}
	}
	return protocol.Result(request.ID, s.proxy.Snapshot(params.IncludeConnections))
}

func (s *Server) resolveDNS(ctx context.Context, request protocol.Request) protocol.Response {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.proxy == nil || !s.proxy.Running() {
		return protocol.Failure(request.ID, "invalid_state", "proxy engine is not running", nil)
	}
	var params api.DNSResolveParams
	if err := json.Unmarshal(request.Params, &params); err != nil {
		return protocol.Failure(request.ID, "invalid_params", "DNS params are not valid JSON", nil)
	}
	result, err := s.proxy.ResolveDNS(ctx, params.Domain, params.Adapter, params.RecordType)
	if err != nil {
		return protocol.Failure(
			request.ID,
			"dns_failed",
			"DNS resolution failed",
			map[string]any{"message": err.Error()},
		)
	}
	return protocol.Result(request.ID, result)
}

func (s *Server) dnsStatus(requestID string) protocol.Response {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.proxy == nil {
		return protocol.Failure(requestID, "invalid_state", "proxy engine is not running", nil)
	}
	status, ok := s.proxy.DNSStatus()
	if !ok {
		return protocol.Failure(requestID, "invalid_state", "proxy engine is not running", nil)
	}
	return protocol.Result(requestID, status)
}

func (s *Server) handleDNSFallback(event dns.FallbackEvent) {
	_ = s.emitEvent(api.EventDNSFallbackRequired, api.DNSFallbackRequiredData{
		Adapter: event.Adapter,
		Policy:  event.Policy,
		Reason:  event.Reason,
	})
}

func (s *Server) activateTun(
	ctx context.Context,
	request protocol.Request,
) protocol.Response {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.runtime.Snapshot().State != engineRuntime.StateRunning ||
		s.proxy == nil ||
		s.mode != "tun_tcp_pool" {
		return protocol.Failure(
			request.ID,
			"invalid_state",
			"TUN activation requires a running tun_tcp_pool",
			nil,
		)
	}
	var params api.TunActivateParams
	if err := json.Unmarshal(request.Params, &params); err != nil {
		return protocol.Failure(
			request.ID,
			"invalid_params",
			"TUN activation params are not valid JSON",
			nil,
		)
	}
	primaryConfig, err := s.authorizeTunConfig(params.Config())
	if err != nil {
		return protocol.Failure(
			request.ID,
			"security_policy_rejected",
			"TUN executable is not authorized by the Core security policy",
			map[string]any{"message": err.Error()},
		)
	}
	var fallbackConfig tun.Config
	if strings.TrimSpace(params.IPv4FallbackConfigPath) != "" {
		fallbackConfig, err = s.authorizeTunConfig(params.IPv4FallbackConfig())
		if err != nil {
			return protocol.Failure(
				request.ID,
				"security_policy_rejected",
				"TUN fallback executable is not authorized by the Core security policy",
				map[string]any{"message": err.Error()},
			)
		}
	}
	if params.StrictRoute {
		if !s.identity.Elevated {
			return protocol.Failure(
				request.ID,
				"elevation_required",
				"strict-route WFP DNS exemption requires an elevated Core",
				nil,
			)
		}
		exemption, err := wfp.OpenDNSExemption("", s.adapters)
		if err != nil {
			return protocol.Failure(
				request.ID,
				"wfp_unavailable",
				"could not install the dynamic WFP DNS exemption",
				map[string]any{"message": err.Error()},
			)
		}
		if closeErr := s.closeDNSExemption(); closeErr != nil {
			_ = exemption.Close()
			return protocol.Failure(
				request.ID,
				"wfp_unavailable",
				"could not replace the previous dynamic WFP DNS exemption",
				map[string]any{"message": closeErr.Error()},
			)
		}
		s.dnsExemption = exemption
	}
	status, err := s.tun.Activate(ctx, primaryConfig)
	recoveredStaleAdapter := false
	if err != nil && isStaleTunAdapterError(err) {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 20*time.Second)
		_, cleanupErr := s.tun.Stop(stopCtx)
		stopCancel()
		if cleanupErr == nil {
			status, err = s.tun.Activate(ctx, primaryConfig)
			if err == nil {
				recoveredStaleAdapter = true
			} else {
				err = errors.Join(errors.New("stale TUN adapter retry failed"), err)
			}
		} else {
			err = errors.Join(errors.New("stale TUN adapter retry cleanup failed"), cleanupErr)
		}
	}
	ipv4OnlyFallback := false
	if err != nil && strings.TrimSpace(params.IPv4FallbackConfigPath) != "" && isIPv6AddressSetupError(err) {
		// The first activation can have created a partial Wintun/TUN state.
		// Stop it completely before asking sing-box to try the IPv4-only file.
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 20*time.Second)
		_, cleanupErr := s.tun.Stop(stopCtx)
		stopCancel()
		if cleanupErr == nil {
			status, err = s.tun.Activate(ctx, fallbackConfig)
			if err == nil {
				ipv4OnlyFallback = true
			} else {
				err = errors.Join(errors.New("IPv4-only TUN retry failed"), err)
			}
		} else {
			err = errors.Join(errors.New("IPv4-only TUN retry cleanup failed"), cleanupErr)
		}
	}
	if err == nil {
		s.activeTunGeneration = status.Generation
		return protocol.Result(request.ID, api.TunLifecycleResult{
			Accepted:              true,
			RecoveredStaleAdapter: recoveredStaleAdapter,
			IPv4OnlyFallback:      ipv4OnlyFallback,
			Tun:                   status,
		})
	}

	stopCtx, stopCancel := context.WithTimeout(
		context.Background(),
		20*time.Second,
	)
	_, tunStopErr := s.tun.Stop(stopCtx)
	stopCancel()
	wfpErr := s.closeDNSExemption()
	proxyCtx, proxyCancel := context.WithTimeout(
		context.Background(),
		5*time.Second,
	)
	proxyStopErr := s.proxy.Stop(proxyCtx)
	proxyCancel()
	s.proxy = nil
	s.mode = ""
	s.adapters = nil
	s.activeTunGeneration = 0
	_, _ = s.runtime.Transition(engineRuntime.StateFailed, "TUN activation failed")
	return protocol.Failure(
		request.ID,
		"tun_failed",
		"could not activate managed TUN lifecycle",
		map[string]any{
			"message": errors.Join(err, tunStopErr, wfpErr, proxyStopErr).Error(),
		},
	)
}

func (s *Server) authorizeTunConfig(config tun.Config) (tun.Config, error) {
	trusted := strings.TrimSpace(s.metadata.TunExecutable)
	if trusted == "" {
		return tun.Config{}, errors.New("trusted sing-box executable policy is unavailable")
	}
	trusted, err := filepath.Abs(trusted)
	if err != nil {
		return tun.Config{}, fmt.Errorf("resolve trusted sing-box executable: %w", err)
	}
	pinnedDigest := strings.TrimSpace(s.metadata.TunExecutableSHA256)
	asserted := strings.TrimSpace(config.Executable)
	if asserted != "" {
		asserted, err = filepath.Abs(asserted)
		if err != nil {
			return tun.Config{}, fmt.Errorf("resolve requested sing-box executable: %w", err)
		}
		if !strings.EqualFold(filepath.Clean(asserted), filepath.Clean(trusted)) && pinnedDigest == "" {
			return tun.Config{}, errors.New("requested sing-box executable does not match the trusted policy")
		}
	}
	// A machine service owns the pinned executable location. The desktop keeps
	// an application-local Core copy for UAC fallback, so its legacy asserted
	// path can legitimately differ after the protected ProgramData deployment.
	// Never execute that asserted path: the service policy path and digest win.
	config.Executable = filepath.Clean(trusted)
	config.ExecutableSHA256 = pinnedDigest
	config.RequireProtectedConfig = config.ExecutableSHA256 != ""
	if config.ExecutableSHA256 != "" && strings.TrimSpace(config.ConfigSHA256) == "" {
		return tun.Config{}, errors.New("pinned sing-box requires a pinned configuration digest")
	}
	return config, nil
}

func isIPv6AddressSetupError(err error) bool {
	if err == nil {
		return false
	}
	value := strings.ToLower(err.Error())
	return strings.Contains(value, "set ipv6 address") ||
		(strings.Contains(value, "ipv6") && strings.Contains(value, "element not found"))
}

func isStaleTunAdapterError(err error) bool {
	if err == nil {
		return false
	}
	value := strings.ToLower(err.Error())
	return strings.Contains(value, "cannot create a file when that file already exists") ||
		(strings.Contains(value, "create adapter") &&
			strings.Contains(value, "open existing adapter") &&
			strings.Contains(value, "element not found")) ||
		strings.Contains(value, "stale hypomux-tun device still exists")
}

func (s *Server) deactivateTun(
	ctx context.Context,
	requestID string,
) protocol.Response {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	statusBefore := s.tun.Status()
	status, err := s.tun.Stop(ctx)
	wfpErr := s.closeDNSExemption()
	if joined := errors.Join(err, wfpErr); joined != nil {
		return protocol.Failure(
			requestID,
			"tun_failed",
			"could not deactivate managed TUN lifecycle",
			map[string]any{"message": joined.Error()},
		)
	}
	s.activeTunGeneration = 0
	return protocol.Result(requestID, api.TunLifecycleResult{
		Accepted: statusBefore.State != tun.StateStopped,
		Tun:      status,
	})
}

func (s *Server) handleTunLog(message string) {
	_ = s.emitEvent(api.EventLogRecord, api.LogRecordData{
		Component: "sing-box",
		Message:   message,
	})
}

func (s *Server) handleTunUnexpectedExit(status tun.Status) {
	s.lifecycleMu.Lock()
	if status.Generation == 0 || status.Generation != s.activeTunGeneration {
		s.lifecycleMu.Unlock()
		return
	}
	s.activeTunGeneration = 0
	_ = s.closeDNSExemption()
	if s.mode == "tun_tcp_pool" && s.proxy != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = s.proxy.Stop(ctx)
		cancel()
		s.proxy = nil
		s.mode = ""
		s.adapters = nil
	}
	state := s.runtime.Snapshot().State
	if state == engineRuntime.StateRunning || state == engineRuntime.StateDegraded {
		_, _ = s.runtime.Transition(
			engineRuntime.StateFailed,
			"managed TUN sidecar exited unexpectedly",
		)
	}
	engineState := s.runtime.Snapshot()
	s.lifecycleMu.Unlock()
	_ = s.emitEvent(api.EventTunStateChanged, status)
	_ = s.emitEvent(api.EventEngineStateChanged, engineState)
}

func (s *Server) stopProxyForHostExit() {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	tunCtx, tunCancel := context.WithTimeout(context.Background(), 20*time.Second)
	_, _ = s.tun.Stop(tunCtx)
	tunCancel()
	s.activeTunGeneration = 0
	_ = s.closeDNSExemption()
	if s.proxy != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = s.proxy.Stop(ctx)
		cancel()
	}
	s.proxy = nil
	s.mode = ""
	s.adapters = nil
	state := s.runtime.Snapshot().State
	if state == engineRuntime.StateRunning || state == engineRuntime.StateDegraded {
		_, _ = s.runtime.Transition(engineRuntime.StateStopping, "host exiting")
		_, _ = s.runtime.Transition(engineRuntime.StateStopped, "host exited")
	}
}

func (s *Server) closeDNSExemption() error {
	if s.dnsExemption == nil {
		return nil
	}
	current := s.dnsExemption
	s.dnsExemption = nil
	return current.Close()
}

func (s *Server) uptimeMilliseconds() int64 {
	return time.Since(s.started).Milliseconds()
}

func (s *Server) writeMessage(message any) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.encoder.Encode(message)
}

func (s *Server) emitEvent(name string, data any) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.eventSeq++
	return s.encoder.Encode(protocol.Notification(s.eventSeq, name, data))
}
