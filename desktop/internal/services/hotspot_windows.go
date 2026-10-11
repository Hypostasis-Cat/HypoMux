//go:build windows

package services

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"os/exec"
)

//go:embed hotspot_windows.ps1
var hotspotScript string

func hotspotSupported() bool { return true }

const hotspotAdaptersScript = `$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding($false)
$adapters = @(Get-NetAdapter -Physical | Where-Object { $_.InterfaceType -eq 71 -or $_.NdisPhysicalMedium -in @(1, 9) } | Sort-Object Name | ForEach-Object {
    @{ id = ([guid]$_.InterfaceGuid).ToString('B'); name = [string]$_.Name; description = [string]$_.InterfaceDescription; status = [string]$_.Status }
})
ConvertTo-Json -InputObject $adapters -Compress
`

func listHotspotAdapters(ctx context.Context) ([]HotspotAdapter, error) {
	executable, err := resolveWindowsPowerShellExecutable()
	if err != nil {
		return nil, err
	}
	command := exec.CommandContext(ctx, executable, "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", hotspotAdaptersScript)
	configureBackgroundCommand(command)
	output, err := command.Output()
	if err != nil {
		return nil, errors.New("无法读取热点发射网卡，请确认 WLAN 服务已启动后刷新")
	}
	var adapters []HotspotAdapter
	if json.Unmarshal(output, &adapters) != nil {
		return nil, errors.New("热点发射网卡列表无效，请刷新重试")
	}
	return adapters, nil
}

func hotspotCommand() (*exec.Cmd, error) {
	executable, err := resolveWindowsPowerShellExecutable()
	if err != nil {
		return nil, err
	}
	// exec passes UTF-16 arguments directly to CreateProcess. Base64-encoding
	// UTF-16 inflated the fixed script beyond Windows' 32K command-line limit.
	// Credentials remain exclusively on stdin, never interpolated into code.
	return exec.Command(executable, "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", hotspotScript), nil
}
