package main

import (
	"context"
	"errors"
	"io"
)

// transport connects the CLI to a running protocol-v1 engine host. Production
// uses the authenticated HypoMux Core Service pipe; tests inject pipe-backed
// fakes or a real engine server.
type transport interface {
	Connect(ctx context.Context) (io.ReadWriteCloser, error)
	// Describe names the transport in diagnostics and help output.
	Describe() string
}

var (
	errCoreServiceNotInstalled = errors.New("HypoMux Core 服务未安装；请先安装 HypoMux 桌面版，或以管理员身份运行 hypomux-engine install-service --desktop <hypomux.exe 路径> --cli <hypomux-cli.exe 路径>")
	errCoreServiceNotRunning   = errors.New("HypoMux Core 服务未运行；请启动 HypoMux 或检查服务管理器中的 HypoMuxCore 服务")
)
