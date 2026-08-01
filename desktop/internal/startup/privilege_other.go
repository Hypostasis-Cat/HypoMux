//go:build !windows

package startup

// PrepareDesktopLaunch is a no-op outside Windows, where the WebView2 host
// privilege boundary does not apply.
func PrepareDesktopLaunch([]string) DesktopLaunchSecurity {
	return DesktopLaunchSecurity{}
}
