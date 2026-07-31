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
$pattern = 'meta|clash|mihomo|tun|wintun|wireguard|tailscale|vpn|tap'
$items = @(Get-NetRoute -AddressFamily IPv4 -DestinationPrefix '0.0.0.0/0' -ErrorAction Stop |
  Where-Object { $_.InterfaceAlias -match $pattern } |
  Select-Object -ExpandProperty InterfaceAlias -Unique)
ConvertTo-Json -InputObject @($items) -Compress
`
	command := exec.Command(
		"powershell.exe", "-NoProfile", "-NonInteractive",
		"-ExecutionPolicy", "Bypass", "-Command", script,
	)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	output, err := command.Output()
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
