//go:build windows

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestCorePipeClientAuthenticatesExpectedDesktopHost(t *testing.T) {
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
		if auth.Token != "one-time-test-token" || auth.Kind != pipeSessionAuthKind {
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
	connection, err := connectAuthenticatedPipe(ctx, name, "one-time-test-token", os.Getpid())
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

func TestCorePipeClientRejectsUnexpectedDesktopHostPID(t *testing.T) {
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
	connection, err := connectAuthenticatedPipe(ctx, name, "unused", os.Getpid()+1)
	if connection != nil {
		_ = connection.Close()
	}
	if err == nil {
		t.Fatal("unexpected desktop host PID was accepted")
	}
	if connectErr := <-connected; connectErr != nil {
		t.Fatal(connectErr)
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
