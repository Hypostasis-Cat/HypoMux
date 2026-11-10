package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

// fakeEngine serves one JSONL session side of a net.Pipe. Handlers are keyed
// by request method and receive the raw params; returning an error produces a
// protocol failure response.
type fakeEngine struct {
	handlers map[string]func(params json.RawMessage) (any, error)
	events   []string // events emitted before each response
	silent   bool     // read requests but never answer, for timeout tests
	conn     net.Conn
}

func startFakeEngine(t *testing.T) (*fakeEngine, *session) {
	t.Helper()
	engineConn, clientConn := net.Pipe()
	engine := &fakeEngine{handlers: map[string]func(json.RawMessage) (any, error){}}
	engine.conn = engineConn
	done := make(chan struct{})
	go func() {
		defer close(done)
		engine.serve()
	}()
	t.Cleanup(func() {
		_ = engineConn.Close()
		_ = clientConn.Close()
		<-done
	})
	return engine, newSession(clientConn, &strings.Builder{})
}

func (e *fakeEngine) serve() {
	decoder := json.NewDecoder(e.conn)
	for {
		var request struct {
			Protocol int             `json:"protocol"`
			ID       string          `json:"id"`
			Method   string          `json:"method"`
			Params   json.RawMessage `json:"params"`
		}
		if err := decoder.Decode(&request); err != nil {
			return
		}
		if e.silent {
			continue
		}
		for _, event := range e.events {
			writeEnveloped(e.conn, map[string]any{
				"protocol": 1, "sequence": 1, "event": event, "data": map[string]any{"reason": "test"},
			})
		}
		handler, known := e.handlers[request.Method]
		if !known {
			writeEnveloped(e.conn, map[string]any{
				"protocol": 1, "id": request.ID,
				"error": map[string]any{"code": "method_not_found", "message": "unknown method"},
			})
			continue
		}
		result, err := handler(request.Params)
		if err != nil {
			writeEnveloped(e.conn, map[string]any{
				"protocol": 1, "id": request.ID,
				"error": map[string]any{"code": "invalid_params", "message": err.Error()},
			})
			continue
		}
		writeEnveloped(e.conn, map[string]any{
			"protocol": 1, "id": request.ID, "result": result,
		})
	}
}

func writeEnveloped(conn net.Conn, message map[string]any) {
	payload, err := json.Marshal(message)
	if err != nil {
		return
	}
	_, _ = conn.Write(append(payload, '\n'))
}

func TestSessionCorrelatesResponsesAndReportsEvents(t *testing.T) {
	engine, s := startFakeEngine(t)
	engine.events = []string{"log.record"}
	engine.handlers["engine.hello"] = func(params json.RawMessage) (any, error) {
		return map[string]any{"engine": "hypomux-engine", "protocol_version": 1}, nil
	}

	var hello helloResult
	if err := s.call(context.Background(), "engine.hello", nil, &hello); err != nil {
		t.Fatalf("call: %v", err)
	}
	if hello.Engine != "hypomux-engine" || hello.ProtocolVersion != 1 {
		t.Fatalf("hello = %#v", hello)
	}
	events := s.events.(*strings.Builder).String()
	if !strings.Contains(events, "[log.record]") {
		t.Fatalf("event not reported: %q", events)
	}
}

func TestSessionSurfacesRemoteErrors(t *testing.T) {
	engine, s := startFakeEngine(t)
	engine.handlers["dns.status"] = func(params json.RawMessage) (any, error) {
		return nil, errors.New("proxy engine is not running")
	}

	err := s.call(context.Background(), "dns.status", nil, nil)
	var remote *RemoteError
	if !errors.As(err, &remote) || remote.Code != "invalid_params" {
		t.Fatalf("want remote error, got %v", err)
	}
}

func TestSessionTimesOutWhenCoreNeverAnswers(t *testing.T) {
	engine, s := startFakeEngine(t)
	engine.silent = true
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := s.call(ctx, "engine.status", nil, nil); err == nil {
		t.Fatal("expected a timeout error")
	}
}

func TestSessionRejectsEmptyMethod(t *testing.T) {
	_, s := startFakeEngine(t)
	if err := s.call(context.Background(), "  ", nil, nil); err == nil {
		t.Fatal("empty method must be rejected")
	}
}
