//go:build windows

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestCorePipeClientAuthenticatesVerifiedDesktopHost(t *testing.T) {
	name, server := createTestPipeServer(t)
	var serverFileOwnsHandle atomic.Bool
	defer func() {
		if !serverFileOwnsHandle.Load() {
			_ = windows.CloseHandle(server)
		}
	}()
	serverResult := make(chan error, 1)
	go func() {
		if err := windows.ConnectNamedPipe(server, nil); err != nil && err != windows.ERROR_PIPE_CONNECTED {
			serverResult <- err
			return
		}
		file := os.NewFile(uintptr(server), name)
		if file == nil {
			serverResult <- fmt.Errorf("create test pipe file")
			return
		}
		serverFileOwnsHandle.Store(true)
		defer file.Close()
		line, err := bufio.NewReaderSize(file, maxPipeSessionMessage).ReadBytes('\n')
		if err != nil {
			serverResult <- err
			return
		}
		var auth pipeAuthMessage
		if err := json.Unmarshal(line, &auth); err != nil {
			serverResult <- err
			return
		}
		if auth.Kind != pipeSessionAuthKind || auth.Protocol != pipeSessionProtocol {
			serverResult <- fmt.Errorf("unexpected auth message: %+v", auth)
			return
		}
		ready, err := json.Marshal(pipeReadyMessage{
			Protocol: pipeSessionProtocol,
			Kind:     pipeSessionReadyKind,
			OK:       true,
		})
		if err != nil {
			serverResult <- err
			return
		}
		payload := append(ready, '\n')
		payload = append(payload, []byte("protocol-request\n")...)
		_, err = file.Write(payload)
		serverResult <- err
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	verifier := func(serverPID uint32) error {
		if serverPID != uint32(os.Getpid()) {
			return fmt.Errorf("unexpected test server PID %d", serverPID)
		}
		return nil
	}
	connection, err := connectAuthenticatedPipe(ctx, name, verifier)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if err := <-serverResult; err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(connection).ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	if string(line) != "protocol-request\n" {
		t.Fatalf("buffered protocol request = %q", line)
	}
}

func TestCorePipeClientRejectsUnverifiedDesktopHost(t *testing.T) {
	name, server := createTestPipeServer(t)
	defer windows.CloseHandle(server)
	connected := make(chan error, 1)
	go func() {
		err := windows.ConnectNamedPipe(server, nil)
		// The rejected client may disconnect before ConnectNamedPipe returns.
		// ERROR_NO_DATA confirms that the client reached the pipe and closed it.
		if err == windows.ERROR_PIPE_CONNECTED || err == windows.ERROR_NO_DATA {
			err = nil
		}
		connected <- err
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	verifier := func(serverPID uint32) error { return errors.New("unverified host") }
	connection, err := connectAuthenticatedPipe(ctx, name, verifier)
	if connection != nil {
		_ = connection.Close()
	}
	if err == nil {
		t.Fatal("unverified desktop host was accepted")
	}
	if connectErr := <-connected; connectErr != nil {
		t.Fatal(connectErr)
	}
}

func TestDesktopHostVerificationAcceptsSiblingExecutable(t *testing.T) {
	// The development layout resolves hypomux.exe next to the engine binary.
	// The test binary plays the engine role, so the hypomux.exe placed in the
	// same directory must satisfy the image verifier.
	sibling := filepath.Join(filepath.Dir(mustTestExecutable(t)), "hypomux.exe")
	mustWriteTestFile(t, sibling, "test desktop executable")
	t.Cleanup(func() { _ = os.Remove(sibling) })
	canonical, err := canonicalRegularFile(sibling, "test desktop executable")
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyDesktopHostImage(canonical, uint32(os.Getpid())); err != nil {
		t.Fatalf("sibling desktop executable was rejected: %v", err)
	}
}

func TestDesktopHostVerificationRejectsForeignImage(t *testing.T) {
	engine := mustTestExecutable(t)
	canonical, err := canonicalRegularFile(engine, "test engine executable")
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyDesktopHostImage(canonical, uint32(os.Getpid())); err == nil {
		t.Fatal("a process image that is not the pinned desktop executable was accepted")
	}
}

func TestDesktopHostProcessVerificationRejectsForeignProcess(t *testing.T) {
	// End to end: the test binary is neither the policy-pinned desktop
	// executable nor a hypomux.exe next to itself, so the verifier must
	// reject it whatever the registry state is.
	if err := verifyDesktopHostProcess(uint32(os.Getpid())); err == nil {
		t.Fatal("a process that is not the pinned desktop executable was accepted")
	}
}

func mustTestExecutable(t *testing.T) string {
	t.Helper()
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func mustWriteTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func createTestPipeServer(t *testing.T) (string, windows.Handle) {
	t.Helper()
	name := fmt.Sprintf(`\\.\pipe\HypoMux-Core-Test-%d-%d`, os.Getpid(), time.Now().UnixNano())
	namePointer, err := windows.UTF16PtrFromString(name)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateNamedPipe(
		namePointer,
		windows.PIPE_ACCESS_DUPLEX|windows.FILE_FLAG_FIRST_PIPE_INSTANCE,
		windows.PIPE_TYPE_BYTE|windows.PIPE_READMODE_BYTE|windows.PIPE_WAIT|windows.PIPE_REJECT_REMOTE_CLIENTS,
		1,
		4096,
		4096,
		0,
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	return name, handle
}
