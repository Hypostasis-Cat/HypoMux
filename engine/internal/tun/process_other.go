//go:build !windows

package tun

import (
	"os"
	"os/exec"
)

type noopContainment struct{}

func configureProcess(*exec.Cmd) {}

func containProcess(*os.Process) (processContainment, error) {
	return noopContainment{}, nil
}

func (noopContainment) Close() error {
	return nil
}
