//go:build !windows

package main

import (
	"errors"
	"fmt"
	"io"

	"github.com/Hypostasis-Cat/HypoMux/engine/internal/server"
)

func installWindowsService() error {
	return errors.New("Windows Service installation is only available on Windows")
}

func removeWindowsService() error {
	return errors.New("Windows Service removal is only available on Windows")
}

func runWindowsService(stderr io.Writer, _ server.Metadata) int {
	fmt.Fprintln(stderr, "Windows Service mode is only available on Windows")
	return 2
}
