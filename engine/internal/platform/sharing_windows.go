//go:build windows

package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
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

// Windows ICS can block for tens of seconds per connection while it queries
// some adapters (for example Remote NDIS USB tethering). Allow a slow but
// finishing enumeration, and run at most one at a time so a lingering query
// is never multiplied by the hotspot monitor's retries.
const (
	sharingInspectionTimeout   = 45 * time.Second
	sharingInspectionQueueWait = 50 * time.Second
)

var sharingInspectionSlot = make(chan struct{}, 1)

func InspectSharing(ctx context.Context) (SharingSnapshot, error) {
	queue := time.NewTimer(sharingInspectionQueueWait)
	defer queue.Stop()
	select {
	case sharingInspectionSlot <- struct{}{}:
	case <-ctx.Done():
		return SharingSnapshot{}, fmt.Errorf("inspect Windows sharing: canceled while waiting: %w", ctx.Err())
	case <-queue.C:
		return SharingSnapshot{}, fmt.Errorf("inspect Windows sharing: previous Windows ICS query still running after %s", sharingInspectionQueueWait)
	}
	defer func() { <-sharingInspectionSlot }()

	ctx, cancel := context.WithTimeout(ctx, sharingInspectionTimeout)
	defer cancel()
	system, err := windows.GetSystemDirectory()
	if err != nil {
		return SharingSnapshot{}, err
	}
	started := time.Now()
	command := exec.CommandContext(ctx, filepath.Join(system, "WindowsPowerShell", "v1.0", "powershell.exe"), "-NoProfile", "-NonInteractive", "-Command", sharingInspectionScript)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	var output, stderr bytes.Buffer
	command.Stdout, command.Stderr = &output, &stderr
	if err := command.Run(); err != nil {
		return SharingSnapshot{}, describeSharingFailure(ctx.Err(), err, stderr.String(), time.Since(started))
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

// A killed process reports only "exit status 1" with empty stderr. Name the
// timeout explicitly so support logs do not need an external probe.
func describeSharingFailure(contextErr, runErr error, stderr string, elapsed time.Duration) error {
	elapsed = elapsed.Round(100 * time.Millisecond)
	switch {
	case errors.Is(contextErr, context.DeadlineExceeded):
		return fmt.Errorf("inspect Windows sharing: Windows ICS did not finish enumerating connections within %s (slow adapters such as USB tethering can cause this)", sharingInspectionTimeout)
	case errors.Is(contextErr, context.Canceled):
		return fmt.Errorf("inspect Windows sharing: canceled after %s", elapsed)
	}
	detail := strings.TrimSpace(stderr)
	if detail == "" {
		detail = "no error output"
	}
	if len(detail) > 512 {
		detail = detail[:512] + "..."
	}
	return fmt.Errorf("inspect Windows sharing: %w: %s", runErr, detail)
}
