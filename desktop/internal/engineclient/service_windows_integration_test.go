//go:build windows

package engineclient

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestInstalledDesktopExercisesTrustedServiceClientPath(t *testing.T) {
	if os.Getenv("HYPOMUX_RUN_SERVICE_TEST") != "1" {
		t.Skip("set HYPOMUX_RUN_SERVICE_TEST=1 after installing the current build")
	}
	desktopPath := os.Getenv("HYPOMUX_INSTALLED_DESKTOP")
	if desktopPath == "" {
		desktopPath = filepath.Join(os.Getenv("ProgramFiles"), "HypoMux", "hypomux.exe")
	}
	if info, err := os.Stat(desktopPath); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("installed HypoMux desktop is unavailable at %q: %v", desktopPath, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, desktopPath, "--core-service-self-test")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("trusted installed desktop self-test failed: %v\n%s", err, output)
	}
}
