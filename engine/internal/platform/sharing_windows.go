//go:build windows

package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

const sharingInspectionScript = `
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding($false)
function Read-SharedConnections($sharing) {
    foreach ($connection in $sharing.EnumEveryConnection()) {
        $entry = $sharing.INetSharingConfigurationForINetConnection($connection)
        if ($entry.SharingEnabled) {
            $properties = $sharing.NetConnectionProps($connection)
            [PSCustomObject]@{ guid = ([guid]$properties.Guid).ToString(); name = [string]$properties.Name; role = [int]$entry.SharingConnectionType }
        }
    }
}
$sharing = New-Object -ComObject HNetCfg.HNetShare
@{ connections = @(Read-SharedConnections $sharing) } | ConvertTo-Json -Depth 4 -Compress
`

func InspectSharing(ctx context.Context) (SharingSnapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	system, err := windows.GetSystemDirectory()
	if err != nil {
		return SharingSnapshot{}, err
	}
	command := exec.CommandContext(ctx, filepath.Join(system, "WindowsPowerShell", "v1.0", "powershell.exe"), "-NoProfile", "-NonInteractive", "-Command", sharingInspectionScript)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	var output, stderr bytes.Buffer
	command.Stdout, command.Stderr = &output, &stderr
	if err := command.Run(); err != nil {
		return SharingSnapshot{}, fmt.Errorf("inspect Windows sharing: %w: %s", err, stderr.String())
	}
	var result SharingSnapshot
	if err := json.Unmarshal(bytes.TrimPrefix(output.Bytes(), []byte{0xef, 0xbb, 0xbf}), &result); err != nil {
		return SharingSnapshot{}, fmt.Errorf("decode Windows sharing: %w", err)
	}
	if result.Connections == nil {
		result.Connections = []SharingConnection{}
	}
	return result, nil
}
