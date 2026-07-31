//go:build windows

package engineclient

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestAuthenticatedPipeAcceptsExpectedProcessAndToken(t *testing.T) {
	pipe, err := createAuthenticatedPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer pipe.close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	accepted := make(chan struct {
		file *os.File
		err  error
	}, 1)
	go func() {
		file, acceptErr := pipe.accept(ctx, os.Getpid())
		accepted <- struct {
			file *os.File
			err  error
		}{file: file, err: acceptErr}
	}()

	client := openTestPipe(t, pipe.name)
	defer client.Close()
	if err := writeSessionMessage(client, sessionAuthMessage{
		Protocol: sessionProtocol,
		Kind:     sessionAuthKind,
		Token:    pipe.token,
	}); err != nil {
		t.Fatal(err)
	}
	var ready sessionReadyMessage
	if err := readTestPipeMessage(client, &ready); err != nil {
		t.Fatal(err)
	}
	if !ready.OK || ready.Kind != sessionReadyKind {
		t.Fatalf("unexpected ready response: %+v", ready)
	}
	result := <-accepted
	if result.err != nil {
		t.Fatal(result.err)
	}
	if result.file == nil {
		t.Fatal("authenticated pipe did not return a connection")
	}
	_ = result.file.Close()
}

func TestAuthenticatedPipeRejectsWrongOneTimeToken(t *testing.T) {
	pipe, err := createAuthenticatedPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer pipe.close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	accepted := make(chan error, 1)
	go func() {
		_, acceptErr := pipe.accept(ctx, os.Getpid())
		accepted <- acceptErr
	}()

	client := openTestPipe(t, pipe.name)
	defer client.Close()
	if err := writeSessionMessage(client, sessionAuthMessage{
		Protocol: sessionProtocol,
		Kind:     sessionAuthKind,
		Token:    "not-the-issued-token",
	}); err != nil {
		t.Fatal(err)
	}
	var ready sessionReadyMessage
	if err := readTestPipeMessage(client, &ready); err != nil {
		t.Fatal(err)
	}
	if ready.OK || ready.Error != "authentication_failed" {
		t.Fatalf("unexpected rejection response: %+v", ready)
	}
	if err := <-accepted; err == nil {
		t.Fatal("wrong token was accepted")
	}
}

func TestAuthenticatedPipeRejectsUnexpectedClientPID(t *testing.T) {
	pipe, err := createAuthenticatedPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer pipe.close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	accepted := make(chan error, 1)
	go func() {
		_, acceptErr := pipe.accept(ctx, os.Getpid()+1)
		accepted <- acceptErr
	}()
	client := openTestPipe(t, pipe.name)
	defer client.Close()
	if err := <-accepted; err == nil {
		t.Fatal("unexpected client PID was accepted")
	}
}

func openTestPipe(t *testing.T, name string) *os.File {
	t.Helper()
	namePointer, err := windows.UTF16PtrFromString(name)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(
		namePointer,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		0,
		nil,
		windows.OPEN_EXISTING,
		0,
		0,
	)
	if err != nil {
		t.Fatal(err)
	}
	file := os.NewFile(uintptr(handle), name)
	if file == nil {
		_ = windows.CloseHandle(handle)
		t.Fatal("create client pipe file")
	}
	return file
}

func readTestPipeMessage(reader *os.File, target any) error {
	line, err := bufio.NewReaderSize(reader, maxSessionMessageBytes).ReadBytes('\n')
	if err != nil {
		return err
	}
	if len(line) > maxSessionMessageBytes {
		return errors.New("session message too large")
	}
	return json.Unmarshal(line, target)
}
