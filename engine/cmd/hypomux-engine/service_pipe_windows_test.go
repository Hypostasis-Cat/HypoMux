//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Hypostasis-Cat/HypoMux/engine/internal/server"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
)

func TestCoreServiceStopIsBoundedWhenPipeShutdownStalls(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	host := &coreWindowsService{
		stderr:          io.Discard,
		shutdownTimeout: 50 * time.Millisecond,
		serve: func(ctx context.Context, _ server.Metadata) error {
			close(started)
			<-release
			return ctx.Err()
		},
	}
	defer close(release)

	requests := make(chan svc.ChangeRequest, 1)
	statuses := make(chan svc.Status, 3)
	result := make(chan struct {
		serviceSpecific bool
		exitCode        uint32
	}, 1)
	go func() {
		serviceSpecific, exitCode := host.Execute(nil, requests, statuses)
		result <- struct {
			serviceSpecific bool
			exitCode        uint32
		}{serviceSpecific: serviceSpecific, exitCode: exitCode}
	}()

	<-started
	if state := (<-statuses).State; state != svc.StartPending {
		t.Fatalf("initial state = %v, want StartPending", state)
	}
	if state := (<-statuses).State; state != svc.Running {
		t.Fatalf("ready state = %v, want Running", state)
	}
	requests <- svc.ChangeRequest{Cmd: svc.Stop}
	if state := (<-statuses).State; state != svc.StopPending {
		t.Fatalf("stopping state = %v, want StopPending", state)
	}

	select {
	case stopped := <-result:
		if stopped.serviceSpecific || stopped.exitCode != 0 {
			t.Fatalf("stop result = service-specific %t, exit code %d", stopped.serviceSpecific, stopped.exitCode)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("service remained blocked after its shutdown deadline")
	}
}

func TestConnectServicePipeCancellationDoesNotBlockServiceStop(t *testing.T) {
	pipeName := fmt.Sprintf(`\\.\pipe\HypoMux-Core-Cancel-Test-%d-%d`, os.Getpid(), time.Now().UnixNano())
	handle, err := createServicePipeNamed(pipeName)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	err = connectServicePipe(ctx, handle)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline cancellation, got %v", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("cancelled pipe accept blocked service stop for %s", elapsed)
	}
}

func TestConnectServicePipeAcceptsClient(t *testing.T) {
	pipeName := fmt.Sprintf(`\\.\pipe\HypoMux-Core-Connect-Test-%d-%d`, os.Getpid(), time.Now().UnixNano())
	handle, err := createServicePipeNamed(pipeName)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	connected := make(chan error, 1)
	go func() {
		connected <- connectServicePipe(ctx, handle)
	}()
	name, err := windows.UTF16PtrFromString(pipeName)
	if err != nil {
		t.Fatal(err)
	}
	client, err := windows.CreateFile(
		name,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		0,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_OVERLAPPED,
		0,
	)
	if err != nil {
		if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			t.Skip("test token is not an interactive user allowed by the production pipe ACL")
		}
		t.Fatal(err)
	}
	defer windows.CloseHandle(client)
	select {
	case err := <-connected:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("overlapped Core Service pipe did not accept a client")
	}
}

func TestCoreServicePipeACLExcludesGenericAuthenticatedUsers(t *testing.T) {
	if strings.Contains(coreServicePipeSDDL, ";;;AU)") {
		t.Fatalf("Core Service pipe grants access to every authenticated user: %s", coreServicePipeSDDL)
	}
	if !strings.Contains(coreServicePipeSDDL, ";;;IU)") {
		t.Fatalf("Core Service pipe does not grant the interactive UI access: %s", coreServicePipeSDDL)
	}
}
