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

func TestHotspotRetryRechecksLivePrerequisitesAfterFailedCleanup(t *testing.T) {
	done := make(chan struct{})
	close(done)
	s := &EngineService{lifecycleGate: make(chan struct{}, 1), hotspot: &hotspotSession{
		done: done, status: HotspotStatus{State: "failed", CleanupComplete: false, Message: "old cleanup error"},
	}}
	_, err := s.StartHotspot(HotspotConfig{SSID: "HypoMux", Password: "password123", Band: "auto"})
	if err == nil || !strings.Contains(err.Error(), "TUN 模式") {
		t.Fatalf("retry should reach live prerequisites, got %v", err)
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
	if mode == "inspect" {
		_ = encoder.Encode(map[string]string{"kind": "inspect_sharing"})
		line, _ := input.ReadBytes('\n')
		var reply hotspotSharingReply
		if json.Unmarshal(line, &reply) != nil || reply.Error != "" || len(reply.Connections) != 2 || reply.Connections[0].Name != "HypoMux-Tun" {
			os.Exit(4)
		}
	}
	if mode == "reject" {
		_ = encoder.Encode(HotspotStatus{State: "failed", Message: "Shared egress mismatch; rolled back", CleanupComplete: true})
		os.Exit(0)
	}
	if mode == "unverified" {
		_ = encoder.Encode(HotspotStatus{State: "running", SharingVerified: false})
	} else if mode == "windows-managed" {
		_ = encoder.Encode(HotspotStatus{State: "running", SharingVerified: false, GatewayAddress: "192.168.137.1", Message: "Egress unverified"})
	} else {
		_ = encoder.Encode(HotspotStatus{State: "running", SSID: config.SSID, SharingVerified: true})
	}
	if mode == "crash" {
		os.Exit(3)
	}
	_, _ = io.Copy(io.Discard, input)
	if mode == "stop-error" {
		_ = encoder.Encode(HotspotStatus{State: "failed", Message: "Desktop disconnected during sharing inspection", CleanupComplete: true})
		os.Exit(0)
	}
	if mode == "cleanup-failure" {
		_ = encoder.Encode(HotspotStatus{State: "failed", Message: "Windows hotspot still active", CleanupComplete: false})
		os.Exit(0)
	}
	_ = encoder.Encode(HotspotStatus{State: "stopped", CleanupComplete: true})
	os.Exit(0)
}

func launchTestHotspot(t *testing.T, mode string, timeout time.Duration) (*hotspotSession, error) {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=^TestHotspotWorkerProcess$")
	command.Env = append(os.Environ(), "HYPOMUX_HOTSPOT_TEST_WORKER="+mode)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return launchHotspot(ctx, command, HotspotConfig{SSID: "HypoMux 手机", Password: "safe-'$`password", Band: "auto"}, func() hotspotSharingReply {
		return hotspotSharingReply{Connections: []hotspotSharingConnection{{Name: "HypoMux-Tun", Role: 0}, {Name: "Wi-Fi Direct", Role: 1}}}
	})
}

func TestHotspotWorkerBrokersSharingInspection(t *testing.T) {
	h, err := launchTestHotspot(t, "inspect", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.stop(ctx); err != nil {
		t.Fatal(err)
	}
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

func TestHotspotExpectedShutdownDoesNotHideCleanupFailure(t *testing.T) {
	for _, mode := range []string{"stop-error", "cleanup-failure"} {
		t.Run(mode, func(t *testing.T) {
			h, err := launchTestHotspot(t, mode, 5*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err = h.stop(ctx)
			status := h.snapshot()
			if mode == "stop-error" {
				if err != nil || status.State != "stopped" || status.Message != "" {
					t.Fatal("expected shutdown reported as failure", status, err)
				}
			} else if err == nil || status.State != "failed" || status.CleanupComplete {
				t.Fatal("real cleanup failure was hidden", status, err)
			}
		})
	}
}

func TestHotspotRepeatedStartStopSessions(t *testing.T) {
	for range 5 {
		h, err := launchTestHotspot(t, "normal", 5*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err = h.stop(ctx)
		cancel()
		if err != nil || h.snapshot().State != "stopped" {
			t.Fatal("session did not cleanly stop", err)
		}
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
	s := &EngineService{lifecycleGate: make(chan struct{}, 1), hotspot: h}
	status, err := s.StopHotspot()
	if err != nil || status.State != "stopped" || status.Message != "" {
		t.Fatal("successful explicit stop retained stale failure", err, status)
	}
}

func TestHotspotPublishesStartingSessionBeforeReadiness(t *testing.T) {
	command := exec.Command(os.Args[0], "-test.run=^TestHotspotWorkerProcess$")
	command.Env = append(os.Environ(), "HYPOMUX_HOTSPOT_TEST_WORKER=normal")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	observed := false
	h, err := launchHotspotObserved(ctx, command, HotspotConfig{SSID: "test", Password: "safe-'$`password", Band: "auto"}, func(h *hotspotSession) {
		observed = h.snapshot().State == "starting"
	})
	if err != nil {
		t.Fatal(err)
	}
	defer h.stop(ctx)
	if !observed {
		t.Fatal("starting session unavailable during launch")
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

func TestHotspotWorkerKeepsOperationalUnverifiedHotspot(t *testing.T) {
	h, err := launchTestHotspot(t, "windows-managed", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	status := h.snapshot()
	if status.State != "running" || status.SharingVerified || status.GatewayAddress != "192.168.137.1" || status.Message == "" {
		t.Fatal(status)
	}
	select {
	case <-h.done:
		t.Fatal("unobserved legacy sharing must not stop an operational hotspot")
	default:
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
