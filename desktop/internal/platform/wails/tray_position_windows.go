//go:build windows

package wails

import (
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
	"golang.org/x/sys/windows"
)

var trayUser32 = windows.NewLazySystemDLL("user32.dll")
var trayGetCursorPos = trayUser32.NewProc("GetCursorPos")
var trayMonitorFromRect = trayUser32.NewProc("MonitorFromRect")
var trayGetMonitorInfo = trayUser32.NewProc("GetMonitorInfoW")

type trayNativePoint struct{ X, Y int32 }
type trayNativeRect struct{ Left, Top, Right, Bottom int32 }
type trayMonitorInfo struct {
	Size          uint32
	Monitor, Work trayNativeRect
	Flags         uint32
}

func newTrayPositioner(window application.Window, tray *application.SystemTray) func() error {
	// Capture once at invocation: asynchronous content resizing must not follow
	// the pointer as the user moves into the menu.
	var point trayNativePoint
	if ok, _, _ := trayGetCursorPos.Call(uintptr(unsafe.Pointer(&point))); ok == 0 {
		return func() error { return tray.PositionWindow(window, 8) }
	}
	rect := trayNativeRect{point.X, point.Y, point.X + 1, point.Y + 1}
	monitor, _, _ := trayMonitorFromRect.Call(uintptr(unsafe.Pointer(&rect)), 2)
	info := trayMonitorInfo{}
	info.Size = uint32(unsafe.Sizeof(info))
	if ok, _, _ := trayGetMonitorInfo.Call(monitor, uintptr(unsafe.Pointer(&info))); ok == 0 {
		return func() error { return tray.PositionWindow(window, 8) }
	}
	return func() error {
		return application.InvokeSyncWithError(func() error {
			anchor := application.PhysicalToDipPoint(application.Point{X: int(point.X), Y: int(point.Y)})
			work := application.PhysicalToDipRect(application.Rect{
				X: int(info.Work.Left), Y: int(info.Work.Top),
				Width: int(info.Work.Right - info.Work.Left), Height: int(info.Work.Bottom - info.Work.Top),
			})
			bounds := window.Bounds()
			x, y := trayMenuPosition(anchor.X, anchor.Y, bounds.Width, bounds.Height, work.X, work.Y, work.Width, work.Height)
			window.SetPosition(x, y)
			return nil
		})
	}
}
