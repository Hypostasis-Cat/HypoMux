package tun

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Hypostasis-Cat/HypoMux/engine/internal/fileintegrity"
)

type testContainment struct {
	closed atomic.Int32
}

func (c *testContainment) Close() error {
	c.closed.Add(1)
	return nil
}

func TestSupervisorActivatesStopsAndCleansExactRun(t *testing.T) {
	supervisor, cleanupCalls, containment := testSupervisor(t, "stable")
	logs := make(chan string, 16)
	supervisor.SetHandlers(func(message string) {
		logs <- message
	}, nil)

	status, err := supervisor.Activate(
		context.Background(),
		testConfig(t),
	)
	if err != nil {
		t.Fatalf("Activate() failed: %v", err)
	}
	if status.State != StateRunning || status.PID <= 0 ||
		status.StartedAt == nil {
		t.Fatalf("running status = %#v", status)
	}
	stopped, err := supervisor.Stop(context.Background())
	if err != nil {
		t.Fatalf("Stop() failed: %v", err)
	}
	if stopped.State != StateStopped || stopped.PID != 0 {
		t.Fatalf("stopped status = %#v", stopped)
	}
	if got := cleanupCalls.Load(); got != 2 {
		t.Fatalf("cleanup calls = %d, want preflight + stop", got)
	}
	if containment.closed.Load() == 0 {
		t.Fatal("process containment was not closed")
	}
	close(logs)
	joined := ""
	for message := range logs {
		joined += message
	}
	if !strings.Contains(joined, "configuration check passed") ||
		!strings.Contains(joined, "[sing-box:stderr] helper-ready") {
		t.Fatalf("forwarded logs = %q", joined)
	}
}

func TestSupervisorReturnsWhenTunInterfaceIsReady(t *testing.T) {
	supervisor, _, _ := testSupervisor(t, "stable")
	supervisor.startupReady = func() bool { return true }
	supervisor.readyStableFor = 20 * time.Millisecond
	config := testConfig(t)
	config.StartupTimeout = 900 * time.Millisecond
	started := time.Now()
	if _, err := supervisor.Activate(context.Background(), config); err != nil {
		t.Fatalf("Activate() failed: %v", err)
	}
	if elapsed := time.Since(started); elapsed >= 500*time.Millisecond {
		t.Fatalf("ready activation took %v", elapsed)
	}
	if _, err := supervisor.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() failed: %v", err)
	}
}

func TestSupervisorTreatsStartupTimeoutAsFailure(t *testing.T) {
	supervisor, cleanupCalls, _ := testSupervisor(t, "stable")
	supervisor.startupReady = func() bool { return false }
	config := testConfig(t)
	config.StartupTimeout = 140 * time.Millisecond
	status, err := supervisor.Activate(context.Background(), config)
	if err == nil || status.State != StateFailed {
		t.Fatalf("startup timeout must fail: status=%#v err=%v", status, err)
	}
	if !strings.Contains(status.LastError, "did not become ready") || status.PID != 0 {
		t.Fatalf("timeout failure lost readiness evidence: %#v", status)
	}
	if cleanupCalls.Load() != 2 {
		t.Fatalf("timeout cleanup calls = %d, want preflight + failed-run cleanup", cleanupCalls.Load())
	}
}

func TestSupervisorRejectsConfigBeforeNetworkCleanup(t *testing.T) {
	supervisor, cleanupCalls, _ := testSupervisor(t, "check-fails")
	status, err := supervisor.Activate(
		context.Background(),
		testConfig(t),
	)
	if err == nil {
		t.Fatal("invalid sidecar configuration unexpectedly activated")
	}
	if status.State != StateFailed ||
		!strings.Contains(status.LastError, "synthetic-check-failure") {
		t.Fatalf("failed status = %#v, err=%v", status, err)
	}
	if cleanupCalls.Load() != 0 {
		t.Fatal("network cleanup ran before configuration validation")
	}
}

func TestSupervisorReportsEarlyAndUnexpectedExit(t *testing.T) {
	supervisor, cleanupCalls, _ := testSupervisor(t, "early-exit")
	unexpected := make(chan Status, 1)
	supervisor.SetHandlers(nil, func(status Status) {
		unexpected <- status
	})

	status, err := supervisor.Activate(
		context.Background(),
		testConfig(t),
	)
	if err == nil {
		t.Fatal("early sidecar exit unexpectedly activated")
	}
	if status.State != StateFailed || status.ExitCode == nil ||
		*status.ExitCode != 17 || status.Generation == 0 {
		t.Fatalf("early-exit status = %#v", status)
	}
	select {
	case event := <-unexpected:
		if event.State != StateFailed ||
			event.Generation != status.Generation ||
			!strings.Contains(event.LastError, "helper-crashed") {
			t.Fatalf("unexpected-exit callback = %#v", event)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("unexpected-exit callback was not delivered")
	}
	if cleanupCalls.Load() != 2 {
		t.Fatalf("early-exit cleanup calls = %d", cleanupCalls.Load())
	}
}

func TestSupervisorConcurrentStopIsIdempotent(t *testing.T) {
	supervisor, _, _ := testSupervisor(t, "stable")
	if _, err := supervisor.Activate(
		context.Background(),
		testConfig(t),
	); err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	errorsSeen := make(chan error, 8)
	for range 8 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			_, err := supervisor.Stop(context.Background())
			errorsSeen <- err
		}()
	}
	workers.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		if err != nil {
			t.Fatalf("concurrent Stop() failed: %v", err)
		}
	}
	if supervisor.Status().State != StateStopped {
		t.Fatalf("final status = %#v", supervisor.Status())
	}
}

