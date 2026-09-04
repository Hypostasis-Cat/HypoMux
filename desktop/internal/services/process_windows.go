//go:build windows

package services

import (
	"os/exec"
	"syscall"
)

// Background probes must not allocate a console when launched by the GUI.
func configureBackgroundCommand(command *exec.Cmd) {
	if command.SysProcAttr == nil {
		command.SysProcAttr = &syscall.SysProcAttr{}
	}
	command.SysProcAttr.HideWindow = true
	command.SysProcAttr.CreationFlags |= 0x08000000 // CREATE_NO_WINDOW
}
