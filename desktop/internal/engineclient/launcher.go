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

var ErrCoreServiceUnavailable = errors.New("HypoMux Core Service 未安装")

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
	closeMu  sync.Once
	closeErr error
}

type coreLauncher interface {
	Launch(context.Context, string) (*coreSession, error)
}

type stdioLauncher struct{}

type serviceFirstLauncher struct {
	service  coreLauncher
	fallback coreLauncher
}

type execCoreProcess struct {
	command *exec.Cmd
}

func (launcher serviceFirstLauncher) Launch(ctx context.Context, path string) (*coreSession, error) {
	session, err := launcher.service.Launch(ctx, path)
	if err == nil {
		return session, nil
	}
	if !errors.Is(err, ErrCoreServiceUnavailable) {
		return nil, err
	}
	return launcher.fallback.Launch(ctx, path)
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
