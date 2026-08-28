//go:build windows

package services

import (
	"bytes"
	"encoding/base64"
	"image/png"
	"strings"
	"testing"
)

func TestListRunningProcessesReturnsSortedExecutableNames(t *testing.T) {
	processes, err := listRunningProcesses()
	if err != nil {
		t.Fatal(err)
	}
	if len(processes) == 0 {
		t.Fatal("native process snapshot returned no executables")
	}
	seen := make(map[string]struct{}, len(processes))
	previous := ""
	for _, name := range processes {
		key := strings.ToLower(name)
		if !strings.HasSuffix(key, ".exe") {
			t.Fatalf("process name is not an executable: %q", name)
		}
		if _, exists := seen[key]; exists {
			t.Fatalf("duplicate process name: %q", name)
		}
		if previous != "" && previous > key {
			t.Fatalf("process names are not sorted: %q before %q", previous, key)
		}
		seen[key] = struct{}{}
		previous = key
	}
}

func TestListRunningProcessChoicesReturnsValidIcons(t *testing.T) {
	processes, err := listRunningProcessChoices()
	if err != nil {
		t.Fatal(err)
	}
	if len(processes) == 0 {
		t.Fatal("native process snapshot returned no choices")
	}
	seen := make(map[string]struct{}, len(processes))
	previous := ""
	for _, process := range processes {
		key := strings.ToLower(process.Name)
		if !strings.HasSuffix(key, ".exe") {
			t.Fatalf("process name is not an executable: %q", process.Name)
		}
		if _, exists := seen[key]; exists {
			t.Fatalf("duplicate process choice: %q", process.Name)
		}
		if previous != "" && previous > key {
			t.Fatalf("process choices are not sorted: %q before %q", previous, key)
		}
		seen[key] = struct{}{}
		previous = key
		if process.Icon == "" {
			continue
		}
		const prefix = "data:image/png;base64,"
		if !strings.HasPrefix(process.Icon, prefix) {
			t.Fatalf("process icon is not a PNG data URL: %q", process.Name)
		}
		payload, decodeErr := base64.StdEncoding.DecodeString(strings.TrimPrefix(process.Icon, prefix))
		if decodeErr != nil {
			t.Fatalf("decode process icon %q: %v", process.Name, decodeErr)
		}
		image, decodeErr := png.Decode(bytes.NewReader(payload))
		if decodeErr != nil {
			t.Fatalf("decode process icon PNG %q: %v", process.Name, decodeErr)
		}
		if bounds := image.Bounds(); bounds.Dx() != processIconSize || bounds.Dy() != processIconSize {
			t.Fatalf("process icon %q has unexpected dimensions %v", process.Name, bounds)
		}
	}
}

func TestGenericAndUnavailableProcessIconsUseFallback(t *testing.T) {
	generic := loadProcessIcon("hypomux-test-generic.exe", true)
	if generic == nil {
		t.Fatal("Windows Shell did not return its generic executable icon")
	}
	if !isGenericProcessIcon(generic) {
		t.Fatal("Windows generic executable icon was not recognized")
	}
	if icon := processIconDataURL(`Z:\hypomux-does-not-exist\missing.exe`); icon != "" {
		t.Fatal("unavailable executable unexpectedly returned a custom icon")
	}
}
