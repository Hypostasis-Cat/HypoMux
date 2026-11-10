package wails

import (
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func TestFitWindowToWorkArea(t *testing.T) {
	preferred := windowGeometry{
		Width: MainWindowWidth, Height: MainWindowHeight,
		MinWidth: MainWindowMinWidth, MinHeight: MainWindowMinHeight,
	}
	tests := []struct {
		name       string
		workWidth  int
		workHeight int
		want       windowGeometry
	}{
		{
			// 1920×1080 at 100 %: everything fits, nothing changes.
			name: "large work area keeps preferred size", workWidth: 1920, workHeight: 1032,
			want: preferred,
		},
		{
			// 1920×1080 at 150 % with a taskbar: issue #118.
			name: "scaled 1080p shrinks height and minimum", workWidth: 1280, workHeight: 688,
			want: windowGeometry{Width: 1120, Height: 640, MinWidth: 960, MinHeight: 640},
		},
		{
			// 1920×1080 at 175 %: both axes too small.
			name: "heavily scaled display shrinks both axes", workWidth: 1097, workHeight: 590,
			want: windowGeometry{Width: 1049, Height: 542, MinWidth: 960, MinHeight: 542},
		},
		{
			// Below the floor the window uses the whole work area; the WebView scrolls.
			name: "tiny work area uses floor without overflowing", workWidth: 600, workHeight: 400,
			want: windowGeometry{Width: 600, Height: 400, MinWidth: 600, MinHeight: 400},
		},
		{
			name: "unknown work area leaves geometry alone", workWidth: 0, workHeight: 0,
			want: preferred,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := fitWindowToWorkArea(preferred, test.workWidth, test.workHeight)
			if got != test.want {
				t.Fatalf("fitWindowToWorkArea(%dx%d) = %+v, want %+v", test.workWidth, test.workHeight, got, test.want)
			}
			if test.workWidth > 0 && (got.Width > test.workWidth || got.Height > test.workHeight) {
				t.Fatalf("window %dx%d exceeds work area %dx%d", got.Width, got.Height, test.workWidth, test.workHeight)
			}
			if got.Width < got.MinWidth || got.Height < got.MinHeight {
				t.Fatalf("size %+v is below its own minimum", got)
			}
		})
	}
}

func TestFitWindowToWorkAreaNeverGrows(t *testing.T) {
	small := windowGeometry{Width: 900, Height: 600, MinWidth: 800, MinHeight: 500}
	if got := fitWindowToWorkArea(small, 2560, 1400); got != small {
		t.Fatalf("fit grew a window that already fits: %+v", got)
	}
}

type fitTestWindow struct {
	trayTestWindow
	screen              *application.Screen
	width, height       int
	minWidth, minHeight int
	centered            int
}

func (w *fitTestWindow) GetScreen() (*application.Screen, error) { return w.screen, nil }
func (w *fitTestWindow) Size() (int, int)                        { return w.width, w.height }
func (w *fitTestWindow) SetSize(width, height int) application.Window {
	w.width, w.height = width, height
	return w
}
func (w *fitTestWindow) SetMinSize(width, height int) application.Window {
	w.minWidth, w.minHeight = width, height
	return w
}
func (w *fitTestWindow) Center() { w.centered++ }

func TestShowFitsMainWindowToScreenOnce(t *testing.T) {
	window := &fitTestWindow{
		screen: &application.Screen{WorkArea: application.Rect{Width: 1280, Height: 688}},
		width:  MainWindowWidth, height: MainWindowHeight,
	}
	host := &DesktopHost{window: window}
	host.ShowStartup()
	if window.width != 1120 || window.height != 640 || window.minHeight != 640 || window.centered != 1 {
		t.Fatalf("startup show did not fit window: %+v", window)
	}
	if !window.shown {
		t.Fatal("startup show must still reveal the window")
	}
	// A size the user picks later must survive hiding to tray and showing again.
	window.width, window.height = 1200, 680
	host.Show()
	if window.width != 1200 || window.height != 680 || window.centered != 1 {
		t.Fatalf("later show resized the window: %+v", window)
	}
}
