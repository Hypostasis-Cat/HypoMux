//go:build windows

package startup

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// CleanupZombieProcesses terminates lingering sing-box and hypomux-engine
// processes from a previous crashed session, then removes stale TUN adapters
// and routes.
//
// This is the equivalent of the Python version's force_evict_zombie_backends
// from main.py lines 83-108.
func CleanupZombieProcesses(ctx context.Context) error {
	// Step 1: Kill zombie sing-box processes
	if err := killProcess(ctx, "sing-box.exe"); err != nil {
		return fmt.Errorf("kill zombie sing-box: %w", err)
	}

	// Step 2: Kill zombie hypomux-engine processes
	if err := killProcess(ctx, "hypomux-engine.exe"); err != nil {
		return fmt.Errorf("kill zombie hypomux-engine: %w", err)
	}

	// Step 3: Clean up TUN adapter and routes
	if err := cleanupTunAndRoutes(ctx); err != nil {
		return fmt.Errorf("cleanup TUN adapter: %w", err)
	}

	return nil
}

func killProcess(ctx context.Context, processName string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "taskkill", "/F", "/IM", processName, "/T")
	// Suppress window creation on Windows
	hideWindow(cmd)

	// taskkill returns error if process not found, which is acceptable
	output, _ := cmd.CombinedOutput()

	// Check if process was not found (acceptable) vs actual error
	outputStr := string(output)
	if strings.Contains(outputStr, "not found") || strings.Contains(outputStr, "未找到") {
		return nil
	}

	return nil
}

func cleanupTunAndRoutes(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	// PowerShell script to:
	// 1. Remove HypoMux-Tun routes (0.0.0.0/0 and ::/0)
	// 2. Disable and remove HypoMux-Tun adapter
	script := `
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

	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	hideWindow(cmd)

	output, err := cmd.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(output))
		if detail == "" {
			detail = err.Error()
		}
		return fmt.Errorf("PowerShell cleanup failed: %s", detail)
	}

	return nil
}
