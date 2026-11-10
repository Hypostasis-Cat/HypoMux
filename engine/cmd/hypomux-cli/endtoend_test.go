package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"strings"
	"testing"

	"github.com/Hypostasis-Cat/HypoMux/engine/internal/server"
)

// engineTestHost wires a real protocol-v1 engine server (the same package the
// installed Core runs) to the CLI over net.Pipe. Each invocation gets a fresh
// pipe and server, matching the service's one-session-per-client behavior.
type engineTestHost struct {
	factory transportFactory
	servers []context.CancelFunc
}

func startEngineTestHost(t *testing.T) *engineTestHost {
	t.Helper()
	host := &engineTestHost{}
	host.factory = func() transport {
		return &hostTransport{host: host}
	}
	t.Cleanup(func() {
		for _, cancel := range host.servers {
			cancel()
		}
	})
	return host
}

type hostTransport struct {
	host      *engineTestHost
	described bool
}

func (t *hostTransport) Connect(ctx context.Context) (io.ReadWriteCloser, error) {
	clientConn, engineConn := net.Pipe()
	engineServer := server.New(engineConn, engineConn, server.Metadata{
		Name:    "hypomux-engine",
		Version: "test",
		Commit:  "test",
	})
	runCtx, cancel := context.WithCancel(context.Background())
	t.host.servers = append(t.host.servers, cancel)
	go func() {
		_ = engineServer.Run(runCtx)
	}()
	// Closing the client side also releases the engine's reader.
	go func() {
		<-ctx.Done()
		_ = clientConn.Close()
		_ = engineConn.Close()
	}()
	return clientConn, nil
}

func (t *hostTransport) Describe() string {
	return "engine-test-host"
}

func runCLI(t *testing.T, factory transportFactory, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(args, &stdout, &stderr, factory)
	return code, stdout.String(), stderr.String()
}

func TestCLIEndToEndAgainstRealEngineServer(t *testing.T) {
	host := startEngineTestHost(t)

	code, stdout, stderr := runCLI(t, host.factory, "hello")
	if code != 0 {
		t.Fatalf("hello exit = %d, stderr = %s", code, stderr)
	}
	if !strings.Contains(stdout, "hypomux-engine test") {
		t.Fatalf("hello output = %q", stdout)
	}
	if !strings.Contains(stdout, "engine.hello") {
		t.Fatalf("capabilities missing from output: %q", stdout)
	}

	code, stdout, stderr = runCLI(t, host.factory, "health")
	if code != 0 {
		t.Fatalf("health exit = %d, stderr = %s", code, stderr)
	}
	if !strings.Contains(stdout, "stopped") {
		t.Fatalf("health output = %q", stdout)
	}

	// engine.stop on an already-stopped engine is a safe no-op that reports
	// accepted=false.
	code, stdout, stderr = runCLI(t, host.factory, "stop")
	if code != 0 {
		t.Fatalf("stop exit = %d, stderr = %s", code, stderr)
	}
	if !strings.Contains(stdout, "无需停止") {
		t.Fatalf("stop output = %q", stdout)
	}
}

func TestCLIJSONOutputMatchesProtocolResult(t *testing.T) {
	host := startEngineTestHost(t)
	code, stdout, stderr := runCLI(t, host.factory, "health", "--json")
	if code != 0 {
		t.Fatalf("health --json exit = %d, stderr = %s", code, stderr)
	}
	var result struct {
		OK           bool   `json:"ok"`
		State        string `json:"state"`
		HostUptimeMS int64  `json:"host_uptime_ms"`
	}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("invalid JSON output %q: %v", stdout, err)
	}
	if !result.OK || result.State != "stopped" {
		t.Fatalf("result = %#v", result)
	}
}

func TestCLIRemoteErrorFailsCommand(t *testing.T) {
	host := startEngineTestHost(t)
	// dns.status requires a running proxy; the fresh engine is stopped.
	code, _, stderr := runCLI(t, host.factory, "dns", "status")
	if code != 1 {
		t.Fatalf("dns status exit = %d", code)
	}
	if !strings.Contains(stderr, "invalid_state") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestCLIRawRejectsUnknownMethodButCallsKnownOnes(t *testing.T) {
	host := startEngineTestHost(t)

	code, _, stderr := runCLI(t, host.factory, "raw", "missing.method")
	if code != 1 {
		t.Fatalf("raw unknown exit = %d", code)
	}
	if !strings.Contains(stderr, "missing.method") || !strings.Contains(stderr, "engine.hello") {
		t.Fatalf("stderr = %q", stderr)
	}

	code, stdout, stderr := runCLI(t, host.factory, "raw", "engine.status")
	if code != 0 {
		t.Fatalf("raw engine.status exit = %d, stderr = %s", code, stderr)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("invalid JSON %q: %v", stdout, err)
	}
	if _, ok := result["engine"]; !ok {
		t.Fatalf("result = %#v", result)
	}

	code, _, stderr = runCLI(t, host.factory, "raw", "engine.telemetry", "not-json")
	if code != 2 {
		t.Fatalf("raw bad params exit = %d, stderr = %s", code, stderr)
	}
}

func TestCLIRulesClearAgainstRealEngine(t *testing.T) {
	host := startEngineTestHost(t)
	// A fresh engine has no running proxy: update must report applied=false
	// rather than failing.
	code, stdout, stderr := runCLI(t, host.factory, "rules", "clear")
	if code != 0 {
		t.Fatalf("rules clear exit = %d, stderr = %s", code, stderr)
	}
	if !strings.Contains(stdout, "尚未启动") {
		t.Fatalf("output = %q", stdout)
	}
}

func TestCLIStatusReportsEngineState(t *testing.T) {
	host := startEngineTestHost(t)
	code, stdout, stderr := runCLI(t, host.factory, "status")
	if code != 0 {
		t.Fatalf("status exit = %d, stderr = %s", code, stderr)
	}
	if !strings.Contains(stdout, "stopped") {
		t.Fatalf("status output = %q", stdout)
	}
}

func TestCLIHelpAndVersionNeedNoCore(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"help"}, &stdout, &stderr, unreachableFactory(t)); code != 0 {
		t.Fatalf("help exit = %d", code)
	}
	if !strings.Contains(stdout.String(), "hypomux-cli") || !strings.Contains(stdout.String(), "start") {
		t.Fatalf("help output = %q", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"version"}, &stdout, &stderr, unreachableFactory(t)); code != 0 {
		t.Fatalf("version exit = %d", code)
	}
	if !strings.Contains(stdout.String(), "hypomux-cli") {
		t.Fatalf("version output = %q", stdout.String())
	}
}

func TestCLIRejectsUnknownAndMissingCommands(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run(nil, &stdout, &stderr, unreachableFactory(t)); code != 2 {
		t.Fatalf("no command exit = %d", code)
	}
	if !strings.Contains(stderr.String(), "缺少命令") {
		t.Fatalf("stderr = %q", stderr.String())
	}
	stderr.Reset()
	if code := run([]string{"nonsense"}, &stdout, &stderr, unreachableFactory(t)); code != 2 {
		t.Fatalf("unknown command exit = %d", code)
	}
	if !strings.Contains(stderr.String(), "未知命令") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func unreachableFactory(t *testing.T) transportFactory {
	t.Helper()
	return func() transport {
		return failingTransport{}
	}
}

type failingTransport struct{}

func (failingTransport) Connect(context.Context) (io.ReadWriteCloser, error) {
	return nil, context.DeadlineExceeded
}

func (failingTransport) Describe() string { return "failing" }
