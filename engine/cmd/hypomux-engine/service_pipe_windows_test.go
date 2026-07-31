//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

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
