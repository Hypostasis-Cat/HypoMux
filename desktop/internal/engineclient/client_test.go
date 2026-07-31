package engineclient

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRealEngineHandshakeAndStoppedStatus(t *testing.T) {
	path := filepath.Clean(filepath.Join("..", "..", "..", "..", "hypomux-engine.exe"))
	if _, err := os.Stat(path); err != nil {
		t.Skip("real hypomux-engine.exe is not available")
	}
	t.Setenv("HYPOMUX_ENGINE_PATH", path)
	client := New()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	hello, err := client.Ensure(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Shutdown(ctx)
	if hello.ProtocolVersion != ProtocolVersion {
		t.Fatalf("unexpected protocol %d", hello.ProtocolVersion)
	}
	var status struct {
		Engine struct {
			State string `json:"state"`
		} `json:"engine"`
	}
	if err := client.Request(ctx, "engine.status", nil, &status); err != nil {
		t.Fatal(err)
	}
	if status.Engine.State != "stopped" {
		t.Fatalf("expected stopped engine, got %q", status.Engine.State)
	}
}
