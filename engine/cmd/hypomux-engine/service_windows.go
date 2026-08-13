//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"

	"github.com/Hypostasis-Cat/HypoMux/engine/internal/server"
	"github.com/Hypostasis-Cat/HypoMux/engine/internal/tun"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const (
	coreServiceName     = "HypoMuxCore"
	coreServicePipeName = `\\.\pipe\HypoMux-Core-Service`
	servicePipeBuffer   = 64 * 1024
	serviceDisplayName  = "HypoMux Core Service"
	serviceDescription  = "Privileged TUN, WFP, route, DNS and network recovery host for HypoMux."
	coreServicePipeSDDL = "D:P(A;;GA;;;SY)(A;;GA;;;BA)(A;;GRGW;;;IU)"
)

var errServiceClientRejected = errors.New("Core Service client rejected")

func installWindowsService() error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return fmt.Errorf("resolve absolute executable path: %w", err)
	}
	if err := requireMachineInstallLocation(executable); err != nil {
		return err
	}
	policy, err := buildCoreServicePolicy(executable)
	if err != nil {
		return fmt.Errorf("build Core Service security policy: %w", err)
	}
	if err := tun.PrepareTrustedConfigStorage(); err != nil {
		return fmt.Errorf("prepare Core Service config storage: %w", err)
	}

	manager, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to Service Control Manager: %w", err)
	}
	defer manager.Disconnect()

	service, err := manager.OpenService(coreServiceName)
	if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
		service, err = manager.CreateService(coreServiceName, executable, mgr.Config{
			StartType:    mgr.StartAutomatic,
			ErrorControl: mgr.ErrorNormal,
			DisplayName:  serviceDisplayName,
			Description:  serviceDescription,
		}, "service")
		if err != nil {
			return fmt.Errorf("create service: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("open service: %w", err)
	} else {
		// An in-place update must never leave the previous (possibly vulnerable)
		// service process running against newly installed files or policy.
		if stopErr := stopWindowsService(service, 20*time.Second); stopErr != nil {
			service.Close()
			return fmt.Errorf("stop existing service before update: %w", stopErr)
		}
		config, configErr := service.Config()
		if configErr != nil {
			service.Close()
			return fmt.Errorf("read service configuration: %w", configErr)
		}
		config.BinaryPathName = syscall.EscapeArg(executable) + " " + syscall.EscapeArg("service")
		config.StartType = mgr.StartAutomatic
		config.ErrorControl = mgr.ErrorNormal
		config.DisplayName = serviceDisplayName
		config.Description = serviceDescription
		if configErr = service.UpdateConfig(config); configErr != nil {
			service.Close()
			return fmt.Errorf("update service configuration: %w", configErr)
		}
	}
	defer service.Close()

	if err := writeCoreServicePolicy(policy); err != nil {
		return fmt.Errorf("persist Core Service security policy: %w", err)
	}
	if err := service.SetRecoveryActions([]mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 3 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 10 * time.Second},
		{Type: mgr.NoAction},
	}, 24*60*60); err != nil {
		return fmt.Errorf("set service recovery actions: %w", err)
	}
	if err := service.Start(); err != nil && !errors.Is(err, windows.ERROR_SERVICE_ALREADY_RUNNING) {
		return fmt.Errorf("start service: %w", err)
	}
	return nil
}

func removeWindowsService() error {
	manager, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to Service Control Manager: %w", err)
	}
	defer manager.Disconnect()

	service, err := manager.OpenService(coreServiceName)
	if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
		return deleteCoreServicePolicy()
	}
	if err != nil {
		return fmt.Errorf("open service: %w", err)
	}
	defer service.Close()

	if err := stopWindowsService(service, 20*time.Second); err != nil {
		return err
	}
	if err := service.Delete(); err != nil && !errors.Is(err, windows.ERROR_SERVICE_MARKED_FOR_DELETE) {
		return fmt.Errorf("delete service: %w", err)
	}
	return deleteCoreServicePolicy()
}

