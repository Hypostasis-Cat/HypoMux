//go:build !windows

package services

func inspectTunPlatform(checkWFP bool) tunPlatformSnapshot {
	detail := "Windows Filtering Platform is unavailable on this platform"
	if !checkWFP {
		detail = "strict route is disabled"
	}
	return tunPlatformSnapshot{
		WFPDetail:      detail,
		RouteScanError: "TUN route preflight is only implemented on Windows",
	}
}
