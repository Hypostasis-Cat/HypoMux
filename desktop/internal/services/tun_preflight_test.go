package services

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func testTunService(t *testing.T, platform tunPlatformSnapshot) *TunService {
	t.Helper()
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	settings := NewSettingsService()
	adapters := []AdapterView{
		{
			ID: "ethernet", Name: "以太网", Address: "192.168.10.20",
			PrefixLength: 24, Gateway: "192.168.10.1", Operational: true, Selected: true,
		},
		{
			ID: "wlan", Name: "WLAN", Address: "192.168.10.21",
			PrefixLength: 24, Gateway: "192.168.10.1", Operational: true, Selected: true,
		},
	}
	return &TunService{
		settings:        settings,
		listAdapters:    func() ([]AdapterView, error) { return adapters, nil },
		inspectPlatform: func(bool) tunPlatformSnapshot { return platform },
		resolveEngine:   func() (string, error) { return filepath.Join(t.TempDir(), "hypomux-engine.exe"), nil },
		resolveSingBox:  func() (string, error) { return filepath.Join(t.TempDir(), "sing-box.exe"), nil },
		now:             func() time.Time { return time.Date(2026, 7, 31, 11, 0, 0, 0, time.UTC) },
	}
}

func TestTunPreflightBlocksBeforeNetworkTakeover(t *testing.T) {
	service := testTunService(t, tunPlatformSnapshot{
		WFPReady:            false,
		WFPDetail:           "BFE unavailable",
		DefaultRouteAliases: []string{"Clash"},
	})
	snapshot, err := service.Preflight([]string{"ethernet", "wlan"})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Ready {
		t.Fatal("preflight must block without the privilege broker and with a foreign TUN")
	}
	if snapshot.EffectiveStrictRoute {
		t.Fatal("failed WFP probe must use a per-run compatibility decision")
	}
	for _, code := range []string{
		"privilege_broker_unavailable", "foreign_tun", "wfp_compatibility", "shared_lan_gateway",
	} {
		if !hasTunIssue(snapshot, code) {
			t.Fatalf("missing %s in %+v", code, snapshot.Issues)
		}
	}
	if len(snapshot.SharedGatewayRisks) != 1 {
		t.Fatalf("shared gateway risks = %+v", snapshot.SharedGatewayRisks)
	}
}

func TestTunPreflightCanBecomeReadyWithIndependentCore(t *testing.T) {
	service := testTunService(t, tunPlatformSnapshot{
		PrivilegeBrokerAvailable: true,
		WFPReady:                 true,
		WFPDetail:                "FwpmEngineOpen0 succeeded",
	})
	snapshot, err := service.Preflight([]string{"ethernet"})
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Ready || !snapshot.EffectiveStrictRoute {
		t.Fatalf("expected a ready strict-route preflight: %+v", snapshot)
	}
	if len(snapshot.Issues) != 0 {
		t.Fatalf("unexpected issues: %+v", snapshot.Issues)
	}
}

func TestTunPreflightElevatedHostWarnsWithoutBlocking(t *testing.T) {
	service := testTunService(t, tunPlatformSnapshot{
		HostElevated:             true,
		PrivilegeBrokerAvailable: true,
		WFPReady:                 true,
		WFPDetail:                "FwpmEngineOpen0 succeeded",
	})
	snapshot, err := service.Preflight([]string{"ethernet"})
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Ready {
		t.Fatalf("elevated compatibility mode was blocked: %+v", snapshot)
	}
	for _, issue := range snapshot.Issues {
		if issue.Code != "elevated_ui_host" {
			t.Fatalf("unexpected elevated compatibility issue: %+v", issue)
		}
		if issue.Level != "warning" {
			t.Fatalf("elevated UI issue must be a warning, got %+v", issue)
		}
	}
	if !hasTunIssue(snapshot, "elevated_ui_host") {
		t.Fatalf("missing elevated compatibility warning: %+v", snapshot.Issues)
	}
}

func TestTunPreflightElevatedHostStillRequiresCoreChannel(t *testing.T) {
	service := testTunService(t, tunPlatformSnapshot{
		HostElevated:             true,
		PrivilegeBrokerAvailable: false,
		WFPReady:                 true,
	})
	snapshot, err := service.Preflight([]string{"ethernet"})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Ready {
		t.Fatalf("missing Core channel was ignored: %+v", snapshot)
	}
	if !hasTunIssue(snapshot, "elevated_ui_host") || !hasTunIssue(snapshot, "privilege_broker_unavailable") {
		t.Fatalf("unexpected elevated Core-channel evidence: %+v", snapshot.Issues)
	}
}

func TestTunPreflightReportsMissingResources(t *testing.T) {
	service := testTunService(t, tunPlatformSnapshot{
		PrivilegeBrokerAvailable: true,
		WFPReady:                 true,
	})
	service.resolveEngine = func() (string, error) { return "", errors.New("engine missing") }
	service.resolveSingBox = func() (string, error) { return "", errors.New("sing-box missing") }
	snapshot, err := service.Preflight([]string{})
	if err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{"engine_missing", "sing_box_missing"} {
		if !hasTunIssue(snapshot, code) {
			t.Fatalf("missing %s in %+v", code, snapshot.Issues)
		}
	}
}

func hasTunIssue(snapshot TunPreflightSnapshot, code string) bool {
	for _, issue := range snapshot.Issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}
