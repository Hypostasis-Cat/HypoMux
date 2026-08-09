//go:build windows

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

const (
	pipeSessionProtocol   = 1
	pipeSessionAuthKind   = "hypomux.core.authenticate"
	pipeSessionReadyKind  = "hypomux.host.ready"
	maxPipeSessionMessage = 4096
)

type pipeAuthMessage struct {
	Protocol int    `json:"protocol"`
	Kind     string `json:"kind"`
	Token    string `json:"token"`
}

type pipeReadyMessage struct {
	Protocol int    `json:"protocol"`
	Kind     string `json:"kind"`
	OK       bool   `json:"ok"`
	Error    string `json:"error,omitempty"`
}

type bufferedPipeConnection struct {
	io.Reader
	io.Writer
	io.Closer
}

func connectAuthenticatedPipe(
	ctx context.Context,
	name string,
	token string,
	hostPID int,
) (io.ReadWriteCloser, error) {
	if hostPID <= 0 {
		return nil, errors.New("invalid desktop host PID")
	}
	namePointer, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	var handle windows.Handle
	for {
		handle, err = windows.CreateFile(
			namePointer,
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
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(40 * time.Millisecond):
		}
	}
	connection := os.NewFile(uintptr(handle), name)
	if connection == nil {
		_ = windows.CloseHandle(handle)
		return nil, errors.New("create pipe file handle")
	}
	var serverPID uint32
	if err := windows.GetNamedPipeServerProcessId(handle, &serverPID); err != nil {
		_ = connection.Close()
		return nil, fmt.Errorf("read desktop host process identity: %w", err)
	}
	if serverPID != uint32(hostPID) {
		_ = connection.Close()
		return nil, fmt.Errorf("reject unexpected desktop host process (PID %d)", serverPID)
	}
	auth := pipeAuthMessage{
		Protocol: pipeSessionProtocol,
		Kind:     pipeSessionAuthKind,
		Token:    token,
	}
	if err := writePipeMessage(connection, auth); err != nil {
		_ = connection.Close()
		return nil, err
	}
	reader := bufio.NewReaderSize(connection, maxPipeSessionMessage)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		_ = connection.Close()
		return nil, err
	}
	if len(line) > maxPipeSessionMessage {
		_ = connection.Close()
		return nil, errors.New("host authentication response is too large")
	}
	var ready pipeReadyMessage
	if err := json.Unmarshal(line, &ready); err != nil {
		_ = connection.Close()
		return nil, errors.New("invalid host authentication response")
	}
	if ready.Protocol != pipeSessionProtocol || ready.Kind != pipeSessionReadyKind || !ready.OK {
		_ = connection.Close()
		if ready.Error == "" {
			ready.Error = "authentication_failed"
		}
		return nil, fmt.Errorf("host rejected core session: %s", ready.Error)
	}
	return &bufferedPipeConnection{
		Reader: reader,
		Writer: connection,
		Closer: connection,
	}, nil
}

func writePipeMessage(writer io.Writer, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = writer.Write(append(data, '\n'))
	return err
}
