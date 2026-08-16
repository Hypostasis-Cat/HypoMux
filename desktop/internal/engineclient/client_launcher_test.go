package engineclient

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

type failingLauncher struct {
	err error
}

func (launcher failingLauncher) Launch(context.Context, string) (*coreSession, error) {
	return nil, launcher.err
}

type countingLauncher struct {
	calls   *int
	session *coreSession
	err     error
}

type blockingLauncher struct {
	started chan struct{}
	release chan struct{}
}

type testCoreProcess struct {
	pid  int
	done chan struct{}
	once sync.Once
}

func newTestCoreProcess(pid int) *testCoreProcess {
	return &testCoreProcess{pid: pid, done: make(chan struct{})}
}

func (process *testCoreProcess) Wait() error {
	<-process.done
	return nil
}

func (process *testCoreProcess) Kill() error {
	process.once.Do(func() { close(process.done) })
	return nil
}

func (process *testCoreProcess) PID() int { return process.pid }

func closedProtocolSession(source coreSessionSource, pid int) *coreSession {
	client, server := net.Pipe()
	_ = server.Close()
	process := newTestCoreProcess(pid)
	return &coreSession{
		reader: client, writer: client, process: process, source: source,
		close: func() error {
			_ = process.Kill()
			return client.Close()
		},
	}
}

func helloProtocolSession(source coreSessionSource, pid int, protocol int) *coreSession {
	client, server := net.Pipe()
	process := newTestCoreProcess(pid)
	go func() {
		defer server.Close()
		line, err := bufio.NewReader(server).ReadBytes('\n')
		if err != nil {
			return
		}
		var request struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(line, &request) != nil {
			return
		}
		response, _ := json.Marshal(map[string]any{
			"protocol": ProtocolVersion,
			"id":       request.ID,
			"result": map[string]any{
				"engine": "hypomux-engine", "engine_version": "test",
				"protocol_version": protocol, "elevated": true, "pid": pid,
			},
		})
		_, _ = server.Write(append(response, '\n'))
		<-process.done
	}()
	return &coreSession{
		reader: client, writer: client, process: process, source: source,
		close: func() error {
			_ = process.Kill()
			return client.Close()
		},
	}
}

func (launcher blockingLauncher) Launch(context.Context, string) (*coreSession, error) {
	select {
	case launcher.started <- struct{}{}:
	default:
	}
	<-launcher.release
	return nil, errors.New("released blocking launcher")
}

func (launcher countingLauncher) Launch(context.Context, string) (*coreSession, error) {
	*launcher.calls++
	return launcher.session, launcher.err
}

func TestEnsureElevatedReturnsCancellationWithoutStartingCore(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HYPOMUX_ENGINE_PATH", executable)
	client := newClient(stdioLauncher{}, failingLauncher{err: ErrElevationCancelled})
	_, err = client.EnsureElevated(context.Background())
	if !errors.Is(err, ErrElevationCancelled) {
		t.Fatalf("expected elevation cancellation, got %v", err)
	}
	if client.Hello().ProtocolVersion != 0 {
		t.Fatal("a cancelled elevation must not leave a negotiated Core")
	}
}

