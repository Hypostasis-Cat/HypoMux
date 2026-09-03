package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Hypostasis-Cat/HypoMux/desktop/internal/services"
)

func TestPrivilegeNormalizationPrecedesDesktopSideEffects(t *testing.T) {
	sourceBytes, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(sourceBytes)
	prepareAt := strings.Index(source, "startup.PrepareDesktopLaunch")
	if prepareAt < 0 || !strings.Contains(source[prepareAt:], "if launchSecurity.Relaunched") {
		t.Fatal("desktop entry point does not exit after a successful standard-permission relaunch")
	}
	for _, sideEffect := range []string{
		"desktopplatform.WebView2Available",
		"application.New(",
	} {
		sideEffectAt := strings.Index(source, sideEffect)
		if sideEffectAt < 0 || prepareAt >= sideEffectAt {
			t.Fatalf("privilege normalization must precede %s: prepare=%d side-effect=%d", sideEffect, prepareAt, sideEffectAt)
		}
	}
}

func TestOrdinaryStartupDoesNotRunDestructiveRecovery(t *testing.T) {
	sourceBytes, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(sourceBytes)
	for _, forbidden := range []string{
		"CleanupZombieProcesses",
		"cleanupTunAndRoutes",
		"taskkill",
		"Remove-NetRoute",
		"Disable-PnpDevice",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("ordinary desktop startup still contains destructive recovery %q", forbidden)
		}
	}
	if !strings.Contains(source, "SingleInstance:") {
		t.Fatal("desktop startup lost its single-instance boundary")
	}
}

func TestElevatedCompatibilityFallbackDoesNotShowStartupModal(t *testing.T) {
	sourceBytes, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(sourceBytes)
	if strings.Contains(source, "ShowElevationCompatibilityMessage") || strings.Contains(source, "MessageBoxW") {
		t.Fatal("startup compatibility fallback must not show a modal message box")
	}
	if !strings.Contains(source, "desktop privilege compatibility fallback") {
		t.Fatal("startup compatibility fallback must be recorded in the process log")
	}
}

func TestTrayUsesNativeMenuWithoutSecondWebView(t *testing.T) {
	mainSource, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	mainText := string(mainSource)
	for _, forbidden := range []string{"createTrayMenuWindow", "tray-menu.html", "Name:             \"tray-menu\""} {
		if strings.Contains(mainText, forbidden) {
			t.Fatalf("desktop entry point still creates a tray WebView: found %q", forbidden)
		}
	}

	hostSource, err := os.ReadFile("internal/platform/wails/desktop.go")
	if err != nil {
		t.Fatal(err)
	}
	host := string(hostSource)
	for _, required := range []string{
		"menu := d.app.Menu.New()",
		"d.trayStatus = menu.Add",
		"d.tray.OnClick",
		"d.tray.SetMenu(menu)",
	} {
		if !strings.Contains(host, required) {
			t.Fatalf("native tray menu is missing %q", required)
		}
	}
	for _, forbidden := range []string{"AttachWindow", "ToggleWindow", "trayMenuWindow", "trayMenuFactory"} {
		if strings.Contains(host, forbidden) {
			t.Fatalf("tray host still uses a WebView popup: found %q", forbidden)
		}
	}
}

func TestHasArgument(t *testing.T) {
	if !hasArgument([]string{"--silent", "--recover-network"}, "--recover-network") {
		t.Fatal("expected recovery argument to be found")
	}
	if hasArgument([]string{"--silent"}, "--recover-network") {
		t.Fatal("unexpected recovery argument")
	}
}