func testSupervisor(
	t *testing.T,
	mode string,
) (*Supervisor, *atomic.Int32, *testContainment) {
	t.Helper()
	supervisor := NewSupervisor()
	var cleanupCalls atomic.Int32
	containment := &testContainment{}
	supervisor.cleanup = func(context.Context) error {
		cleanupCalls.Add(1)
		return nil
	}
	supervisor.contain = func(*os.Process) (processContainment, error) {
		return containment, nil
	}
	supervisor.configure = func(*exec.Cmd) {}
	supervisor.stageConfig = func(config Config) (string, func(), error) {
		return config.ConfigPath, func() {}, nil
	}
	if mode == "stable" {
		supervisor.startupReady = func() bool { return true }
		supervisor.readyStableFor = 10 * time.Millisecond
	} else {
		supervisor.startupReady = func() bool { return false }
	}
	supervisor.command = func(
		ctx context.Context,
		_ string,
		arguments ...string,
	) *exec.Cmd {
		helperArguments := []string{
			"-test.run=TestTunSidecarHelperProcess",
			"--",
		}
		helperArguments = append(helperArguments, arguments...)
		command := exec.CommandContext(ctx, os.Args[0], helperArguments...)
		command.Env = append(
			os.Environ(),
			"HYPOMUX_TUN_HELPER=1",
			"HYPOMUX_TUN_HELPER_MODE="+mode,
		)
		return command
	}
	return supervisor, &cleanupCalls, containment
}

func testConfig(t *testing.T) Config {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	configPath := t.TempDir() + string(os.PathSeparator) + "config.json"
	if err := os.WriteFile(configPath, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	return Config{
		Executable:     executable,
		ConfigPath:     configPath,
		StartupTimeout: 120 * time.Millisecond,
	}
}

func TestNormalizeConfigEnforcesPinnedExecutableDigest(t *testing.T) {
	directory := t.TempDir()
	executable := directory + string(os.PathSeparator) + "sing-box.exe"
	configPath := directory + string(os.PathSeparator) + "config.json"
	if err := os.WriteFile(executable, []byte("trusted-sidecar"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	digest, err := fileintegrity.SHA256(executable)
	if err != nil {
		t.Fatal(err)
	}
	config := Config{
		Executable:       executable,
		ExecutableSHA256: digest,
		ConfigPath:       configPath,
		StartupTimeout:   time.Second,
	}
	if _, err := normalizeConfig(config); err != nil {
		t.Fatalf("trusted executable was rejected: %v", err)
	}
	if err := os.WriteFile(executable, []byte("mutated-sidecar"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := normalizeConfig(config); err == nil {
		t.Fatal("modified executable passed its pinned digest")
	}
}

func TestNormalizeConfigEnforcesPinnedConfigurationDigest(t *testing.T) {
	config := testConfig(t)
	digest, err := fileintegrity.SHA256(config.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	config.ConfigSHA256 = digest
	if _, err := normalizeConfig(config); err != nil {
		t.Fatalf("trusted configuration was rejected: %v", err)
	}
	if err := os.WriteFile(config.ConfigPath, []byte(`{"tampered":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := normalizeConfig(config); err == nil {
		t.Fatal("modified configuration passed its pinned digest")
	}
}

func TestTunSidecarHelperProcess(t *testing.T) {
	if os.Getenv("HYPOMUX_TUN_HELPER") != "1" {
		return
	}
	separator := -1
	for index, argument := range os.Args {
		if argument == "--" {
			separator = index
			break
		}
	}
	if separator < 0 || separator+1 >= len(os.Args) {
		os.Exit(90)
	}
	action := os.Args[separator+1]
	mode := os.Getenv("HYPOMUX_TUN_HELPER_MODE")
	switch action {
	case "check":
		if mode == "check-fails" {
			_, _ = fmt.Fprintln(os.Stderr, "synthetic-check-failure")
			os.Exit(9)
		}
		os.Exit(0)
	case "run":
		_, _ = fmt.Fprintln(os.Stderr, "helper-ready")
		if mode == "early-exit" {
			_, _ = fmt.Fprintln(os.Stderr, "helper-crashed")
			os.Exit(17)
		}
		for {
			time.Sleep(time.Second)
		}
	default:
		os.Exit(91)
	}
}