func TestEnsureWaitForConcurrentStartupHonoursContext(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HYPOMUX_ENGINE_PATH", executable)
	launcher := blockingLauncher{started: make(chan struct{}, 1), release: make(chan struct{})}
	client := newClient(launcher, launcher)
	firstDone := make(chan error, 1)
	go func() {
		_, firstErr := client.Ensure(context.Background())
		firstDone <- firstErr
	}()
	select {
	case <-launcher.started:
	case <-time.After(time.Second):
		t.Fatal("first startup did not reach the launcher")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	_, err = client.EnsureElevated(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("concurrent startup wait did not honour its deadline: %v", err)
	}
	close(launcher.release)
	select {
	case <-firstDone:
	case <-time.After(time.Second):
		t.Fatal("first startup did not release the gate")
	}
}

func TestServiceFirstLauncherFallsBackOnlyWhenServiceIsNotInstalled(t *testing.T) {
	serviceCalls, fallbackCalls := 0, 0
	expected := &coreSession{}
	launcher := serviceFirstLauncher{
		service: countingLauncher{
			calls: &serviceCalls,
			err:   ErrCoreServiceUnavailable,
		},
		fallback: countingLauncher{
			calls:   &fallbackCalls,
			session: expected,
		},
	}
	session, err := launcher.Launch(context.Background(), "core.exe")
	if err != nil {
		t.Fatal(err)
	}
	if session != expected || serviceCalls != 1 || fallbackCalls != 1 {
		t.Fatalf("unexpected service-first result: session=%p service=%d fallback=%d", session, serviceCalls, fallbackCalls)
	}
}

func TestServiceFirstLauncherDoesNotBypassBrokenInstalledService(t *testing.T) {
	serviceCalls, fallbackCalls := 0, 0
	serviceErr := errors.New("installed service identity check failed")
	launcher := serviceFirstLauncher{
		service:  countingLauncher{calls: &serviceCalls, err: serviceErr},
		fallback: countingLauncher{calls: &fallbackCalls},
	}
	_, err := launcher.Launch(context.Background(), "core.exe")
	if !errors.Is(err, serviceErr) {
		t.Fatalf("expected service error, got %v", err)
	}
	if serviceCalls != 1 || fallbackCalls != 0 {
		t.Fatalf("unexpected calls: service=%d fallback=%d", serviceCalls, fallbackCalls)
	}
}

func TestServiceFirstLauncherFallsBackForStoppedServiceOnlyAfterPathValidation(t *testing.T) {
	serviceCalls, fallbackCalls, trustCalls := 0, 0, 0
	expected := &coreSession{source: coreSourceRunAs}
	launcher := serviceFirstLauncher{
		service:  countingLauncher{calls: &serviceCalls, err: ErrCoreServiceNotRunning},
		fallback: countingLauncher{calls: &fallbackCalls, session: expected},
		allowAutomaticFallbackPath: func(string) error {
			trustCalls++
			return nil
		},
	}
	session, err := launcher.Launch(context.Background(), "core.exe")
	if err != nil {
		t.Fatal(err)
	}
	if session != expected || session.fallback != "service_not_running" {
		t.Fatalf("unexpected fallback session: %#v", session)
	}
	if serviceCalls != 1 || fallbackCalls != 1 || trustCalls != 1 {
		t.Fatalf("unexpected calls: service=%d fallback=%d trust=%d", serviceCalls, fallbackCalls, trustCalls)
	}
}

func TestServiceFirstLauncherFailsClosedWhenStoppedServiceFallbackPathIsUntrusted(t *testing.T) {
	serviceCalls, fallbackCalls := 0, 0
	launcher := serviceFirstLauncher{
		service:  countingLauncher{calls: &serviceCalls, err: ErrCoreServiceNotRunning},
		fallback: countingLauncher{calls: &fallbackCalls},
		allowAutomaticFallbackPath: func(string) error {
			return errors.New("untrusted packaged path")
		},
	}
	_, err := launcher.Launch(context.Background(), "core.exe")
	if !errors.Is(err, ErrCoreServiceNotRunning) {
		t.Fatalf("expected stopped service error, got %v", err)
	}
	if serviceCalls != 1 || fallbackCalls != 0 {
		t.Fatalf("unexpected calls: service=%d fallback=%d", serviceCalls, fallbackCalls)
	}
}

func TestEnsureElevatedRetriesOneTrustedFallbackAfterServiceHandshakeFailure(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HYPOMUX_ENGINE_PATH", executable)
	serviceCalls, fallbackCalls := 0, 0
	launcher := serviceFirstLauncher{
		service: countingLauncher{
			calls: &serviceCalls, session: closedProtocolSession(coreSourceService, 41),
		},
		fallback: countingLauncher{
			calls: &fallbackCalls, session: helloProtocolSession(coreSourceRunAs, 42, ProtocolVersion),
		},
		allowAutomaticFallbackPath: func(string) error { return nil },
		allowPostHandshakeFallback: func(*coreSession, error) bool { return true },
	}
	client := newClient(stdioLauncher{}, launcher)
	defer client.Kill()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	hello, err := client.EnsureElevated(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if hello.Launcher != string(coreSourceRunAs) || !hello.Fallback {
		t.Fatalf("unexpected negotiated launch metadata: %#v", hello)
	}
	if serviceCalls != 1 || fallbackCalls != 1 {
		t.Fatalf("unexpected calls: service=%d fallback=%d", serviceCalls, fallbackCalls)
	}
	report := client.LastLaunchReport()
	if !report.Fallback || len(report.Attempts) != 4 {
		t.Fatalf("unexpected launch report: %#v", report)
	}
}

func TestEnsureElevatedDoesNotFallbackAfterLiveServicePolicyRejection(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HYPOMUX_ENGINE_PATH", executable)
	serviceCalls, fallbackCalls := 0, 0
	launcher := serviceFirstLauncher{
		service: countingLauncher{
			calls: &serviceCalls, session: closedProtocolSession(coreSourceService, 51),
		},
		fallback:                   countingLauncher{calls: &fallbackCalls},
		allowAutomaticFallbackPath: func(string) error { return nil },
		allowPostHandshakeFallback: func(*coreSession, error) bool { return false },
	}
	client := newClient(stdioLauncher{}, launcher)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err = client.EnsureElevated(ctx)
	if err == nil || !containsError(err, "聚合核心协议协商失败") {
		t.Fatalf("expected service handshake failure, got %v", err)
	}
	if serviceCalls != 1 || fallbackCalls != 0 {
		t.Fatalf("unexpected calls: service=%d fallback=%d", serviceCalls, fallbackCalls)
	}
}

func containsError(err error, text string) bool {
	return err != nil && strings.Contains(err.Error(), text)
}
