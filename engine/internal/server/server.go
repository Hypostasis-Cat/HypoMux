package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
)

type Metadata struct {
	Name    string
	Version string
	Commit  string
}

type tunController interface {
	Activate(context.Context, tun.Config) (tun.Status, error)
	Stop(context.Context) (tun.Status, error)
	Status() tun.Status
	SetHandlers(func(string), func(tun.Status))
}

type Server struct {
	input       io.Reader
	encoder     *json.Encoder
	metadata    Metadata
	identity    platform.Identity
	runtime     *engineRuntime.Runtime
	proxy       *proxy.Server
	tun         tunController
	mode        string
	startedAt   time.Time
	started     time.Time
	writeMu     sync.Mutex
	lifecycleMu sync.Mutex
	eventSeq    uint64
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
	proxyServer, err := proxy.New(params.ProxyConfig())
	if err == nil {
		proxyServer.SetDNSFallbackHandler(s.handleDNSFallback)
		var endpoints proxy.Endpoints
		endpoints, err = proxyServer.Start()
		if err == nil {
			s.proxy = proxyServer
			s.mode = mode
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
		return protocol.Result(requestID, api.EngineStopResult{
			Accepted: false,
			State:    s.runtime.Snapshot(),
		})
	}
	if state == engineRuntime.StateFailed {
		stopCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		_, tunErr := s.tun.Stop(stopCtx)
		cancel()
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
		_, _ = s.runtime.Transition(engineRuntime.StateStopped, "failed proxy cleared")
		if err := errors.Join(tunErr, proxyErr); err != nil {
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
	var proxyErr error
	if s.proxy != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		proxyErr = s.proxy.Stop(ctx)
		cancel()
	}
	if err := errors.Join(tunErr, proxyErr); err != nil {
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
	if s.mode != "proxy" {
		return protocol.Failure(
			request.ID,
			"invalid_state",
			"DNS methods are unavailable in tun_tcp_pool mode",
			nil,
		)
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
	if s.mode != "proxy" {
		return protocol.Failure(
			requestID,
			"invalid_state",
			"DNS methods are unavailable in tun_tcp_pool mode",
			nil,
		)
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
	status, err := s.tun.Activate(ctx, params.Config())
	if err == nil {
		return protocol.Result(request.ID, api.TunLifecycleResult{
			Accepted: true,
			Tun:      status,
		})
	}

	stopCtx, stopCancel := context.WithTimeout(
		context.Background(),
		20*time.Second,
	)
	_, tunStopErr := s.tun.Stop(stopCtx)
	stopCancel()
	proxyCtx, proxyCancel := context.WithTimeout(
		context.Background(),
		5*time.Second,
	)
	proxyStopErr := s.proxy.Stop(proxyCtx)
	proxyCancel()
	s.proxy = nil
	s.mode = ""
	_, _ = s.runtime.Transition(engineRuntime.StateFailed, "TUN activation failed")
	return protocol.Failure(
		request.ID,
		"tun_failed",
		"could not activate managed TUN lifecycle",
		map[string]any{
			"message": errors.Join(err, tunStopErr, proxyStopErr).Error(),
		},
	)
}

func (s *Server) deactivateTun(
	ctx context.Context,
	requestID string,
) protocol.Response {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	statusBefore := s.tun.Status()
	status, err := s.tun.Stop(ctx)
	if err != nil {
		return protocol.Failure(
			requestID,
			"tun_failed",
			"could not deactivate managed TUN lifecycle",
			map[string]any{"message": err.Error()},
		)
	}
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
	if s.mode == "tun_tcp_pool" && s.proxy != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = s.proxy.Stop(ctx)
		cancel()
		s.proxy = nil
		s.mode = ""
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
	if s.proxy != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = s.proxy.Stop(ctx)
		cancel()
	}
	s.proxy = nil
	s.mode = ""
	state := s.runtime.Snapshot().State
	if state == engineRuntime.StateRunning || state == engineRuntime.StateDegraded {
		_, _ = s.runtime.Transition(engineRuntime.StateStopping, "host exiting")
		_, _ = s.runtime.Transition(engineRuntime.StateStopped, "host exited")
	}
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
