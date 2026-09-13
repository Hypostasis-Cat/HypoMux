//go:build windows

package services

import (
	_ "embed"
	"os/exec"
)

//go:embed hotspot_windows.ps1
var hotspotScript string

func hotspotSupported() bool { return true }

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
