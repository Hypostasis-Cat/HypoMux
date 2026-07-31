//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"unsafe"

	"github.com/Hypostasis-Cat/HypoMux/engine/internal/server"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
)

const (
	coreServiceName     = "HypoMuxCore"
	coreServicePipeName = `\\.\pipe\HypoMux-Core-Service`
	servicePipeBuffer   = 64 * 1024
)

var errServiceClientRejected = errors.New("Core Service client rejected")

type coreWindowsService struct {
	metadata server.Metadata
	stderr   io.Writer
}

func runWindowsService(stderr io.Writer, metadata server.Metadata) int {
	isService, err := svc.IsWindowsService()
	if err != nil {
		fmt.Fprintf(stderr, "detect Windows Service context: %v\n", err)
		return 1
	}
	if !isService {
		fmt.Fprintln(stderr, "service mode must be started by Windows Service Control Manager")
		return 2
	}
	if err := svc.Run(coreServiceName, &coreWindowsService{metadata: metadata, stderr: stderr}); err != nil {
		fmt.Fprintf(stderr, "run %s: %v\n", coreServiceName, err)
		return 1
	}
	return 0
}

func (service *coreWindowsService) Execute(
	_ []string,
	requests <-chan svc.ChangeRequest,
	status chan<- svc.Status,
) (bool, uint32) {
	const accepted = svc.AcceptStop | svc.AcceptShutdown
	status <- svc.Status{State: svc.StartPending}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- serveCoreServicePipe(ctx, service.metadata)
	}()
	status <- svc.Status{State: svc.Running, Accepts: accepted}

	for {
		select {
		case request, ok := <-requests:
			if !ok {
				status <- svc.Status{State: svc.StopPending}
				cancel()
				err := <-done
				if err != nil && !errors.Is(err, context.Canceled) {
					return false, 1
				}
				return false, 0
			}
			switch request.Cmd {
			case svc.Interrogate:
				status <- request.CurrentStatus
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending}
				cancel()
				err := <-done
				if err != nil && !errors.Is(err, context.Canceled) {
					fmt.Fprintf(service.stderr, "stop service pipe: %v\n", err)
					return false, 1
				}
				return false, 0
			}
		case err := <-done:
			cancel()
			if err != nil && !errors.Is(err, context.Canceled) {
				fmt.Fprintf(service.stderr, "service pipe failed: %v\n", err)
				return false, 1
			}
			return false, 0
		}
	}
}

func serveCoreServicePipe(ctx context.Context, metadata server.Metadata) error {
	for {
		connection, err := acceptServicePipe(ctx)
		if err != nil {
			if errors.Is(err, errServiceClientRejected) {
				fmt.Fprintf(os.Stderr, "%v\n", err)
				continue
			}
			return err
		}
		engineServer := server.New(connection, connection, metadata)
		sessionDone := make(chan error, 1)
		go func() {
			sessionDone <- engineServer.Run(ctx)
		}()
		var runErr error
		select {
		case runErr = <-sessionDone:
			_ = connection.Close()
		case <-ctx.Done():
			// Closing the pipe interrupts a client that is idle in Read so the
			// SCM stop deadline is not held hostage by an attached UI.
			_ = connection.Close()
			runErr = <-sessionDone
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if runErr != nil && !errors.Is(runErr, io.EOF) {
			// A malformed or abruptly disconnected client must not terminate the
			// Windows Service. The next authenticated local UI may reconnect.
			fmt.Fprintf(os.Stderr, "Core Service client session ended: %v\n", runErr)
		}
	}
}

func acceptServicePipe(ctx context.Context) (*os.File, error) {
	handle, err := createServicePipe()
	if err != nil {
		return nil, err
	}
	var closeOnce sync.Once
	closeHandle := func() {
		closeOnce.Do(func() {
			_ = windows.CloseHandle(handle)
		})
	}
	connected := make(chan error, 1)
	go func() {
		connectErr := windows.ConnectNamedPipe(handle, nil)
		if errors.Is(connectErr, windows.ERROR_PIPE_CONNECTED) {
			connectErr = nil
		}
		connected <- connectErr
	}()
	select {
	case err := <-connected:
		if err != nil {
			closeHandle()
			return nil, err
		}
	case <-ctx.Done():
		closeHandle()
		<-connected
		return nil, ctx.Err()
	}
	if err := validateServicePipeClient(handle); err != nil {
		_ = windows.DisconnectNamedPipe(handle)
		closeHandle()
		return nil, fmt.Errorf("%w: %v", errServiceClientRejected, err)
	}
	connection := os.NewFile(uintptr(handle), coreServicePipeName)
	if connection == nil {
		closeHandle()
		return nil, errors.New("create Core Service pipe file")
	}
	// os.File now owns the handle.
	closeOnce.Do(func() {})
	return connection, nil
}

func createServicePipe() (windows.Handle, error) {
	descriptor, err := windows.SecurityDescriptorFromString(
		"D:P(A;;GA;;;SY)(A;;GA;;;BA)(A;;GRGW;;;AU)",
	)
	if err != nil {
		return 0, fmt.Errorf("build Core Service pipe ACL: %w", err)
	}
	attributes := windows.SecurityAttributes{
		Length:             uint32(unsafe.Sizeof(windows.SecurityAttributes{})),
		SecurityDescriptor: descriptor,
	}
	name, err := windows.UTF16PtrFromString(coreServicePipeName)
	if err != nil {
		return 0, err
	}
	handle, err := windows.CreateNamedPipe(
		name,
		windows.PIPE_ACCESS_DUPLEX|windows.FILE_FLAG_FIRST_PIPE_INSTANCE,
		windows.PIPE_TYPE_BYTE|windows.PIPE_READMODE_BYTE|windows.PIPE_WAIT|windows.PIPE_REJECT_REMOTE_CLIENTS,
		1,
		servicePipeBuffer,
		servicePipeBuffer,
		0,
		&attributes,
	)
	if err != nil {
		return 0, fmt.Errorf("create Core Service pipe: %w", err)
	}
	return handle, nil
}

func validateServicePipeClient(handle windows.Handle) error {
	var clientPID uint32
	if err := windows.GetNamedPipeClientProcessId(handle, &clientPID); err != nil {
		return fmt.Errorf("read Core Service client PID: %w", err)
	}
	if clientPID == 0 {
		return errors.New("reject Core Service client without process identity")
	}
	var clientSession uint32
	if err := windows.ProcessIdToSessionId(clientPID, &clientSession); err != nil {
		return fmt.Errorf("read Core Service client session: %w", err)
	}
	activeSession := windows.WTSGetActiveConsoleSessionId()
	if activeSession == ^uint32(0) || clientSession != activeSession {
		return fmt.Errorf(
			"reject Core Service client outside active console session (PID %d, session %d)",
			clientPID,
			clientSession,
		)
	}
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, clientPID)
	if err != nil {
		return fmt.Errorf("open Core Service client process: %w", err)
	}
	defer windows.CloseHandle(process)
	var token windows.Token
	if err := windows.OpenProcessToken(process, windows.TOKEN_QUERY, &token); err != nil {
		return fmt.Errorf("open Core Service client token: %w", err)
	}
	defer token.Close()
	if token.IsElevated() {
		return fmt.Errorf("reject elevated Core Service UI client (PID %d)", clientPID)
	}
	return nil
}
