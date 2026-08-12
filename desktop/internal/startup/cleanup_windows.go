//go:build windows

package startup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

	taskkill, err := resolveStartupSystemExecutable("taskkill.exe")
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, taskkill, "/F", "/IM", processName, "/T")
	// Suppress window creation on Windows
	hideWindow(cmd)

	output, err := cmd.CombinedOutput()
	if err != nil {
		outputStr := string(output)
		// taskkill reports "no matching task" via exit code 128 regardless of
		// UI language; text matching alone would miss localized variants
		// (e.g. zh-CN prints the message in CP936). A missing process is the
		// expected steady state and must not surface as a startup error.
		var exitErr *exec.ExitError
		if (errors.As(err, &exitErr) && exitErr.ExitCode() == 128) ||
			strings.Contains(outputStr, "not found") {
			return nil
		}
		if strings.TrimSpace(outputStr) == "" {
			// A context deadline can kill taskkill before it writes anything.
			outputStr = err.Error()
		}
		return fmt.Errorf("kill %s: %s", processName, strings.TrimSpace(outputStr))
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
$ErrorActionPreference = 'Stop'
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
  $pnputil = Join-Path $env:SystemRoot 'System32\pnputil.exe'
  foreach ($device in $targets) {
    & $pnputil /remove-device $device.InstanceId 2>&1 | Out-Null
  }
}
$deadline = [DateTime]::UtcNow.AddSeconds(8)
do {
  $remainingDevices = @(Get-PnpDevice -Class Net -ErrorAction SilentlyContinue |
    Where-Object {
      $_.FriendlyName -eq 'HypoMux-Tun' -and $_.InstanceId -like '*WINTUN*'
    })
  $remainingAdapters = @(Get-NetAdapter -IncludeHidden -ErrorAction SilentlyContinue |
    Where-Object { $_.Name -eq 'HypoMux-Tun' })
  if ($remainingDevices.Count -eq 0 -and $remainingAdapters.Count -eq 0) {
    break
  }
  Start-Sleep -Milliseconds 200
} while ([DateTime]::UtcNow -lt $deadline)
if ($remainingDevices.Count -gt 0 -or $remainingAdapters.Count -gt 0) {
  $deviceIds = @($remainingDevices | Select-Object -ExpandProperty InstanceId)
  throw ('stale HypoMux-Tun device still exists: ' + ($deviceIds -join ', '))
}
if ($targets.Count -gt 0) {
  Start-Sleep -Milliseconds 300
}
`

	powerShell, err := resolveStartupPowerShell()
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, powerShell, "-NoProfile", "-NonInteractive", "-Command", script)
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

func resolveStartupPowerShell() (string, error) {
	for _, root := range startupWindowsRoots() {
		for _, directory := range []string{"System32", "Sysnative"} {
			candidate := filepath.Join(root, directory, "WindowsPowerShell", "v1.0", "powershell.exe")
			if startupRegularFile(candidate) {
				return filepath.Abs(candidate)
			}
		}
	}
	if path, err := exec.LookPath("powershell.exe"); err == nil && startupRegularFile(path) {
		return filepath.Abs(path)
	}
	return "", errors.New("Windows PowerShell is unavailable from System32, Sysnative, and PATH")
}

func resolveStartupSystemExecutable(name string) (string, error) {
	name = filepath.Base(strings.TrimSpace(name))
	for _, root := range startupWindowsRoots() {
		for _, directory := range []string{"System32", "Sysnative"} {
			candidate := filepath.Join(root, directory, name)
			if startupRegularFile(candidate) {
				return filepath.Abs(candidate)
			}
		}
	}
	if path, err := exec.LookPath(name); err == nil && startupRegularFile(path) {
		return filepath.Abs(path)
	}
	return "", fmt.Errorf("%s is unavailable from System32, Sysnative, and PATH", name)
}

func startupWindowsRoots() []string {
	result := make([]string, 0, 2)
	seen := map[string]struct{}{}
	for _, root := range []string{os.Getenv("SystemRoot"), os.Getenv("WINDIR")} {
		root = filepath.Clean(strings.TrimSpace(root))
		if root == "" || root == "." {
			continue
		}
		key := strings.ToLower(root)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, root)
	}
	return result
}

func startupRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
