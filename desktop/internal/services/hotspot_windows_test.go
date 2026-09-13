//go:build windows

package services

import (
	"context"
	"net"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestHotspotWindowsSharingVerdictAndPrivateReadiness(t *testing.T) {
	executable, err := resolveWindowsPowerShellExecutable()
	if err != nil {
		t.Fatal(err)
	}
	// Execute the production functions against fake adapters, without WinRT,
	// Core, COM, or changes to any network adapter.
	functions := strings.Split(strings.ReplaceAll(hotspotScript, "\r\n", "\n"), "\ntry {\n")[0]
	script := functions + `
$publicID = [guid]'00000000-0000-0000-0000-000000000001'
$script:privateID = [guid]'00000000-0000-0000-0000-000000000002'
$otherID = [guid]'00000000-0000-0000-0000-000000000003'
if ((Get-SharingVerdict @() $publicID) -ne 'unobserved') { throw 'empty sharing must be inconclusive' }
$public = [PSCustomObject]@{ Guid = $publicID; Role = 0 }
$private = [PSCustomObject]@{ Guid = $script:privateID; Role = 1 }
if ((Get-SharingVerdict @($public, $private) $publicID) -ne 'verified') { throw 'matching pair rejected' }
$public.Guid = $otherID
if ((Get-SharingVerdict @($public, $private) $publicID) -ne 'mismatch') { throw 'wrong public accepted' }
$public.Guid = $publicID
$private.Guid = $otherID
if ((Get-SharingVerdict @($public, $private) $publicID) -ne 'mismatch') { throw 'wrong private accepted' }
if ((Get-SharingVerdict @($public) $publicID) -ne 'mismatch') { throw 'partial sharing accepted' }
$manager = [PSCustomObject]@{ TetheringOperationalState = 'On'; ClientCount = 0 }
$fakeAddress = '192.168.137.1'
$fakeState = 'Up'
function Get-NetAdapter { param([switch]$IncludeHidden) [PSCustomObject]@{ InterfaceDescription = 'Microsoft Wi-Fi Direct Virtual Adapter'; Status = $fakeState; InterfaceGuid = '00000000-0000-0000-0000-000000000002'; ifIndex = 23 } }
function Get-NetIPAddress { param($InterfaceIndex, $AddressFamily, $ErrorAction) [PSCustomObject]@{ AddressState = 'Preferred'; IPAddress = $fakeAddress } }
$script:privateID = $null
if (-not (Test-PrivateNetwork)) { throw 'new operational AP rejected' }
if ($script:gatewayAddress -ne '192.168.137.1') { throw 'gateway missing' }
$fakeAddress = '169.254.1.2'
if (Test-PrivateNetwork) { throw 'APIPA accepted' }
$fakeAddress = '192.168.137.1'
$fakeState = 'Disconnected'
if (Test-PrivateNetwork) { throw 'disconnected AP accepted' }
$fakeState = 'Up'
$script:privateID = $null
$script:previousPrivateIDs = @([guid]'00000000-0000-0000-0000-000000000002')
if (Test-PrivateNetwork) { throw 'pre-existing AP accepted' }
$manager.TetheringOperationalState = 'Off'
if (Test-PrivateNetwork) { throw 'stopped hotspot accepted' }
`
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "-NoProfile", "-NonInteractive", "-Command", script)
	configureBackgroundCommand(command)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, output)
	}
}

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
