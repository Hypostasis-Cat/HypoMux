//go:build windows

package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"github.com/Microsoft/go-winio"
)

// uniqueTestPipeName keeps parallel test runs from colliding on the pipe
// namespace.
func uniqueTestPipeName(label string) string {
	return fmt.Sprintf(`\\.\pipe\HypoMux-CLI-Test-%s-%d-%d`, label, os.Getpid(), time.Now().UnixNano())
}

func TestConnectServicePipeVerifiesServerPID(t *testing.T) {
	pipeName := uniqueTestPipeName("pid")
	listener, err := winio.ListenPipe(pipeName, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	// Serve connections in a loop; each request must be consumed so the
	// client's overlapped write completes.
	served := make(chan io.ReadWriteCloser, 4)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			buffer := make([]byte, 32)
			_, _ = conn.Read(buffer)
			served <- conn
		}
	}()

	// The in-process listener makes this test binary the pipe server, so the
	// expected server PID is our own.
	connection, err := connectServicePipe(context.Background(), pipeName, os.Getpid())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, err := connection.Write([]byte("ping\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	select {
	case server := <-served:
		_ = server.Close()
	case <-time.After(2 * time.Second):
		t.Fatal("pipe server never consumed the client request")
	}
	if err := connection.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// The server side re-validates this client's fingerprint; here the client
	// rejects a pipe served by a process other than the queried service PID.
	if _, err := connectServicePipe(context.Background(), pipeName, os.Getpid()+1); err == nil {
		t.Fatal("a pipe served by an unexpected process was accepted")
	}
}

func TestQueryCoreServiceHandlesAbsentService(t *testing.T) {
	// A service name that must never exist keeps this test hermetic: the SCM
	// lookup reports "not installed" instead of an error.
	pid, installed, err := queryCoreService("HypoMuxCore-Definitely-Missing")
	if err != nil {
		t.Fatalf("query missing service: %v", err)
	}
	if installed || pid != 0 {
		t.Fatalf("missing service reported installed=%t pid=%d", installed, pid)
	}
}
