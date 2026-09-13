package services

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestHotspotConfigValidation(t *testing.T) {
	valid := HotspotConfig{SSID: "HypoMux 手机", Password: "safe-'$`password", Band: "auto"}
	if err := validateHotspotConfig(valid); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		config HotspotConfig
	}{
		{"blank", HotspotConfig{SSID: "  ", Password: valid.Password, Band: "auto"}},
		{"ssid_bytes", HotspotConfig{SSID: strings.Repeat("网", 11), Password: valid.Password, Band: "auto"}},
		{"ssid_newline", HotspotConfig{SSID: "a\nb", Password: valid.Password, Band: "auto"}},
		{"short_password", HotspotConfig{SSID: "a", Password: "1234567", Band: "auto"}},
		{"unicode_password", HotspotConfig{SSID: "a", Password: "密码12345678", Band: "auto"}},
		{"band", HotspotConfig{SSID: "a", Password: valid.Password, Band: "6"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if validateHotspotConfig(test.config) == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
}

// Exercise the real subprocess/pipe lifecycle without changing host networking.
func TestHotspotWorkerProcess(t *testing.T) {
	mode := os.Getenv("HYPOMUX_HOTSPOT_TEST_WORKER")
	if mode == "" {
		return
	}
	input := bufio.NewReader(os.Stdin)
	line, _ := input.ReadBytes('\n')
	var config HotspotConfig
	if json.Unmarshal(line, &config) != nil || config.Password != "safe-'$`password" {
		os.Exit(2)
	}
	encoder := json.NewEncoder(os.Stdout)
	if mode == "reject" {
		_ = encoder.Encode(HotspotStatus{State: "failed", Message: "Shared egress mismatch; rolled back", CleanupComplete: true})
		os.Exit(0)
	}
	if mode == "unverified" {
		_ = encoder.Encode(HotspotStatus{State: "running", SharingVerified: false})
	} else {
		_ = encoder.Encode(HotspotStatus{State: "running", SSID: config.SSID, SharingVerified: true})
	}
	if mode == "crash" {
		os.Exit(3)
	}
	_, _ = io.Copy(io.Discard, input)
	_ = encoder.Encode(HotspotStatus{State: "stopped", CleanupComplete: true})
	os.Exit(0)
}

func launchTestHotspot(t *testing.T, mode string, timeout time.Duration) (*hotspotSession, error) {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=^TestHotspotWorkerProcess$")
	command.Env = append(os.Environ(), "HYPOMUX_HOTSPOT_TEST_WORKER="+mode)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return launchHotspot(ctx, command, HotspotConfig{SSID: "HypoMux 手机", Password: "safe-'$`password", Band: "auto"})
}

func TestHotspotWorkerStopsOnPipeClose(t *testing.T) {
	h, err := launchTestHotspot(t, "normal", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !h.snapshot().SharingVerified || h.snapshot().SSID != "HypoMux 手机" {
		t.Fatal(h.snapshot())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.stop(ctx); err != nil {
		t.Fatal(err)
	}
	if err := h.stop(ctx); err != nil {
		t.Fatal("stop must be idempotent", err)
	}
	if h.snapshot().State != "stopped" || h.snapshot().SharingVerified {
		t.Fatal(h.snapshot())
	}
}

func TestHotspotWorkerRejectsWrongSharedEgress(t *testing.T) {
	h, err := launchTestHotspot(t, "reject", 5*time.Second)
	if err == nil || !strings.Contains(err.Error(), "egress mismatch") {
		t.Fatalf("unexpected error: %v", err)
	}
	if h.snapshot().State != "failed" {
		t.Fatal(h.snapshot())
	}
	if err := h.stop(context.Background()); err != nil {
		t.Fatal("a rolled-back start failure must not poison engine shutdown", err)
	}
}

func TestHotspotWorkerDoesNotAcceptUnverifiedRunningState(t *testing.T) {
	h, err := launchTestHotspot(t, "unverified", time.Second)
	if err == nil {
		t.Fatal("unverified sharing reported as successful")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.stop(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestHotspotWorkerCrashClearsVerifiedState(t *testing.T) {
	h, _ := launchTestHotspot(t, "crash", 5*time.Second)
	select {
	case <-h.done:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not exit")
	}
	status := h.snapshot()
	if status.State != "failed" || status.SharingVerified || status.Message == "" {
		t.Fatal(status)
	}
}
