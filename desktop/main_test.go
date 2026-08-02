package main

import (
	"errors"
	"os"
	"strings"
	"testing"

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
		"startup.CleanupZombieProcesses",
		"desktopplatform.WebView2Available",
		"application.New(",
	} {
		sideEffectAt := strings.Index(source, sideEffect)
		if sideEffectAt < 0 || prepareAt >= sideEffectAt {
			t.Fatalf("privilege normalization must precede %s: prepare=%d side-effect=%d", sideEffect, prepareAt, sideEffectAt)
		}
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
		settings,
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
		settings,
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
