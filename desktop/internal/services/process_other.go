//go:build !windows

package services

import "os/exec"

func configureBackgroundCommand(command *exec.Cmd) {}
