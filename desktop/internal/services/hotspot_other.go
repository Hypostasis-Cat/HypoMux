//go:build !windows

package services

import (
	"errors"
	"os/exec"
)

func hotspotSupported() bool { return false }
func hotspotCommand() (*exec.Cmd, error) {
	return nil, errors.New("聚合热点目前仅支持 Windows 10/11")
}
