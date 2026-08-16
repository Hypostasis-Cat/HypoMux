//go:build windows

package engineclient

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	CoreServiceName     = "HypoMuxCore"
	CoreServicePipeName = `\\.\pipe\HypoMux-Core-Service`
)

type windowsServiceLauncher struct{}

type serviceCoreProcess struct {
	pid  int
	done chan struct{}
	once sync.Once
}

func (windowsServiceLauncher) Launch(ctx context.Context, _ string) (*coreSession, error) {
	pid, installed, err := queryCoreService()
	if err != nil {
		return nil, err
	}
	if !installed {
		return nil, ErrCoreServiceUnavailable
	}
	if pid == 0 {
		return nil, ErrCoreServiceNotRunning
	}
	connectCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	connection, err := connectCoreServicePipe(connectCtx, pid)
	if err != nil {
		return nil, err
	}
	process := &serviceCoreProcess{pid: pid, done: make(chan struct{})}
	details := launchSessionDetails{ServicePID: pid}
	var desktopSession uint32
	if windows.ProcessIdToSessionId(uint32(os.Getpid()), &desktopSession) == nil {
		details.DesktopSession = &desktopSession
	}
	consoleSession := windows.WTSGetActiveConsoleSessionId()
	if consoleSession != ^uint32(0) {
		details.ConsoleSession = &consoleSession
	}
	return &coreSession{
		reader: connection,
		writer: connection,
		close: func() error {
			closeErr := connection.Close()
			process.signalClosed()
			return closeErr
		},
		process: process,
		path:    CoreServicePipeName,
		source:  coreSourceService,
		details: details,
	}, nil
}

func allowAutomaticCoreFallbackPath(path string) error {
	desktopPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("读取 HypoMux 安装路径失败：%w", err)
	}
	expected := filepath.Join(filepath.Dir(desktopPath), "bin", "hypomux-engine.exe")
	expectedInfo, err := os.Stat(expected)
	if err != nil {
		return fmt.Errorf("正式安装目录中的 Core 不可用：%w", err)
	}
	actualInfo, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("待启动 Core 不可用：%w", err)
	}
	if !expectedInfo.Mode().IsRegular() || !actualInfo.Mode().IsRegular() || !os.SameFile(expectedInfo, actualInfo) {
		return errors.New("待启动 Core 不是当前 HypoMux 安装目录中的 bin\\hypomux-engine.exe")
	}
	return nil
}

func allowServicePostHandshakeFallback(session *coreSession, cause error) bool {
	if errors.Is(cause, ErrCoreProtocolIncompatible) {
		return true
	}
	if session == nil || session.process == nil {
		return false
	}
	pid, installed, err := queryCoreService()
	if err != nil {
		return false
	}
	// A rejection from the same live service can be an intentional path/hash
	// policy decision and must fail closed. Retry only after the service has
	// actually disappeared, stopped, or restarted under a different PID.
	return !installed || pid == 0 || pid != session.process.PID()
}

func queryCoreService() (pid int, installed bool, err error) {
	manager, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return 0, false, fmt.Errorf("连接 Windows 服务管理器失败：%w", err)
	}
	defer windows.CloseServiceHandle(manager)

	name, err := windows.UTF16PtrFromString(CoreServiceName)
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

func connectCoreServicePipe(ctx context.Context, expectedPID int) (*os.File, error) {
	name, err := windows.UTF16PtrFromString(CoreServicePipeName)
	if err != nil {
		return nil, err
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
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(40 * time.Millisecond):
		}
	}

	var serverPID uint32
	if err := windows.GetNamedPipeServerProcessId(handle, &serverPID); err != nil {
		_ = windows.CloseHandle(handle)
		return nil, fmt.Errorf("读取 Core Service 管道身份失败：%w", err)
	}
	if expectedPID <= 0 || serverPID != uint32(expectedPID) {
		_ = windows.CloseHandle(handle)
		return nil, fmt.Errorf("拒绝非预期 Core Service 管道（PID %d）", serverPID)
	}
	connection := os.NewFile(uintptr(handle), CoreServicePipeName)
	if connection == nil {
		_ = windows.CloseHandle(handle)
		return nil, errors.New("创建 Core Service 管道句柄失败")
	}
	return connection, nil
}

func (p *serviceCoreProcess) Wait() error {
	<-p.done
	return nil
}

func (p *serviceCoreProcess) Kill() error {
	p.signalClosed()
	return nil
}

func (p *serviceCoreProcess) PID() int {
	return p.pid
}

func (p *serviceCoreProcess) signalClosed() {
	p.once.Do(func() {
		close(p.done)
	})
}
