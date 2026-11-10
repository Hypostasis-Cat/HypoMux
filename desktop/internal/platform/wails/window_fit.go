package wails

// Main window geometry in DIPs. The preferred size suits a 1080p display at
// 100–125 % scaling; fitWindowToWorkArea shrinks it on smaller or more heavily
// scaled displays so the frameless window never opens past the screen edge.
const (
	MainWindowWidth     = 1120
	MainWindowHeight    = 800
	MainWindowMinWidth  = 960
	MainWindowMinHeight = 680

	// Keep a little of the desktop visible around the window so it reads as a
	// window rather than a maximised one, and the taskbar edge stays clear.
	windowWorkAreaMargin = 24
	// Below this the shell is unusable; the WebView scrolls instead (app.css).
	windowFloorWidth  = 640
	windowFloorHeight = 480
)

type windowGeometry struct {
	Width, Height       int
	MinWidth, MinHeight int
}

// fitWindowToWorkArea returns a size and minimum size that fit inside a work
// area of workWidth × workHeight DIPs. It only ever shrinks: a window that
// already fits keeps its current size. A non-positive work area (screen lookup
// failed) leaves the geometry untouched.
func fitWindowToWorkArea(current windowGeometry, workWidth, workHeight int) windowGeometry {
	if workWidth <= 0 || workHeight <= 0 {
		return current
	}
	maxWidth := max(workWidth-2*windowWorkAreaMargin, min(workWidth, windowFloorWidth))
	maxHeight := max(workHeight-2*windowWorkAreaMargin, min(workHeight, windowFloorHeight))

	fitted := current
	fitted.MinWidth = min(current.MinWidth, maxWidth)
	fitted.MinHeight = min(current.MinHeight, maxHeight)
	fitted.Width = max(min(current.Width, maxWidth), fitted.MinWidth)
	fitted.Height = max(min(current.Height, maxHeight), fitted.MinHeight)
	return fitted
}

// fitToScreen sizes the main window for the monitor it is about to open on.
// It runs once, before the first Show, while the window is still hidden.
func (d *DesktopHost) fitToScreen() {
	d.fitOnce.Do(func() {
		screen, err := d.window.GetScreen()
		if err != nil || screen == nil {
			return
		}
		width, height := d.window.Size()
		current := windowGeometry{
			Width: width, Height: height,
			MinWidth: MainWindowMinWidth, MinHeight: MainWindowMinHeight,
		}
		fitted := fitWindowToWorkArea(current, screen.WorkArea.Width, screen.WorkArea.Height)
		if fitted == current {
			return
		}
		d.window.SetMinSize(fitted.MinWidth, fitted.MinHeight)
		d.window.SetSize(fitted.Width, fitted.Height)
		d.window.Center()
	})
}
