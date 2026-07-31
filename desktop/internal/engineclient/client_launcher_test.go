package engineclient

import (
	"context"
	"errors"
	"os"
	"testing"
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
