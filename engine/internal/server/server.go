package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	api "github.com/Hypostasis-Cat/HypoMux/engine/internal/api/v1"
	"github.com/Hypostasis-Cat/HypoMux/engine/internal/diagnostic"
	"github.com/Hypostasis-Cat/HypoMux/engine/internal/platform"
	"github.com/Hypostasis-Cat/HypoMux/engine/internal/protocol"
	"github.com/Hypostasis-Cat/HypoMux/engine/internal/proxy"
	engineRuntime "github.com/Hypostasis-Cat/HypoMux/engine/internal/runtime"
)

type Metadata struct {
	Name    string
	Version string
	Commit  string
}

type Server struct {
	input     io.Reader
	encoder   *json.Encoder
	metadata  Metadata
	identity  platform.Identity
	runtime   *engineRuntime.Runtime
	proxy     *proxy.Server
	startedAt time.Time
	started   time.Time
	eventSeq  uint64
}

func New(input io.Reader, output io.Writer, metadata Metadata) *Server {
	now := time.Now()
	return &Server{
		input:     input,
		encoder:   json.NewEncoder(output),
		metadata:  metadata,
		identity:  platform.CurrentIdentity(),
		runtime:   engineRuntime.New(time.Now),
		startedAt: now.UTC(),
		started:   now,
	}
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
		response, shutdown := s.handle(ctx, line)
		if err := s.encoder.Encode(response); err != nil {
			return fmt.Errorf("write response: %w", err)
		}
		stateAfter := s.runtime.Snapshot()
		if stateAfter.Sequence != stateBefore.Sequence {
			event := s.notification(api.EventEngineStateChanged, stateAfter)
			if err := s.encoder.Encode(event); err != nil {
				return fmt.Errorf("write state event: %w", err)
			}
		}
		if shutdown {
			event := s.notification(api.EventHostExiting, api.HostExitingData{
				Reason: "requested",
			})
			if err := s.encoder.Encode(event); err != nil {
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
	result := api.StatusResult{
		Engine:       s.runtime.Snapshot(),
		HostUptimeMS: s.uptimeMilliseconds(),
	}
	if s.proxy != nil {
		result.Proxy = &api.ProxyStatus{
			Running:   s.proxy.Running(),
			Endpoints: s.proxy.Endpoints(),
			Telemetry: s.proxy.Snapshot(false),
		}
	}
	return result
}

func (s *Server) startProxy(request protocol.Request) protocol.Response {
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
	if params.Mode != "" && params.Mode != "proxy" {
		return protocol.Failure(
			request.ID,
			"unsupported_mode",
			"only proxy mode is available in this migration phase",
			map[string]any{"mode": params.Mode},
		)
	}
	if _, err := s.runtime.Transition(engineRuntime.StateStarting, "proxy start requested"); err != nil {
		return protocol.Failure(request.ID, "invalid_state", err.Error(), nil)
	}
	proxyServer, err := proxy.New(params.ProxyConfig())
	if err == nil {
		var endpoints proxy.Endpoints
		endpoints, err = proxyServer.Start()
		if err == nil {
			s.proxy = proxyServer
			_, _ = s.runtime.Transition(engineRuntime.StateRunning, "proxy listeners ready")
			return protocol.Result(request.ID, api.EngineStartResult{
				State:     s.runtime.Snapshot(),
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
	state := s.runtime.Snapshot().State
	if state == engineRuntime.StateStopped {
		return protocol.Result(requestID, api.EngineStopResult{
			Accepted: false,
			State:    s.runtime.Snapshot(),
		})
	}
	if state == engineRuntime.StateFailed {
		s.proxy = nil
		_, _ = s.runtime.Transition(engineRuntime.StateStopped, "failed proxy cleared")
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
	if s.proxy != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := s.proxy.Stop(ctx)
		cancel()
		if err != nil {
			_, _ = s.runtime.Transition(engineRuntime.StateFailed, "proxy stop failed")
			return protocol.Failure(
				requestID,
				"stop_failed",
				"could not stop proxy engine",
				map[string]any{"message": err.Error()},
			)
		}
	}
	s.proxy = nil
	_, _ = s.runtime.Transition(engineRuntime.StateStopped, "proxy stopped")
	return protocol.Result(requestID, api.EngineStopResult{
		Accepted: true,
		State:    s.runtime.Snapshot(),
	})
}

func (s *Server) proxyTelemetry(request protocol.Request) protocol.Response {
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

func (s *Server) stopProxyForHostExit() {
	if s.proxy == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	_ = s.proxy.Stop(ctx)
	cancel()
	s.proxy = nil
	state := s.runtime.Snapshot().State
	if state == engineRuntime.StateRunning || state == engineRuntime.StateDegraded {
		_, _ = s.runtime.Transition(engineRuntime.StateStopping, "host exiting")
		_, _ = s.runtime.Transition(engineRuntime.StateStopped, "host exited")
	}
}

func (s *Server) uptimeMilliseconds() int64 {
	return time.Since(s.started).Milliseconds()
}

func (s *Server) notification(name string, data any) protocol.Event {
	s.eventSeq++
	return protocol.Notification(s.eventSeq, name, data)
}
