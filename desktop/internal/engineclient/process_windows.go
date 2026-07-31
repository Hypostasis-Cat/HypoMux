//go:build windows

package engineclient

import (
	"os/exec"
	"syscall"
)

func configureCommand(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000,
	}
}
