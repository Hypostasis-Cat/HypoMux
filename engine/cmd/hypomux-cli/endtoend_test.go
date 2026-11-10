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

// engineTestHost wires a real protocol-v1 engine (the same package the installed
// Core runs) to the CLI over net.Pipe. It models production: one shared service
// engine singleton, with every CLI invocation attaching a short-lived session
// carrying the restricted CLI role.
type engineTestHost struct {
	factory transportFactory
	engine  *server.Server
}

func startEngineTestHost(t *testing.T) *engineTestHost {
	t.Helper()
	host := &engineTestHost{
		engine: server.NewHost(server.Metadata{
			Name:    "hypomux-engine",
			Version: "test",
			Commit:  "test",
		}),
	}
	host.factory = func() transport {
		return &hostTransport{host: host}
	}
	t.Cleanup(host.engine.Close)
	return host
}

type hostTransport struct {
	host      *engineTestHost
	described bool
}

func (t *hostTransport) Connect(ctx context.Context) (io.ReadWriteCloser, error) {
	clientConn, engineConn := net.Pipe()
	session := t.host.engine.Attach(engineConn, engineConn, server.RoleCLI)
	go func() {
		_ = session.Run(context.Background())
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

// Reproduces the production semantics the per-connection server model broke: a
// proxy started by one CLI invocation must still be running when a later
// invocation queries status, because both are sessions of one service engine.
func TestCLIStartSurvivesAcrossInvocations(t *testing.T) {
	host := startEngineTestHost(t)
	params := `{"mode":"proxy","listen_host":"127.0.0.1","socks_port":0,"http_port":0,"dns":{"policy":"off"},"adapters":[{"name":"loopback","source_ip":"127.0.0.1"}]}`
	code, _, stderr := runCLI(t, host.factory, "raw", "engine.start", params)
	if code != 0 {
		t.Fatalf("start exit = %d, stderr = %s", code, stderr)
	}

	// The starting process has exited; a fresh invocation attaches a new session.
	code, stdout, stderr := runCLI(t, host.factory, "status", "--json")
	if code != 0 {
		t.Fatalf("status exit = %d, stderr = %s", code, stderr)
	}
	var status struct {
		Engine struct {
			State string `json:"state"`
		} `json:"engine"`
	}
	if err := json.Unmarshal([]byte(stdout), &status); err != nil {
		t.Fatalf("invalid status JSON %q: %v", stdout, err)
	}
	if status.Engine.State != "running" {
		t.Fatalf("engine state across invocations = %q, want running", status.Engine.State)
	}

	if code, _, stderr := runCLI(t, host.factory, "stop"); code != 0 {
		t.Fatalf("stop exit = %d, stderr = %s", code, stderr)
	}
}

// The CLI role must not be able to reach privileged methods even through raw.
func TestCLIRawCannotReachPrivilegedMethods(t *testing.T) {
	host := startEngineTestHost(t)
	for _, method := range []string{"tun.activate", "mtu.set", "host.shutdown"} {
		code, _, stderr := runCLI(t, host.factory, "raw", method)
		if code != 1 {
			t.Fatalf("raw %s exit = %d, want 1", method, code)
		}
		if !strings.Contains(stderr, method) {
			t.Fatalf("raw %s stderr = %q", method, stderr)
		}
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
