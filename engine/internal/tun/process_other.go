//go:build !windows

package tun

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

func interruptProcess(_ context.Context, process *os.Process) error {
	return process.Signal(os.Interrupt)
}

func InterruptConsoleProcess(uint32) error {
	return fmt.Errorf("Windows console interruption is unavailable on this platform")
}

type noopContainment struct{}

func configureProcess(*exec.Cmd) {}

func containProcess(*os.Process) (processContainment, error) {
	return noopContainment{}, nil
}

func (noopContainment) Close() error {
	return nil
}
