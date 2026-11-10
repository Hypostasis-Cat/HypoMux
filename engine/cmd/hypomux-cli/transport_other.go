//go:build !windows

package main

import (
	"context"
	"errors"
	"io"
)

// The Core Service pipe is a Windows named pipe. HypoMux itself only supports
// Windows, so non-Windows builds exist to keep `go vet ./...` and editor
// tooling honest rather than to provide a usable CLI.
type unsupportedTransport struct{}

func (unsupportedTransport) Connect(context.Context) (io.ReadWriteCloser, error) {
	return nil, errors.New("HypoMux CLI 仅支持 Windows")
}

func (unsupportedTransport) Describe() string {
	return "unsupported"
}

func newServiceTransport() transport {
	return unsupportedTransport{}
}
