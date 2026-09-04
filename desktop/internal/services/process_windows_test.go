//go:build windows

package services

import (
	"os/exec"
	"syscall"
	"testing"
)

func TestConfigureBackgroundCommand(t *testing.T) {
	for _, existing := range []*syscall.SysProcAttr{nil, {CreationFlags: 0x200}} {
		command := exec.Command("unused.exe")
		command.SysProcAttr = existing
		configureBackgroundCommand(command)
		if !command.SysProcAttr.HideWindow || command.SysProcAttr.CreationFlags&0x08000000 == 0 {
			t.Fatal("background command can display a console")
		}
		if existing != nil && command.SysProcAttr.CreationFlags&0x200 == 0 {
			t.Fatal("existing creation flags were discarded")
		}
	}
}
