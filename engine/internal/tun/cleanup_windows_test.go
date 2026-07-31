//go:build windows

package tun

import (
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
