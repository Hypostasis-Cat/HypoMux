//go:build windows

package tun

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestCleanupPowerShellIsScopedToHypoMuxTun(t *testing.T) {
	required := []string{
		"InterfaceAlias -eq 'HypoMux-Tun'",
		"DestinationPrefix -eq '0.0.0.0/0'",
		"DestinationPrefix -eq '::/0'",
		"FriendlyName -eq 'HypoMux-Tun'",
		"InstanceId -like '*WINTUN*'",
		"Get-NetAdapter -IncludeHidden",
		"stale HypoMux-Tun device still exists",
	}
	for _, fragment := range required {
		if !strings.Contains(cleanupPowerShell, fragment) {
			t.Fatalf("cleanup command is missing %q", fragment)
		}
	}
	for _, forbidden := range []string{"taskkill", "/IM", "Remove-PnpDevice"} {
		if strings.Contains(cleanupPowerShell, forbidden) {
			t.Fatalf("cleanup command contains broad operation %q", forbidden)
		}
	}
}

func TestResolveWindowsPowerShellDoesNotDependOnPATH(t *testing.T) {
	originalPath := t.TempDir()
	t.Setenv("PATH", originalPath)
	path, err := resolveWindowsPowerShell()
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(path) || !strings.EqualFold(filepath.Base(path), "powershell.exe") {
		t.Fatalf("resolved PowerShell path = %q", path)
	}
}

func TestOwnedTunDeviceInspectionUsesSetupAPIWithoutElevation(t *testing.T) {
	if _, err := hasOwnedTunDevice(); err != nil {
		t.Fatalf("inspect owned TUN device: %v", err)
	}
}
