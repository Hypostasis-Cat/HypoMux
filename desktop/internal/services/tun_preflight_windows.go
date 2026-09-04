//go:build windows

package services

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/Hypostasis-Cat/HypoMux/desktop/internal/engineclient"
	"golang.org/x/sys/windows"
)

const tunPreflightPowerShellTimeout = 4 * time.Second

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
	snapshot.DefaultRouteAliases, snapshot.NetworkRisks, snapshot.RouteScanError = inspectForeignNetworkState()
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

// inspectForeignNetworkState collects all PowerShell-only network evidence in
// one process. Starting Windows PowerShell dominated the read-only preflight;
// the old implementation paid that cold-start cost twice on every check.
func inspectForeignNetworkState() ([]string, []string, string) {
	const script = `
$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding($false)
$inspectionErrors = @()
$aliases = @()
$risks = @()
try {
  $routePattern = 'meta|clash|mihomo|tun|wintun|wireguard|tailscale|vpn|tap'
  $aliases = @(Get-NetRoute -AddressFamily IPv4 -DestinationPrefix '0.0.0.0/0' -ErrorAction Stop |
    Where-Object { $_.InterfaceAlias -match $routePattern } |
    Select-Object -ExpandProperty InterfaceAlias -Unique)
} catch {
  $inspectionErrors += ('默认路由检查失败：' + $_.Exception.Message)
}
try {
  $adapterPattern = '(?i)(tun|tap|wintun|wireguard|tailscale|vpn|virtual|vgate)'
  $adapters = @(Get-NetAdapter -ErrorAction Stop)
  foreach ($adapter in $adapters) {
    if ($adapter.Status -eq 'Up' -and $adapter.Name -ne 'HypoMux-Tun' -and
        ($adapter.Name -match $adapterPattern -or $adapter.InterfaceDescription -match $adapterPattern)) {
      $risks += ('active foreign virtual adapter: ' + $adapter.Name + ' [' + $adapter.InterfaceDescription + ']')
    }
  }
} catch {
  $inspectionErrors += ('虚拟网卡检查失败：' + $_.Exception.Message)
}
try {
  $forwarding = @(Get-NetIPInterface -AddressFamily IPv4 -ErrorAction Stop |
    Where-Object { $_.Forwarding -eq 'Enabled' -and $_.InterfaceAlias -ne 'HypoMux-Tun' })
  foreach ($item in $forwarding) {
    $risks += ('IPv4 forwarding enabled: ' + $item.InterfaceAlias)
  }
} catch {
  $inspectionErrors += ('IPv4 转发检查失败：' + $_.Exception.Message)
}
try {
  $ics = Get-Service -Name SharedAccess -ErrorAction SilentlyContinue
  if ($null -ne $ics -and $ics.Status -eq 'Running') {
    $risks += 'Internet Connection Sharing service is running'
  }
} catch {
  $inspectionErrors += ('网络共享检查失败：' + $_.Exception.Message)
}
ConvertTo-Json -InputObject @{
  aliases = @($aliases)
  risks = @($risks)
  errors = @($inspectionErrors)
} -Compress
`
	output, err := runPreflightPowerShell(script)
	if err != nil {
		return []string{}, []string{}, fmt.Sprintf("网络接管风险检查失败：%v", err)
	}
	var payload struct {
		Aliases []string `json:"aliases"`
		Risks   []string `json:"risks"`
		Errors  []string `json:"errors"`
	}
	if err := json.Unmarshal(output, &payload); err != nil {
		return []string{}, []string{}, fmt.Sprintf("网络接管风险检查结果无效：%v", err)
	}
	aliases := make([]string, 0, len(payload.Aliases))
	seen := map[string]struct{}{}
	for _, alias := range payload.Aliases {
		value := strings.TrimSpace(alias)
		key := strings.ToLower(value)
		if value == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		aliases = append(aliases, value)
	}
	risks := make([]string, 0, len(payload.Risks))
	seen = make(map[string]struct{}, len(payload.Risks))
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
	errors := make([]string, 0, len(payload.Errors))
	for _, value := range payload.Errors {
		if value = strings.TrimSpace(value); value != "" {
			errors = append(errors, value)
		}
	}
	return aliases, risks, strings.Join(errors, "；")
}

func runPreflightPowerShell(script string) ([]byte, error) {
	powerShell, err := resolveWindowsPowerShellExecutable()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), tunPreflightPowerShellTimeout)
	defer cancel()
	command := exec.CommandContext(
		ctx,
		powerShell, "-NoProfile", "-NonInteractive",
		"-ExecutionPolicy", "Bypass", "-Command", script,
	)
	configureBackgroundCommand(command)
	output, err := command.Output()
	if ctx.Err() != nil {
		return nil, fmt.Errorf("PowerShell 网络检查超过 %s：%w", tunPreflightPowerShellTimeout, ctx.Err())
	}
	return output, err
}
