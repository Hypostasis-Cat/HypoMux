//go:build windows

package services

import (
	_ "embed"
	"encoding/base64"
	"encoding/binary"
	"os/exec"
	"unicode/utf16"
)

//go:embed hotspot_windows.ps1
var hotspotScript string

func hotspotSupported() bool { return true }

func hotspotCommand() (*exec.Cmd, error) {
	executable, err := resolveWindowsPowerShellExecutable()
	if err != nil {
		return nil, err
	}
	units := utf16.Encode([]rune(hotspotScript))
	encoded := make([]byte, len(units)*2)
	for i, unit := range units {
		binary.LittleEndian.PutUint16(encoded[i*2:], unit)
	}
	return exec.Command(executable, "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", base64.StdEncoding.EncodeToString(encoded)), nil
}
