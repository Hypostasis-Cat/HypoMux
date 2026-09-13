//go:build windows

package services

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

// Only exercise the real worker's preflight rejection. Never start a hotspot
// from an automated test, even on a machine with an active HypoMux TUN.
func TestHotspotWindowsRejectsMissingTUNWithoutMutation(t *testing.T) {
	interfaces, err := net.Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	for _, adapter := range interfaces {
		if adapter.Name == "HypoMux-Tun" {
			t.Skip("active TUN; do not mutate live networking")
		}
	}
	command, err := hotspotCommand()
	if err != nil {
		t.Skip(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	h, err := launchHotspot(ctx, command, HotspotConfig{SSID: "HypoMux-test", Password: "test-password", Band: "auto"})
	if err == nil {
		_ = h.stop(ctx)
		t.Fatal("worker unexpectedly accepted missing TUN")
	}
	if !strings.Contains(err.Error(), "TUN profile discovery") || !strings.Contains(err.Error(), "HypoMux-Tun") {
		t.Fatal(err)
	}
	if !h.snapshot().CleanupComplete {
		t.Fatal("preflight must exit cleanly", h.snapshot())
	}
}
