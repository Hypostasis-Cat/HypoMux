package engineclient

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type blockedWriter struct {
	closed chan struct{}
	once   sync.Once
}

func (writer *blockedWriter) Write([]byte) (int, error) {
	<-writer.closed
	return 0, errors.New("writer closed")
}

func (writer *blockedWriter) Close() error {
	writer.once.Do(func() { close(writer.closed) })
	return nil
}

type inertProcess struct{}

func (inertProcess) Wait() error { return nil }
func (inertProcess) Kill() error { return nil }
func (inertProcess) PID() int    { return 1 }

func TestReadLoopDeliversCoreEventsWithoutTreatingThemAsResponses(t *testing.T) {
	client := newClient(countingLauncher{}, countingLauncher{})
	session := &coreSession{
		reader: bytes.NewBufferString(
			"{\"protocol\":1,\"sequence\":7,\"event\":\"dns.fallback_required\",\"data\":{\"adapter\":\"WLAN\"}}\n",
		),
		close: func() error { return nil },
	}
	client.session = session
	go client.readLoop(session)

	select {
	case event := <-client.Events():
		if event.Name != "dns.fallback_required" || event.Sequence != 7 ||
			!bytes.Contains(event.Data, []byte(`"adapter":"WLAN"`)) {
			t.Fatalf("unexpected event: %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("core event was not delivered")
	}
}

func TestRequestDeadlineInterruptsBlockedTransportWrite(t *testing.T) {
	client := newClient(countingLauncher{}, countingLauncher{})
	writer := &blockedWriter{closed: make(chan struct{})}
	client.session = &coreSession{
		writer:  writer,
		close:   writer.Close,
		process: inertProcess{},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	started := time.Now()
	err := client.Request(ctx, "engine.status", nil, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("blocked write did not return its context deadline: %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("blocked write returned too late: %s", elapsed)
	}
}
