package wails

import (
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func TestTrayMenuPosition(t *testing.T) {
	for _, tc := range []struct {
		name   string
		anchor application.Point
		work   application.Rect
		want   application.Point
	}{
		{"overflow panel", application.Point{X: 1400, Y: 850}, application.Rect{Width: 1920, Height: 1040}, application.Point{X: 1402, Y: 666}},
		{"right edge", application.Point{X: 1900, Y: 1050}, application.Rect{Width: 1920, Height: 1040}, application.Point{X: 1690, Y: 858}},
		{"top taskbar", application.Point{X: 500, Y: 20}, application.Rect{Y: 40, Width: 1920, Height: 1040}, application.Point{X: 502, Y: 40}},
		{"negative monitor origin", application.Point{X: -20, Y: 800}, application.Rect{X: -1280, Width: 1280, Height: 984}, application.Point{X: -230, Y: 616}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := trayMenuPosition(tc.anchor, tc.work, 208, 182); got != tc.want {
				t.Fatalf("position = %+v, want %+v", got, tc.want)
			}
		})
	}
}
