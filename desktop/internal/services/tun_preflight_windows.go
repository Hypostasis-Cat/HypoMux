//go:build windows

package services

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"unsafe"

	"github.com/Hypostasis-Cat/HypoMux/desktop/internal/engineclient"
	"golang.org/x/sys/windows"
)

func inspectTunPlatform(checkWFP bool) tunPlatformSnapshot {
	snapshot := tunPlatformSnapshot{
		HostElevated:             windows.GetCurrentProcessToken().IsElevated(),
		PrivilegeBrokerAvailable: engineclient.PrivilegedLaunchSupported(),
	}
	if checkWFP {
		snapshot.WFPReady, snapshot.WFPDetail = probeWFPEngine()
	} else {
		snapshot.WFPDetail = "严格路由已由用户关闭；未执行 WFP 探测"
	}
	snapshot.DefaultRouteAliases, snapshot.RouteScanError = foreignDefaultRouteAliases()
	snapshot.NetworkRisks, snapshot.RouteScanError = appendForeignNetworkRisks(
		snapshot.NetworkRisks, snapshot.RouteScanError,
	)
	return snapshot
}

func probeWFPEngine() (bool, string) {
	library := windows.NewLazySystemDLL("fwpuclnt.dll")
	open := library.NewProc("FwpmEngineOpen0")
	closeEngine := library.NewProc("FwpmEngineClose0")
	var handle windows.Handle
	status, _, _ := open.Call(
		0,
		uintptr(^uint32(0)),
		0,
		0,
		uintptr(unsafe.Pointer(&handle)),
	)
	if status != 0 {
		return false, fmt.Sprintf(
			"FwpmEngineOpen0 failed (0x%08X): %s",
			uint32(status), syscall.Errno(status).Error(),
		)
	}
	if handle != 0 {
		_, _, _ = closeEngine.Call(uintptr(handle))
	}
	return true, "FwpmEngineOpen0 succeeded"
}

func foreignDefaultRouteAliases() ([]string, string) {
	const script = `
$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding($false)
$pattern = 'meta|clash|mihomo|tun|wintun|wireguard|tailscale|vpn|tap'
$items = @(Get-NetRoute -AddressFamily IPv4 -DestinationPrefix '0.0.0.0/0' -ErrorAction Stop |
  Where-Object { $_.InterfaceAlias -match $pattern } |
  Select-Object -ExpandProperty InterfaceAlias -Unique)
ConvertTo-Json -InputObject @($items) -Compress
`
	output, err := runPreflightPowerShell(script)
	if err != nil {
		return []string{}, fmt.Sprintf("默认路由检查失败：%v", err)
	}
	var aliases []string
	if err := json.Unmarshal(output, &aliases); err != nil {
		return []string{}, fmt.Sprintf("默认路由检查结果无效：%v", err)
	}
	result := make([]string, 0, len(aliases))
	seen := map[string]struct{}{}
	for _, alias := range aliases {
		value := strings.TrimSpace(alias)
		key := strings.ToLower(value)
		if value == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result, ""
}

func appendForeignNetworkRisks(risks []string, existingError string) ([]string, string) {
	const script = `
$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding($false)
$pattern = '(?i)(tun|tap|wintun|wireguard|tailscale|vpn|virtual|vgate)'
$risks = @()
$adapters = @(Get-NetAdapter -ErrorAction Stop)
foreach ($adapter in $adapters) {
  if ($adapter.Status -eq 'Up' -and $adapter.Name -ne 'HypoMux-Tun' -and
      ($adapter.Name -match $pattern -or $adapter.InterfaceDescription -match $pattern)) {
    $risks += ('active foreign virtual adapter: ' + $adapter.Name + ' [' + $adapter.InterfaceDescription + ']')
  }
}
$routes = @(Get-NetRoute -AddressFamily IPv4 -DestinationPrefix '0.0.0.0/0' -ErrorAction Stop)
if ($routes.Count -gt 1) {
  $aliases = @($routes | ForEach-Object { $_.InterfaceAlias + ' metric=' + $_.RouteMetric })
  $risks += ('multiple IPv4 default routes: ' + ($aliases -join ', '))
}
foreach ($route in $routes) {
  if ($route.InterfaceAlias -ne 'HypoMux-Tun' -and [int]$route.RouteMetric -le 5) {
    $risks += ('low-metric IPv4 default route: ' + $route.InterfaceAlias + ' metric=' + $route.RouteMetric)
  }
}
$forwarding = @(Get-NetIPInterface -AddressFamily IPv4 -ErrorAction Stop |
  Where-Object { $_.Forwarding -eq 'Enabled' -and $_.InterfaceAlias -ne 'HypoMux-Tun' })
foreach ($item in $forwarding) {
  $risks += ('IPv4 forwarding enabled: ' + $item.InterfaceAlias)
}
$ics = Get-Service -Name SharedAccess -ErrorAction SilentlyContinue
if ($null -ne $ics -and $ics.Status -eq 'Running') {
  $risks += 'Internet Connection Sharing service is running'
}
ConvertTo-Json -InputObject @{ risks = @($risks) } -Compress
`
	output, err := runPreflightPowerShell(script)
	if err != nil {
		if existingError == "" {
			existingError = fmt.Sprintf("网络接管风险检查失败：%v", err)
		} else {
			existingError += fmt.Sprintf("；网络接管风险检查失败：%v", err)
		}
		return risks, existingError
	}
	var payload struct {
		Risks []string `json:"risks"`
	}
	if err := json.Unmarshal(output, &payload); err != nil {
		if existingError == "" {
			existingError = fmt.Sprintf("网络接管风险检查结果无效：%v", err)
		} else {
			existingError += fmt.Sprintf("；网络接管风险检查结果无效：%v", err)
		}
		return risks, existingError
	}
	seen := make(map[string]struct{}, len(risks)+len(payload.Risks))
	for _, value := range risks {
		seen[strings.ToLower(strings.TrimSpace(value))] = struct{}{}
	}
	for _, value := range payload.Risks {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		risks = append(risks, value)
	}
	return risks, existingError
}

func runPreflightPowerShell(script string) ([]byte, error) {
	powerShell, err := resolveWindowsPowerShellExecutable()
	if err != nil {
		return nil, err
	}
	command := exec.Command(
		powerShell, "-NoProfile", "-NonInteractive",
		"-ExecutionPolicy", "Bypass", "-Command", script,
	)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return command.Output()
}
