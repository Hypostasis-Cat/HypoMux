//go:build windows

package services

import (
	"fmt"

	"golang.org/x/sys/windows"
)

func wfpPlatformFingerprint() string {
	version := windows.RtlGetVersion()
	return fmt.Sprintf(
		"windows-%d.%d.%d-%d",
		version.MajorVersion,
		version.MinorVersion,
		version.BuildNumber,
		version.ServicePackMajor,
	)
}
