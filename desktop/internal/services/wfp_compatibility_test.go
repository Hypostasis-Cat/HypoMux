package services

import (
	"strings"
	"testing"
)

func TestWFPCompatibilityFailureIsDeviceOwnedAndClearedByExplicitRetry(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	originalFingerprint := currentWFPFingerprint
	currentWFPFingerprint = func() string { return "windows-build|hypomux-build" }
	t.Cleanup(func() { currentWFPFingerprint = originalFingerprint })

	service := NewSettingsService()
	if err := service.RememberWFPCompatibilityFailure("FwpmEngineOpen0 failed"); err != nil {
		t.Fatal(err)
	}
	blocked, detail := service.rememberedWFPCompatibilityFailure()
	if !blocked || !strings.Contains(detail, "FwpmEngineOpen0") {
		t.Fatalf("remembered failure = %v, %q", blocked, detail)
	}

	ordinary := service.Get()
	ordinary.Language = "en"
	ordinary.WFPCompatibility = WFPCompatibilityState{}
	persisted, err := service.Update(ordinary)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.WFPCompatibility.Status != "failed" {
		t.Fatalf("ordinary settings write erased device state: %#v", persisted.WFPCompatibility)
	}

	disabled := persisted
	disabled.StrictRoute = false
	if _, err := service.Update(disabled); err != nil {
		t.Fatal(err)
	}
	retry := service.Get()
	retry.StrictRoute = true
	retry, err = service.Update(retry)
	if err != nil {
		t.Fatal(err)
	}
	if retry.WFPCompatibility.Status != "" {
		t.Fatalf("explicit retry did not clear compatibility state: %#v", retry.WFPCompatibility)
	}
}

func TestWFPCompatibilityFailureExpiresWhenEnvironmentChanges(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	originalFingerprint := currentWFPFingerprint
	fingerprint := "before-update"
	currentWFPFingerprint = func() string { return fingerprint }
	t.Cleanup(func() { currentWFPFingerprint = originalFingerprint })

	service := NewSettingsService()
	if err := service.RememberWFPCompatibilityFailure("synthetic failure"); err != nil {
		t.Fatal(err)
	}
	fingerprint = "after-update"
	if blocked, _ := service.rememberedWFPCompatibilityFailure(); blocked {
		t.Fatal("old WFP failure suppressed a retry after environment change")
	}
}

func TestTunPreflightUsesRememberedWFPCompatibilityMode(t *testing.T) {
	originalFingerprint := currentWFPFingerprint
	currentWFPFingerprint = func() string { return "stable-device" }
	t.Cleanup(func() { currentWFPFingerprint = originalFingerprint })

	service := testTunService(t, tunPlatformSnapshot{
		PrivilegeBrokerAvailable: true,
		WFPReady:                 true,
	})
	if err := service.settings.RememberWFPCompatibilityFailure("BFE unavailable"); err != nil {
		t.Fatal(err)
	}
	probedWFP := true
	service.inspectPlatform = func(checkWFP bool) tunPlatformSnapshot {
		probedWFP = checkWFP
		return tunPlatformSnapshot{
			PrivilegeBrokerAvailable: true,
			WFPReady:                 true,
		}
	}
	snapshot, err := service.Preflight([]string{"ethernet"})
	if err != nil {
		t.Fatal(err)
	}
	if probedWFP {
		t.Fatal("remembered device failure repeated the WFP probe")
	}
	if snapshot.EffectiveStrictRoute || !hasTunIssue(snapshot, "wfp_compatibility") {
		t.Fatalf("remembered compatibility snapshot = %#v", snapshot)
	}
}
