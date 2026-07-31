//go:build !windows

package services

import "runtime"

func wfpPlatformFingerprint() string {
	return runtime.GOOS
}
