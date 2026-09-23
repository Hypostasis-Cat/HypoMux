//go:build windows

package wails

import "github.com/wailsapp/wails/v3/pkg/application"

func newTrayPositioner(window application.Window, tray *application.SystemTray) func() error {
	// A touch press does not move the mouse cursor. Anchor the popup to the
	// tray icon instead of GetCursorPos, which may still be at screen center.
	return func() error { return tray.PositionWindow(window, 8) }
}
