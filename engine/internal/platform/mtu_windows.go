//go:build windows

package platform

import (
	"context"
	"fmt"
	"golang.org/x/sys/windows"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

func SetMTU(ctx context.Context, p MTUChange) error {
	if err := p.Validate(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	system, err := windows.GetSystemDirectory()
	if err != nil {
		return err
	}
	// GUID is validated as hexadecimal; all remaining parameters are integers.
	script := fmt.Sprintf(`$ErrorActionPreference='Stop'; Set-StrictMode -Version Latest; $i=%d; $a=Get-NetAdapter -InterfaceIndex $i; if (([guid]$a.InterfaceGuid).ToString() -ne '%s' -or $a.Name -eq 'HypoMux-Tun') { throw 'Adapter changed' }; $n=Get-NetIPInterface -InterfaceIndex $i -AddressFamily IPv4 -PolicyStore ActiveStore; if ($n.NlMtu -ne %d) { throw 'MTU changed; refresh first' }; Set-NetIPInterface -InterfaceIndex $i -AddressFamily IPv4 -NlMtuBytes %d -PolicyStore ActiveStore; $n=Get-NetIPInterface -InterfaceIndex $i -AddressFamily IPv4 -PolicyStore ActiveStore; if ($n.NlMtu -ne %d) { throw 'MTU verification failed' }`, p.IfIndex, p.GUID, p.Expected, p.Value, p.Value)
	cmd := exec.CommandContext(ctx, filepath.Join(system, "WindowsPowerShell", "v1.0", "powershell.exe"), "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("修改 MTU 失败：%w: %s", err, output)
	}
	return nil
}
