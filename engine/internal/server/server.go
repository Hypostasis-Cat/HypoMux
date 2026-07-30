package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"runtime"
	"strings"
	"time"

	"github.com/Hypostasis-Cat/HypoMux/engine/internal/diagnostic"
	"github.com/Hypostasis-Cat/HypoMux/engine/internal/platform"
	"github.com/Hypostasis-Cat/HypoMux/engine/internal/protocol"
	"github.com/Hypostasis-Cat/HypoMux/engine/internal/proxy"
	engineRuntime "github.com/Hypostasis-Cat/HypoMux/engine/internal/runtime"
)

const maxRequestBytes = 1024 * 1024

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
	scanner.Buffer(make([]byte, 64*1024), maxRequestBytes)

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
			event := s.notification("engine.state_changed", stateAfter)
			if err := s.encoder.Encode(event); err != nil {
				return fmt.Errorf("write state event: %w", err)
			}
		}
		if shutdown {
			event := s.notification("host.exiting", map[string]any{
				"reason": "requested",
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
	case "engine.hello":
		return protocol.Result(request.ID, s.hello()), false
	case "engine.status":
		return protocol.Result(request.ID, s.status()), false
	case "engine.start":
		return s.startProxy(request), false
	case "engine.stop":
		return s.stopProxy(request.ID), false
	case "engine.telemetry":
		return s.proxyTelemetry(request), false
	case "health.check":
		return protocol.Result(request.ID, map[string]any{
			"ok":             true,
			"state":          s.runtime.Snapshot().State,
			"host_uptime_ms": s.uptimeMilliseconds(),
		}), false
	case "diagnostic.run":
		var params struct {
			SourceIP string `json:"src_ip"`
			TargetIP string `json:"target_ip"`
			Count    int    `json:"count"`
			Timeout  int    `json:"timeout_ms"`
		}
		if len(request.Params) > 0 {
			if err := json.Unmarshal(request.Params, &params); err != nil {
				return protocol.Failure(request.ID, "invalid_params", "diagnostic params are not valid JSON", nil), false
			}
		}
		result := diagnostic.Run(ctx, diagnostic.Config{
			SourceIP: params.SourceIP,
			TargetIP: params.TargetIP,
			Count:    params.Count,
			Timeout:  time.Duration(params.Timeout) * time.Millisecond,
		})
		return protocol.Result(request.ID, result), false
	case "host.shutdown":
		_ = s.stopProxy(request.ID)
		return protocol.Result(request.ID, map[string]any{"accepted": true}), true
	default:
		return protocol.Failure(
			request.ID,
			"method_not_found",
			"unknown method",
			map[string]any{"method": request.Method},
		), false
	}
}

func (s *Server) hello() map[string]any {
	return map[string]any{
		"engine":           s.metadata.Name,
		"engine_version":   s.metadata.Version,
		"commit":           s.metadata.Commit,
		"protocol_version": protocol.Version,
		"transport":        "stdio-jsonl",
		"capabilities": []string{
			"engine.hello",
			"engine.status",
			"engine.start",
			"engine.stop",
			"engine.telemetry",
			"health.check",
			"diagnostic.run",
			"host.shutdown",
		},
		"os":         runtime.GOOS,
		"arch":       runtime.GOARCH,
		"pid":        s.identity.ProcessID,
		"elevated":   s.identity.Elevated,
		"started_at": s.startedAt,
	}
}

func (s *Server) status() map[string]any {
	result := map[string]any{
		"engine":         s.runtime.Snapshot(),
		"host_uptime_ms": s.uptimeMilliseconds(),
	}
	if s.proxy != nil {
		result["proxy"] = map[string]any{
			"running":   s.proxy.Running(),
			"endpoints": s.proxy.Endpoints(),
			"telemetry": s.proxy.Snapshot(false),
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
	var params struct {
		Mode             string          `json:"mode"`
		ListenHost       string          `json:"listen_host"`
		SOCKSPort        int             `json:"socks_port"`
		HTTPPort         int             `json:"http_port"`
		Weighted         bool            `json:"weighted"`
		Adapters         []proxy.Adapter `json:"adapters"`
		ConnectTimeoutMS int             `json:"connect_timeout_ms"`
	}
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
	proxyServer, err := proxy.New(proxy.Config{
		ListenHost:     params.ListenHost,
		SOCKSPort:      params.SOCKSPort,
		HTTPPort:       params.HTTPPort,
		Weighted:       params.Weighted,
		Adapters:       params.Adapters,
		ConnectTimeout: time.Duration(params.ConnectTimeoutMS) * time.Millisecond,
	})
	if err == nil {
		var endpoints proxy.Endpoints
		endpoints, err = proxyServer.Start()
		if err == nil {
			s.proxy = proxyServer
			_, _ = s.runtime.Transition(engineRuntime.StateRunning, "proxy listeners ready")
			return protocol.Result(request.ID, map[string]any{
				"state":     s.runtime.Snapshot(),
				"endpoints": endpoints,
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
		return protocol.Result(requestID, map[string]any{
			"accepted": false,
			"state":    s.runtime.Snapshot(),
		})
	}
	if state == engineRuntime.StateFailed {
		s.proxy = nil
		_, _ = s.runtime.Transition(engineRuntime.StateStopped, "failed proxy cleared")
		return protocol.Result(requestID, map[string]any{
			"accepted": true,
			"state":    s.runtime.Snapshot(),
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
	return protocol.Result(requestID, map[string]any{
		"accepted": true,
		"state":    s.runtime.Snapshot(),
	})
}

func (s *Server) proxyTelemetry(request protocol.Request) protocol.Response {
	if s.proxy == nil || !s.proxy.Running() {
		return protocol.Failure(request.ID, "invalid_state", "proxy engine is not running", nil)
	}
	var params struct {
		IncludeConnections bool `json:"include_connections"`
	}
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
