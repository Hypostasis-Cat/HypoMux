//go:build windows

package tun

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

const cleanupPowerShell = `
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

func cleanupPlatform(ctx context.Context) error {
	// A clean stopped state has no owned PnP device. SetupAPI is intentionally
	// queried without DIGCF_PRESENT so hidden, disabled, and non-present ghost
	// devices are included. This fast in-process check can safely avoid a
	// PowerShell startup while still sending genuine stale Wintun state through
	// the complete route/PnP cleanup below. On inspection errors we fail safe by
	// retaining the full cleanup path.
	if present, inspectErr := hasOwnedTunDevice(); inspectErr == nil && !present {
		return nil
	}
	powerShell, err := resolveWindowsPowerShell()
	if err != nil {
		return err
	}
	command := exec.CommandContext(
		ctx,
		powerShell,
		"-NoProfile",
		"-NonInteractive",
		"-Command",
		cleanupPowerShell,
	)
	configureProcess(command)
	output := &tailBuffer{limit: 32 * 1024}
	command.Stdout = output
	command.Stderr = output
	err = command.Run()
	if err == nil {
		return nil
	}
	detail := strings.TrimSpace(output.String())
	if detail == "" {
		detail = err.Error()
	}
	return fmt.Errorf("PowerShell cleanup failed: %s", detail)
}

var networkDeviceClassGUID = &windows.GUID{
	Data1: 0x4d36e972,
	Data2: 0xe325,
	Data3: 0x11ce,
	Data4: [8]byte{0xbf, 0xc1, 0x08, 0x00, 0x2b, 0xe1, 0x03, 0x18},
}

func hasOwnedTunDevice() (bool, error) {
	devices, err := windows.SetupDiGetClassDevsEx(
		networkDeviceClassGUID, "", 0, 0, 0, "",
	)
	if err != nil {
		return false, err
	}
	defer devices.Close()
	for index := 0; ; index++ {
		device, enumErr := devices.EnumDeviceInfo(index)
		if errors.Is(enumErr, windows.ERROR_NO_MORE_ITEMS) {
			return false, nil
		}
		if enumErr != nil {
			continue
		}
		value, propertyErr := devices.DeviceRegistryProperty(device, windows.SPDRP_FRIENDLYNAME)
		if propertyErr != nil {
			value, propertyErr = devices.DeviceRegistryProperty(device, windows.SPDRP_DEVICEDESC)
		}
		if propertyErr != nil {
			continue
		}
		name, ok := value.(string)
		if !ok || !strings.EqualFold(strings.TrimSpace(name), tunInterfaceName) {
			continue
		}
		instanceID, idErr := devices.DeviceInstanceID(device)
		if idErr != nil {
			return false, idErr
		}
		if strings.Contains(strings.ToUpper(instanceID), "WINTUN") {
			return true, nil
		}
	}
}

func resolveWindowsPowerShell() (string, error) {
	seen := map[string]struct{}{}
	candidates := make([]string, 0, 5)
	for _, root := range []string{os.Getenv("SystemRoot"), os.Getenv("WINDIR")} {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		candidates = append(candidates,
			filepath.Join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe"),
			filepath.Join(root, "Sysnative", "WindowsPowerShell", "v1.0", "powershell.exe"),
		)
	}
	if path, err := exec.LookPath("powershell.exe"); err == nil {
		candidates = append(candidates, path)
	}
	for _, candidate := range candidates {
		candidate = filepath.Clean(candidate)
		key := strings.ToLower(candidate)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		info, statErr := os.Stat(candidate)
		if statErr == nil && info.Mode().IsRegular() {
			absolute, absoluteErr := filepath.Abs(candidate)
			if absoluteErr == nil {
				return absolute, nil
			}
		}
	}
	return "", errors.New("Windows PowerShell is unavailable from System32, Sysnative, and PATH")
}
