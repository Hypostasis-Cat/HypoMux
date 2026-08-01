package startup

import "strings"

const standardUIRelaunchArgument = "--hypomux-standard-ui-relaunch"

// DesktopLaunchSecurity describes the privilege boundary established before
// Wails, WebView2, startup cleanup, or the single-instance mutex is created.
type DesktopLaunchSecurity struct {
	Relaunched           bool
	Elevated             bool
	ProxyCompatible      bool
	Detail               string
	LegacyTaskRepairNote string
}

func hasStandardUIRelaunchArgument(arguments []string) bool {
	for _, argument := range arguments {
		if strings.EqualFold(strings.TrimSpace(argument), standardUIRelaunchArgument) {
			return true
		}
	}
	return false
}

func standardUIRelaunchArguments(arguments []string) []string {
	result := make([]string, 0, len(arguments)+1)
	for _, argument := range arguments {
		if strings.EqualFold(strings.TrimSpace(argument), standardUIRelaunchArgument) {
			continue
		}
		result = append(result, argument)
	}
	return append(result, standardUIRelaunchArgument)
}

func combineLaunchDetails(values ...string) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		if clean := strings.TrimSpace(value); clean != "" {
			parts = append(parts, clean)
		}
	}
	return strings.Join(parts, "; ")
}
