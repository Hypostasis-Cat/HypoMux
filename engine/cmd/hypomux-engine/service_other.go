//go:build !windows

package main

import (
	"fmt"
	"io"

	"github.com/Hypostasis-Cat/HypoMux/engine/internal/server"
)

func runWindowsService(stderr io.Writer, _ server.Metadata) int {
	fmt.Fprintln(stderr, "Windows Service mode is only available on Windows")
	return 2
}
