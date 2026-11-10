//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"
	"unsafe"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

const (
	coreServiceName     = "HypoMuxCore"
	coreServicePipeName = `\\.\pipe\HypoMux-Core-Service`
	// A stopped-but-installed service and a busy single-instance pipe are both
	// worth a short retry before reporting failure to the caller.
	servicePipeConnectTimeout = 2 * time.Second
)

// serviceTransport connects to the privileged Core Service the same way the
// desktop does: the service authenticates this exact CLI executable by its
// pinned install path and SHA-256 digest before the first protocol byte.
type serviceTransport struct {
	serviceName string
	pipeName    string
}

func newServiceTransport() transport {
	return serviceTransport{serviceName: coreServiceName, pipeName: coreServicePipeName}
}

func (t serviceTransport) Describe() string {
	return t.pipeName
}

func (t serviceTransport) Connect(ctx context.Context) (io.ReadWriteCloser, error) {
	pid, installed, err := queryCoreService(t.serviceName)
	if err != nil {
		return nil, err
	}
	if !installed {
		return nil, errCoreServiceNotInstalled
	}
	if pid == 0 {
		return nil, errCoreServiceNotRunning
	}
	return connectServicePipe(ctx, t.pipeName, pid)
}

func queryCoreService(serviceName string) (pid int, installed bool, err error) {
	manager, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return 0, false, fmt.Errorf("连接 Windows 服务管理器失败：%w", err)
	}
	defer windows.CloseServiceHandle(manager)

	name, err := windows.UTF16PtrFromString(serviceName)
	if err != nil {
		return 0, false, err
	}
	service, err := windows.OpenService(manager, name, windows.SERVICE_QUERY_STATUS)
	if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("查询 HypoMux Core Service 失败：%w", err)
	}
	defer windows.CloseServiceHandle(service)

	var status windows.SERVICE_STATUS_PROCESS
	var needed uint32
	err = windows.QueryServiceStatusEx(
		service,
		windows.SC_STATUS_PROCESS_INFO,
		(*byte)(unsafe.Pointer(&status)),
		uint32(unsafe.Sizeof(status)),
		&needed,
	)
	if err != nil {
		return 0, true, fmt.Errorf("读取 HypoMux Core Service 状态失败：%w", err)
	}
	if status.CurrentState != windows.SERVICE_RUNNING || status.ProcessId == 0 {
		return 0, true, nil
	}
	return int(status.ProcessId), true, nil
}

func connectServicePipe(ctx context.Context, pipeName string, expectedPID int) (io.ReadWriteCloser, error) {
	name, err := windows.UTF16PtrFromString(pipeName)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(servicePipeConnectTimeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	var handle windows.Handle
	for {
		handle, err = windows.CreateFile(
			name,
			windows.GENERIC_READ|windows.GENERIC_WRITE,
			0,
			nil,
			windows.OPEN_EXISTING,
			windows.FILE_FLAG_OVERLAPPED,
			0,
		)
		if err == nil {
			break
		}
		if !errors.Is(err, windows.ERROR_PIPE_BUSY) && !errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
			return nil, fmt.Errorf("连接 HypoMux Core Service 管道失败：%w", err)
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("连接 HypoMux Core Service 管道超时：%w", err)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(40 * time.Millisecond):
		}
	}

	// A stale pipe squatting on the well-known name is not the Core Service.
	// The server side re-validates this CLI executable's fingerprint, but the
	// PID check here keeps the error message honest when something else answers.
	var serverPID uint32
	if err := windows.GetNamedPipeServerProcessId(handle, &serverPID); err != nil {
		_ = windows.CloseHandle(handle)
		return nil, fmt.Errorf("读取 Core Service 管道身份失败：%w", err)
	}
	if expectedPID <= 0 || serverPID != uint32(expectedPID) {
		_ = windows.CloseHandle(handle)
		return nil, fmt.Errorf("拒绝非预期 Core Service 管道（PID %d）", serverPID)
	}
	file, err := winio.NewOpenFile(handle)
	if err != nil {
		_ = windows.CloseHandle(handle)
		return nil, fmt.Errorf("接管 Core Service 管道失败：%w", err)
	}
	return file, nil
}
