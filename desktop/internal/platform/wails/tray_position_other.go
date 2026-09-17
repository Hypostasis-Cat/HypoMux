//go:build !windows

package wails

import "github.com/wailsapp/wails/v3/pkg/application"

func newTrayPositioner(window application.Window, tray *application.SystemTray) func() error {
	return func() error { return tray.PositionWindow(window, 8) }
}
