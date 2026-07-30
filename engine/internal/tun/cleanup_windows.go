//go:build windows

package tun

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

const cleanupPowerShell = `
$routes = @(Get-NetRoute -ErrorAction SilentlyContinue |
  Where-Object {
    $_.InterfaceAlias -eq 'HypoMux-Tun' -and
    ($_.DestinationPrefix -eq '0.0.0.0/0' -or $_.DestinationPrefix -eq '::/0')
  })
foreach ($route in $routes) {
  $route | Remove-NetRoute -Confirm:$false -ErrorAction SilentlyContinue
}
$targets = @(Get-PnpDevice -Class Net -ErrorAction SilentlyContinue |
  Where-Object {
    $_.FriendlyName -eq 'HypoMux-Tun' -and $_.InstanceId -like '*WINTUN*'
  })
if ($targets.Count -gt 0) {
  $targets | Disable-PnpDevice -Confirm:$false -ErrorAction SilentlyContinue
  Start-Sleep -Milliseconds 800
  foreach ($device in $targets) {
    pnputil /remove-device $device.InstanceId 2>&1 | Out-Null
  }
}
`

func cleanupPlatform(ctx context.Context) error {
	command := exec.CommandContext(
		ctx,
		"powershell",
		"-NoProfile",
		"-NonInteractive",
		"-Command",
		cleanupPowerShell,
	)
	configureProcess(command)
	output := &tailBuffer{limit: 32 * 1024}
	command.Stdout = output
	command.Stderr = output
	err := command.Run()
	if err == nil {
		return nil
	}
	detail := strings.TrimSpace(output.String())
	if detail == "" {
		detail = err.Error()
	}
	return fmt.Errorf("PowerShell cleanup failed: %s", detail)
}
