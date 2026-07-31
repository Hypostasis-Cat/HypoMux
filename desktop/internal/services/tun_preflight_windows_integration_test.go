//go:build windows

package services

import (
	"os"
	"testing"
)

func TestRealWindowsTunPreflightIsReadOnly(t *testing.T) {
	if os.Getenv("HYPOMUX_RUN_TUN_PREFLIGHT_TEST") != "1" {
		t.Skip("set HYPOMUX_RUN_TUN_PREFLIGHT_TEST=1 for the explicit read-only Windows TUN preflight")
	}
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	settings := NewSettingsService()
	adapters := NewAdapterService(settings)
	available, err := adapters.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(available) == 0 {
		t.Skip("no active IPv4 adapter is available")
	}
	service := NewTunService(settings, adapters)
	before := inspectTunPlatform(true)
	snapshot, err := service.Preflight([]string{available[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	after := inspectTunPlatform(true)
	if snapshot.CheckedAt.IsZero() || snapshot.WFPDetail == "" {
		t.Fatalf("incomplete real preflight: %+v", snapshot)
	}
	if !snapshot.PrivilegeBrokerAvailable {
		t.Fatal("the authenticated independent Core launcher should be available on Windows")
	}
	if len(before.DefaultRouteAliases) != len(after.DefaultRouteAliases) {
		t.Fatalf("default-route aliases changed during read-only preflight: before=%v after=%v", before.DefaultRouteAliases, after.DefaultRouteAliases)
	}
	for index := range before.DefaultRouteAliases {
		if before.DefaultRouteAliases[index] != after.DefaultRouteAliases[index] {
			t.Fatalf("default-route aliases changed during read-only preflight: before=%v after=%v", before.DefaultRouteAliases, after.DefaultRouteAliases)
		}
	}
}
