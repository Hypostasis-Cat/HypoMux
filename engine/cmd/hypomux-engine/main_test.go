package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestDiagnoseCommandEmitsCompatibleJSONForInvalidSource(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exitCode := run(
		[]string{"diagnose", "--src-ip", "invalid", "--target-ip", "223.5.5.5"},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if exitCode != 0 {
		t.Fatalf("exit code = %d, stderr = %s", exitCode, stderr.String())
	}

	var result map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON %q: %v", stdout.String(), err)
	}
	if result["status"] != "unavailable" || result["note"] != "invalid --src-ip" {
		t.Fatalf("result = %#v", result)
	}
	if result["loss_rate"] != float64(100) || result["sent"] != float64(0) {
		t.Fatalf("counters = %#v", result)
	}
}

func TestServePipeRejectsMissingAuthenticatedSessionArguments(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exitCode := run(
		[]string{"serve-pipe", "--pipe", `\\.\pipe\HypoMux-Core-test`},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if exitCode != 2 {
		t.Fatalf("exit code = %d, stderr = %s", exitCode, stderr.String())
	}
	if !strings.Contains(stderr.String(), "--session-token") {
		t.Fatalf("missing safe argument error: %s", stderr.String())
	}
}

func TestRecoverCommandReportsCleanupFailure(t *testing.T) {
	originalRecoverTUN := recoverTUN
	recoverTUN = func(context.Context) error {
		return errors.New("cleanup failed")
	}
	t.Cleanup(func() {
		recoverTUN = originalRecoverTUN
	})

	var stdout, stderr bytes.Buffer
	exitCode := run(
		[]string{"recover"},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if exitCode != 1 {
		t.Fatalf("exit code = %d, stderr = %s", exitCode, stderr.String())
	}
	if !strings.Contains(stderr.String(), "cleanup failed") {
		t.Fatalf("missing cleanup failure: %s", stderr.String())
	}
}

func TestServiceManagementCommandsReportResults(t *testing.T) {
	originalInstallService := installServiceCommand
	originalRemoveService := removeServiceCommand
	var installedDesktop string
	installServiceCommand = func(desktop string) error {
		installedDesktop = desktop
		return nil
	}
	removeServiceCommand = func() error { return errors.New("remove failed") }
	t.Cleanup(func() {
		installServiceCommand = originalInstallService
		removeServiceCommand = originalRemoveService
	})

	var stdout, stderr bytes.Buffer
	if exitCode := run(
		[]string{"install-service", "--desktop", `D:\Apps\HypoMux\hypomux.exe`},
		strings.NewReader(""),
		&stdout,
		&stderr,
	); exitCode != 0 {
		t.Fatalf("install exit code = %d, stderr = %s", exitCode, stderr.String())
	}
	if installedDesktop != `D:\Apps\HypoMux\hypomux.exe` {
		t.Fatalf("desktop path = %q", installedDesktop)
	}
	if !strings.Contains(stdout.String(), "installed and started") {
		t.Fatalf("install output = %q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := run([]string{"remove-service"}, strings.NewReader(""), &stdout, &stderr); exitCode != 1 {
		t.Fatalf("remove exit code = %d, stderr = %s", exitCode, stderr.String())
	}
	if !strings.Contains(stderr.String(), "remove failed") {
		t.Fatalf("remove error = %q", stderr.String())
	}
}

func TestInstallServiceRequiresDesktopPath(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if exitCode := run([]string{"install-service"}, strings.NewReader(""), &stdout, &stderr); exitCode != 2 {
		t.Fatalf("exit code = %d, stderr = %s", exitCode, stderr.String())
	}
	if !strings.Contains(stderr.String(), "--desktop") {
		t.Fatalf("missing desktop path error: %s", stderr.String())
	}
}
