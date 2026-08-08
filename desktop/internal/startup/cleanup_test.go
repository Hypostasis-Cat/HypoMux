//go:build windows

package startup

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCleanupZombieProcesses(t *testing.T) {
	// Basic smoke test: should not panic or block indefinitely
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := CleanupZombieProcesses(ctx)
	// Error is acceptable (e.g., insufficient privileges for some operations)
	// but the function should complete within timeout
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatal("cleanup timed out")
	}

	t.Logf("cleanup completed with result: %v", err)
}

func TestStartupCleanupToolsDoNotDependOnPATH(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	for _, resolve := range []struct {
		name string
		call func() (string, error)
	}{
		{name: "powershell.exe", call: resolveStartupPowerShell},
		{name: "taskkill.exe", call: func() (string, error) { return resolveStartupSystemExecutable("taskkill.exe") }},
	} {
		path, err := resolve.call()
		if err != nil {
			t.Fatalf("resolve %s: %v", resolve.name, err)
		}
		if !filepath.IsAbs(path) || !strings.EqualFold(filepath.Base(path), resolve.name) {
			t.Fatalf("resolved %s path = %q", resolve.name, path)
		}
	}
}

func TestKillProcess(t *testing.T) {
	ctx := context.Background()

	// Killing a non-existent process should not error
	err := killProcess(ctx, "nonexistent-process-xyz123.exe")
	if err != nil {
		t.Errorf("killing non-existent process should not error: %v", err)
	}
}

func TestCleanupTunAndRoutes(t *testing.T) {
	// This test may require admin privileges to succeed fully,
	// but should not panic or hang
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	err := cleanupTunAndRoutes(ctx)
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatal("TUN cleanup timed out")
	}

	t.Logf("TUN cleanup completed with result: %v", err)
}
