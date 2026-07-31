//go:build !windows

package platform

import "os"

// CurrentIdentity keeps protocol tests and tooling portable. HypoMux itself is
// Windows-only, so elevation is meaningful only in identity_windows.go.
func CurrentIdentity() Identity {
	return Identity{
		ProcessID: os.Getpid(),
		Elevated:  false,
	}
}