func stopWindowsService(service *mgr.Service, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	stopRequested := false
	for {
		status, err := service.Query()
		if errors.Is(err, windows.ERROR_SERVICE_NOT_ACTIVE) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("query service status: %w", err)
		}
		if status.State == svc.Stopped {
			return nil
		}
		if !stopRequested && status.State != svc.StopPending {
			_, err = service.Control(svc.Stop)
			if errors.Is(err, windows.ERROR_SERVICE_NOT_ACTIVE) {
				return nil
			}
			if err != nil && !errors.Is(err, windows.ERROR_SERVICE_CANNOT_ACCEPT_CTRL) {
				return fmt.Errorf("stop service: %w", err)
			}
			stopRequested = err == nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("stop service: timed out after %s", timeout)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

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
		policy, err := loadCoreServicePolicy()
		if err != nil {
			return fmt.Errorf("load Core Service security policy: %w", err)
		}
		connection, err := acceptServicePipe(ctx, policy)
		if err != nil {
			if errors.Is(err, errServiceClientRejected) {
				fmt.Fprintf(os.Stderr, "%v\n", err)
				continue
			}
			return err
		}
		sessionMetadata := metadata
		sessionMetadata.TunExecutable = policy.TunExecutable
		sessionMetadata.TunExecutableSHA256 = policy.TunExecutableSHA256
		engineServer := server.New(connection, connection, sessionMetadata)
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

func acceptServicePipe(ctx context.Context, policy coreServicePolicy) (*os.File, error) {
	handle, err := createServicePipe()
	if err != nil {
		return nil, err
	}
	if err := connectServicePipe(ctx, handle); err != nil {
		_ = windows.CloseHandle(handle)
		return nil, err
	}
	if err := validateServicePipeClient(handle, policy); err != nil {
		_ = windows.DisconnectNamedPipe(handle)
		_ = windows.CloseHandle(handle)
		return nil, fmt.Errorf("%w: %v", errServiceClientRejected, err)
	}
	connection := os.NewFile(uintptr(handle), coreServicePipeName)
	if connection == nil {
		_ = windows.CloseHandle(handle)
		return nil, errors.New("create Core Service pipe file")
	}
	return connection, nil
}

func connectServicePipe(ctx context.Context, handle windows.Handle) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	event, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return fmt.Errorf("create Core Service pipe event: %w", err)
	}
	defer windows.CloseHandle(event)
	overlapped := windows.Overlapped{HEvent: event}
	err = windows.ConnectNamedPipe(handle, &overlapped)
	if err == nil || errors.Is(err, windows.ERROR_PIPE_CONNECTED) {
		return nil
	}
	if !errors.Is(err, windows.ERROR_IO_PENDING) {
		return fmt.Errorf("connect Core Service pipe: %w", err)
	}
	for {
		result, waitErr := windows.WaitForSingleObject(event, 50)
		if waitErr != nil {
			return fmt.Errorf("wait for Core Service pipe client: %w", waitErr)
		}
		switch result {
		case windows.WAIT_OBJECT_0:
			var transferred uint32
			if err := windows.GetOverlappedResult(handle, &overlapped, &transferred, false); err != nil {
				return fmt.Errorf("complete Core Service pipe connection: %w", err)
			}
			return nil
		case uint32(windows.WAIT_TIMEOUT):
			if ctx.Err() == nil {
				continue
			}
			_ = windows.CancelIoEx(handle, &overlapped)
			var transferred uint32
			_ = windows.GetOverlappedResult(handle, &overlapped, &transferred, true)
			return ctx.Err()
		default:
			return fmt.Errorf("wait for Core Service pipe client returned 0x%X", result)
		}
	}
}

func createServicePipe() (windows.Handle, error) {
	return createServicePipeNamed(coreServicePipeName)
}

func createServicePipeNamed(pipeName string) (windows.Handle, error) {
	return createServicePipeNamedWithSDDL(pipeName, coreServicePipeSDDL)
}

func createServicePipeNamedWithSDDL(pipeName string, sddl string) (windows.Handle, error) {
	descriptor, err := windows.SecurityDescriptorFromString(
		sddl,
	)
	if err != nil {
		return 0, fmt.Errorf("build Core Service pipe ACL: %w", err)
	}
	attributes := windows.SecurityAttributes{
		Length:             uint32(unsafe.Sizeof(windows.SecurityAttributes{})),
		SecurityDescriptor: descriptor,
	}
	name, err := windows.UTF16PtrFromString(pipeName)
	if err != nil {
		return 0, err
	}
	handle, err := windows.CreateNamedPipe(
		name,
		windows.PIPE_ACCESS_DUPLEX|windows.FILE_FLAG_FIRST_PIPE_INSTANCE|windows.FILE_FLAG_OVERLAPPED,
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

func validateServicePipeClient(handle windows.Handle, policy coreServicePolicy) error {
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
	if err := validateServiceClientExecutable(clientPID, policy); err != nil {
		return err
	}
	return nil
}
