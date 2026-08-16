package engineclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"sync"
)

var (
	ErrCoreServiceUnavailable = errors.New("HypoMux Core Service 未安装")
	ErrCoreServiceNotRunning  = errors.New("HypoMux Core Service 已安装但未运行")
)

type coreSessionSource string

const (
	coreSourceUnknown coreSessionSource = "unknown"
	coreSourceStdio   coreSessionSource = "stdio"
	coreSourceService coreSessionSource = "service"
	coreSourceRunAs   coreSessionSource = "runas"
)

type coreProcess interface {
	Wait() error
	Kill() error
	PID() int
}

type coreSession struct {
	reader   io.Reader
	writer   io.WriteCloser
	close    func() error
	process  coreProcess
	path     string
	source   coreSessionSource
	fallback string
	details  launchSessionDetails
	closeMu  sync.Once
	closeErr error
}

type launchSessionDetails struct {
	ServicePID     int
	DesktopSession *uint32
	ConsoleSession *uint32
}

type coreLauncher interface {
	Launch(context.Context, string) (*coreSession, error)
}

type stdioLauncher struct{}

type serviceFirstLauncher struct {
	service                    coreLauncher
	fallback                   coreLauncher
	allowAutomaticFallbackPath func(string) error
	allowPostHandshakeFallback func(*coreSession, error) bool
}

type postHandshakeFallbackLauncher interface {
	FallbackAfterHandshake(context.Context, string, *coreSession, error) (*coreSession, bool, error)
}

type execCoreProcess struct {
	command *exec.Cmd
}

func (launcher serviceFirstLauncher) Launch(ctx context.Context, path string) (*coreSession, error) {
	session, err := launcher.service.Launch(ctx, path)
	if err == nil {
		return session, nil
	}
	if !errors.Is(err, ErrCoreServiceUnavailable) && !errors.Is(err, ErrCoreServiceNotRunning) {
		return nil, err
	}
	if errors.Is(err, ErrCoreServiceNotRunning) && launcher.allowAutomaticFallbackPath != nil {
		if trustErr := launcher.allowAutomaticFallbackPath(path); trustErr != nil {
			return nil, fmt.Errorf("%w；自动兼容启动已取消：%v", err, trustErr)
		}
	}
	fallbackSession, fallbackErr := launcher.fallback.Launch(ctx, path)
	if fallbackErr != nil {
		return nil, fallbackErr
	}
	if fallbackSession != nil {
		if errors.Is(err, ErrCoreServiceUnavailable) {
			fallbackSession.fallback = "service_unavailable"
		} else {
			fallbackSession.fallback = "service_not_running"
		}
	}
	return fallbackSession, nil
}

func (launcher serviceFirstLauncher) FallbackAfterHandshake(
	ctx context.Context,
	path string,
	failed *coreSession,
	cause error,
) (*coreSession, bool, error) {
	if failed == nil || failed.source != coreSourceService ||
		launcher.allowPostHandshakeFallback == nil ||
		!launcher.allowPostHandshakeFallback(failed, cause) {
		return nil, false, nil
	}
	if launcher.allowAutomaticFallbackPath == nil {
		return nil, true, errors.New("自动兼容启动缺少正式 Core 路径校验")
	}
	if err := launcher.allowAutomaticFallbackPath(path); err != nil {
		return nil, true, fmt.Errorf("自动兼容启动已取消：%w", err)
	}
	session, err := launcher.fallback.Launch(ctx, path)
	if err != nil {
		return nil, true, err
	}
	if session != nil {
		session.fallback = "service_handshake_failed"
	}
	return session, true, nil
}

func (stdioLauncher) Launch(_ context.Context, path string) (*coreSession, error) {
	command := exec.Command(path)
	configureCommand(command)
	command.Dir = filepath.Dir(path)

	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("连接聚合核心输入失败：%w", err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("连接聚合核心输出失败：%w", err)
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("连接聚合核心错误输出失败：%w", err)
	}
	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("启动 hypomux-engine.exe 失败：%w", err)
	}
	go func() {
		_, _ = io.Copy(io.Discard, stderr)
	}()
	return &coreSession{
		reader:  stdout,
		writer:  stdin,
		close:   stdin.Close,
		process: &execCoreProcess{command: command},
		path:    path,
		source:  coreSourceStdio,
	}, nil
}

func (p *execCoreProcess) Wait() error {
	return p.command.Wait()
}

func (p *execCoreProcess) Kill() error {
	if p.command.Process == nil {
		return nil
	}
	return p.command.Process.Kill()
}

func (p *execCoreProcess) PID() int {
	if p.command.Process == nil {
		return 0
	}
	return p.command.Process.Pid
}
