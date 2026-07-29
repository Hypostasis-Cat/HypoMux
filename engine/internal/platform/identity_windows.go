//go:build windows

package platform

import (
	"os"

	"golang.org/x/sys/windows"
)

// CurrentIdentity reads the current Windows process token without changing it.
func CurrentIdentity() Identity {
	return Identity{
		ProcessID: os.Getpid(),
		Elevated:  windows.GetCurrentProcessToken().IsElevated(),
	}
}
