package engineclient

import (
	"context"
	"errors"
	"os"
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
