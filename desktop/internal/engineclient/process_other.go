//go:build !windows

package engineclient

import "os/exec"

func configureCommand(_ *exec.Cmd) {}
