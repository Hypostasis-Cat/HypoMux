package main

import (
	"os"

	desktopplatform "github.com/Hypostasis-Cat/HypoMux/desktop/internal/platform"
	"github.com/Hypostasis-Cat/HypoMux/desktop/internal/services"
	"github.com/Hypostasis-Cat/HypoMux/desktop/internal/setup"
)

// Installer entrypoints must retain the invoking installer's token. They do
// not create desktop windows or initialize WebView; runtime detection only
// reads registry metadata. Ordinary GUI startup still normalizes privileges.
func runNativeHelper(args []string) (int, bool) {
	if len(args) == 0 {
		return 0, false
	}
	switch args[0] {
	case "--setup-transaction":
		return setup.Run(args[1:], os.Stdout), true
	case "--run-update":
		return services.RunUpdateHelper(args[1:]), true
	case "--webview-check":
		available := false
		if len(args) > 1 && args[1] == "machine" {
			available = desktopplatform.WebView2MachineAvailable()
		} else {
			available = desktopplatform.WebView2Available()
		}
		if !available {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}
