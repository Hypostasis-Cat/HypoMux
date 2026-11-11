package server

import (
	"bytes"
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Hypostasis-Cat/HypoMux/engine/internal/platform"
)

func TestHotspotInspectionRequiresElevatedCore(t *testing.T) {
	s := New(strings.NewReader(""), io.Discard, Metadata{})
	s.identity.Elevated = false
	response, shutdown := s.handle(context.Background(), []byte(`{"protocol":1,"id":"inspect","method":"hotspot.inspect"}`))
	if shutdown || response.Error == nil || response.Error.Code != "elevation_required" {
		t.Fatalf("unexpected response: %+v", response)
	}
}

// Windows ICS can take tens of seconds to enumerate. That must not stall the
// serial request loop: other requests are answered first, and Run waits for
// the in-flight inspection before returning.
func TestSlowHotspotInspectionDoesNotBlockOtherRequests(t *testing.T) {
	input, feed := io.Pipe()
	output := &lockedBuffer{}
	s := New(input, output, Metadata{})
	s.identity.Elevated = true
	release := make(chan struct{})
	s.inspectSharing = func(ctx context.Context) (platform.SharingSnapshot, error) {
		select {
		case <-release:
		case <-ctx.Done():
		}
		return platform.SharingSnapshot{Connections: []platform.SharingConnection{}}, nil
	}
	done := make(chan error, 1)
	go func() { done <- s.Run(context.Background()) }()
	_, _ = feed.Write([]byte(`{"protocol":1,"id":"inspect","method":"hotspot.inspect"}` + "\n"))
	_, _ = feed.Write([]byte(`{"protocol":1,"id":"hello","method":"engine.hello"}` + "\n"))
	waitFor(t, func() bool { return strings.Contains(output.String(), `"id":"hello"`) })
	if strings.Contains(output.String(), `"id":"inspect"`) {
		t.Fatal("inspection answered before it finished")
	}
	close(release)
	waitFor(t, func() bool { return strings.Contains(output.String(), `"id":"inspect"`) })
	_ = feed.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return")
	}
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("condition not reached")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
