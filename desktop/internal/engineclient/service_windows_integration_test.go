//go:build windows

package engineclient

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestInstalledServiceHandlesConsecutiveClientRequests(t *testing.T) {
	if os.Getenv("HYPOMUX_RUN_SERVICE_TEST") != "1" {
		t.Skip("set HYPOMUX_RUN_SERVICE_TEST=1 for the installed Core Service protocol check")
	}
	enginePath := filepath.Join("..", "..", "bin", "hypomux-engine.exe")
	if _, err := os.Stat(enginePath); err != nil {
		t.Fatalf("built Core is unavailable: %v", err)
	}
	t.Setenv("HYPOMUX_ENGINE_PATH", enginePath)

	client := New()
	defer client.Kill()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	hello, err := client.EnsureElevated(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !hello.Elevated {
		t.Fatal("installed Core Service did not report an elevated identity")
	}
	for _, method := range []string{"engine.status", "health.check", "engine.status"} {
		var response map[string]any
		if err := client.Request(ctx, method, nil, &response); err != nil {
			t.Fatalf("%s after handshake failed: %v", method, err)
		}
	}
}
