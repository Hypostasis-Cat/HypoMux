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

	"github.com/Microsoft/go-winio"
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
		file pipeFile
		err  error
	}, 1)
	go func() {
		file, acceptErr := pipe.accept(ctx, os.Getpid())
		accepted <- struct {
			file pipeFile
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

func TestAuthenticatedPipeSupportsConcurrentProtocolTrafficAfterAuthDeadline(t *testing.T) {
	pipe, err := createAuthenticatedPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer pipe.close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	accepted := make(chan struct {
		file pipeFile
		err  error
	}, 1)
	go func() {
		file, acceptErr := pipe.accept(ctx, os.Getpid())
		accepted <- struct {
			file pipeFile
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
	result := <-accepted
	if result.err != nil {
		t.Fatal(result.err)
	}
	host := result.file
	defer host.Close()

	// The authentication context belongs only to the handshake. The returned
	// connection must still carry bidirectional RPC traffic after it expires.
	<-ctx.Done()
	trafficCtx, trafficCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer trafficCancel()

	hostReply := make(chan error, 1)
	readStarted := make(chan struct{})
	go func() {
		close(readStarted)
		line, readErr := bufio.NewReader(host).ReadBytes('\n')
		if readErr == nil && string(line) != "protocol-response\n" {
			readErr = errors.New("unexpected protocol response")
		}
		hostReply <- readErr
	}()
	clientDone := make(chan error, 1)
	go func() {
		line, readErr := bufio.NewReader(client).ReadBytes('\n')
		if readErr != nil {
			clientDone <- readErr
			return
		}
		if string(line) != "protocol-request\n" {
			clientDone <- errors.New("unexpected protocol request")
			return
		}
		_, writeErr := client.Write([]byte("protocol-response\n"))
		clientDone <- writeErr
	}()
	<-readStarted
	writeDone := make(chan error, 1)
	go func() {
		_, writeErr := host.Write([]byte("protocol-request\n"))
		writeDone <- writeErr
	}()

	for label, channel := range map[string]<-chan error{
		"host write":      writeDone,
		"client exchange": clientDone,
		"host read":       hostReply,
	} {
		select {
		case err := <-channel:
			if err != nil {
				t.Fatalf("%s failed: %v", label, err)
			}
		case <-trafficCtx.Done():
			t.Fatalf("%s blocked: %v", label, trafficCtx.Err())
		}
	}
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

func TestAuthenticatedPipeConnectionHonoursReadDeadline(t *testing.T) {
	pipe, err := createAuthenticatedPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer pipe.close()

	client := openTestPipe(t, pipe.name)
	defer client.Close()

	connectCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := connectAuthenticatedPipeServer(connectCtx, pipe.handle); err != nil {
		t.Fatalf("accepting pipe connection: %v", err)
	}

	connection, err := newPipeFile(pipe.handle)
	if err != nil {
		t.Fatal(err)
	}
	pipe.handle = windows.InvalidHandle
	defer connection.Close()

	// authenticateCore relies on this deadline to wake its reader goroutine even
	// when the Close on the ctx.Done() path fails.
	if err := connection.SetReadDeadline(time.Now().Add(150 * time.Millisecond)); err != nil {
		t.Fatalf("pipe connection does not support read deadlines: %v", err)
	}
	started := time.Now()
	if _, err := connection.Read(make([]byte, 16)); !errors.Is(err, winio.ErrTimeout) {
		t.Fatalf("read returned %v, want %v", err, winio.ErrTimeout)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("deadline took %v to abort the blocked read", elapsed)
	}
}

func openTestPipe(t *testing.T, name string) pipeFile {
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
		windows.FILE_FLAG_OVERLAPPED,
		0,
	)
	if err != nil {
		t.Fatal(err)
	}
	file, err := newPipeFile(handle)
	if err != nil {
		t.Fatal(err)
	}
	return file
}

func readTestPipeMessage(reader pipeFile, target any) error {
	line, err := bufio.NewReaderSize(reader, maxSessionMessageBytes).ReadBytes('\n')
	if err != nil {
		return err
	}
	if len(line) > maxSessionMessageBytes {
		return errors.New("session message too large")
	}
	return json.Unmarshal(line, target)
}
