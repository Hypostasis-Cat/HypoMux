package wails

import "github.com/wailsapp/wails/v3/pkg/application"

// Coordinates and dimensions are all logical pixels, including on mixed-DPI desktops.
func trayMenuPosition(anchor application.Point, work application.Rect, width, height int) application.Point {
	const gap = 2 // The WebView also has 6px of transparent padding.
	x, y := anchor.X+gap, anchor.Y-height-gap
	if x+width > work.X+work.Width {
		x = anchor.X - width - gap
	}
	if y < work.Y {
		y = anchor.Y + gap
	}
	return application.Point{
		X: max(work.X, min(x, work.X+work.Width-width)),
		Y: max(work.Y, min(y, work.Y+work.Height-height)),
	}
}
