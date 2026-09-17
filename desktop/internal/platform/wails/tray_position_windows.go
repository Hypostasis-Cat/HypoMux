//go:build windows

package wails

import (
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/w32"
)

func newTrayPositioner(window application.Window, tray *application.SystemTray) func() error {
	x, y, ok := w32.GetCursorPos()
	if !ok {
		return func() error { return tray.PositionWindow(window, 8) }
	}
	// Keep the original click as the anchor when translated text or errors
	// resize the menu; following the cursor would make the menu jump.
	physical := application.Point{X: x, Y: y}
	return func() error {
		return application.InvokeSyncWithError(func() error {
			screen := application.ScreenNearestPhysicalPoint(physical)
			anchor := application.PhysicalToDipPoint(physical)
			width, height := window.Size()
			position := trayMenuPosition(anchor, screen.WorkArea, width, height)
			window.SetPosition(position.X, position.Y)
			return nil
		})
	}
}