func TestRunAutoStartAccelerationUpdatesTrayStatus(t *testing.T) {
	settings := services.DefaultSettings()
	settings.Mode = "proxy"
	statuses := make([]string, 0, 2)
	err := runAutoStartAcceleration(
		context.Background(),
		settings,
		nil,
		func(mode string) (services.EngineSnapshot, error) {
			if mode != "proxy" {
				t.Fatalf("unexpected startup mode: %s", mode)
			}
			return services.EngineSnapshot{Phase: "running", Mode: mode}, nil
		},
		func(phase string, mode string) {
			statuses = append(statuses, phase+":"+mode)
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 2 || statuses[0] != "starting:proxy" || statuses[1] != "running:proxy" {
		t.Fatalf("unexpected tray status sequence: %#v", statuses)
	}

	statuses = statuses[:0]
	expected := errors.New("startup failed")
	err = runAutoStartAcceleration(
		context.Background(),
		settings,
		nil,
		func(string) (services.EngineSnapshot, error) { return services.EngineSnapshot{}, expected },
		func(phase string, mode string) { statuses = append(statuses, phase+":"+mode) },
	)
	if !errors.Is(err, expected) {
		t.Fatalf("unexpected startup error: %v", err)
	}
	if len(statuses) != 2 || statuses[1] != "failed:proxy" {
		t.Fatalf("startup failure did not reach the tray: %#v", statuses)
	}
}

func TestRunAutoStartAccelerationWaitsForAdapters(t *testing.T) {
	settings := services.DefaultSettings()
	settings.Mode = "tun"
	ready := false
	started := false
	statuses := make([]string, 0, 2)
	err := runAutoStartAcceleration(
		context.Background(),
		settings,
		func(context.Context) error {
			if started {
				t.Fatal("engine started before adapters became ready")
			}
			ready = true
			return nil
		},
		func(mode string) (services.EngineSnapshot, error) {
			if !ready {
				t.Fatal("engine started before readiness check completed")
			}
			started = true
			return services.EngineSnapshot{Phase: "running", Mode: mode}, nil
		},
		func(phase string, mode string) { statuses = append(statuses, phase+":"+mode) },
	)
	if err != nil {
		t.Fatal(err)
	}
	if !started || len(statuses) != 2 || statuses[0] != "starting:tun" || statuses[1] != "running:tun" {
		t.Fatalf("unexpected automatic startup: started=%t statuses=%#v", started, statuses)
	}
}

func TestRunAutoStartAccelerationReadinessTimeoutDoesNotStart(t *testing.T) {
	settings := services.DefaultSettings()
	settings.Mode = "proxy"
	expected := context.DeadlineExceeded
	started := false
	statuses := []string{}
	err := runAutoStartAcceleration(
		context.Background(),
		settings,
		func(context.Context) error { return expected },
		func(string) (services.EngineSnapshot, error) {
			started = true
			return services.EngineSnapshot{}, nil
		},
		func(phase string, mode string) { statuses = append(statuses, phase+":"+mode) },
	)
	if !errors.Is(err, expected) || started {
		t.Fatalf("unexpected timeout result: err=%v started=%t", err, started)
	}
	if len(statuses) != 1 || statuses[0] != "failed:proxy" {
		t.Fatalf("readiness failure did not reach the tray: %#v", statuses)
	}
}

func TestWaitForSelectedAdaptersStartsAfterMissingAdapterAppears(t *testing.T) {
	checks := 0
	err := waitForSelectedAdapters(
		context.Background(),
		[]string{"Ethernet", "WLAN"},
		time.Millisecond,
		func() ([]services.AdapterView, error) {
			checks++
			adapters := []services.AdapterView{{ID: "Ethernet"}}
			if checks >= 2 {
				adapters = append(adapters, services.AdapterView{ID: "WLAN"})
			}
			return adapters, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if checks != 2 {
		t.Fatalf("adapter checks = %d, want 2", checks)
	}
}

func TestWaitForSelectedAdaptersReportsMissingAdaptersAtDeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := waitForSelectedAdapters(
		ctx,
		[]string{"WLAN", "Ethernet"},
		time.Hour,
		func() ([]services.AdapterView, error) {
			return []services.AdapterView{{ID: "Ethernet"}}, nil
		},
	)
	if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "WLAN") {
		t.Fatalf("unexpected wait error: %v", err)
	}
}

func TestShouldAutoStartAcceleration(t *testing.T) {
	settings := services.DefaultSettings()
	settings.Autostart = true
	settings.AutoStartEngine = true
	if !shouldAutoStartAcceleration(true, settings) {
		t.Fatal("silent boot launch should start acceleration when both preferences are enabled")
	}
	if shouldAutoStartAcceleration(false, settings) {
		t.Fatal("normal interactive launch must not auto-start acceleration")
	}
	settings.Autostart = false
	if shouldAutoStartAcceleration(true, settings) {
		t.Fatal("acceleration must not auto-start when launch at startup is disabled")
	}
}
