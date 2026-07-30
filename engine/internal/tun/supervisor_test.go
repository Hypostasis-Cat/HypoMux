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
		*status.ExitCode != 17 {
		t.Fatalf("early-exit status = %#v", status)
	}
	select {
	case event := <-unexpected:
		if event.State != StateFailed ||
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
