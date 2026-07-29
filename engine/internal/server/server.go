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

	"github.com/Hypostasis-Cat/HypoMux/engine/internal/platform"
	"github.com/Hypostasis-Cat/HypoMux/engine/internal/protocol"
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

		response, shutdown := s.handle(line)
		if err := s.encoder.Encode(response); err != nil {
			return fmt.Errorf("write response: %w", err)
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

func (s *Server) handle(line []byte) (protocol.Response, bool) {
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
	case "health.check":
		return protocol.Result(request.ID, map[string]any{
			"ok":             true,
			"state":          s.runtime.Snapshot().State,
			"host_uptime_ms": s.uptimeMilliseconds(),
		}), false
	case "host.shutdown":
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
			"health.check",
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
	return map[string]any{
		"engine":         s.runtime.Snapshot(),
		"host_uptime_ms": s.uptimeMilliseconds(),
	}
}

func (s *Server) uptimeMilliseconds() int64 {
	return time.Since(s.started).Milliseconds()
}

func (s *Server) notification(name string, data any) protocol.Event {
	s.eventSeq++
	return protocol.Notification(s.eventSeq, name, data)
}
