package wails

import "testing"

func TestTrayMenuPosition(t *testing.T) {
	for _, tc := range []struct {
		name                                                string
		ax, ay, width, height, wx, wy, ww, wh, wantX, wantY int
	}{
		{"overflow icon", 130, 800, 208, 182, 0, 0, 1920, 1040, 136, 612},
		{"right edge", 1900, 1020, 208, 182, 0, 0, 1920, 1040, 1686, 832},
		{"top taskbar", 400, 50, 208, 182, 0, 40, 1920, 1040, 406, 56},
		{"negative monitor origin", -100, 800, 208, 182, -1920, 0, 1920, 1040, -314, 612},
		{"resized content", 130, 800, 208, 320, 0, 0, 1920, 1040, 136, 474},
		{"corner clamp", 0, 0, 208, 182, 0, 0, 1920, 1040, 6, 6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			x, y := trayMenuPosition(tc.ax, tc.ay, tc.width, tc.height, tc.wx, tc.wy, tc.ww, tc.wh)
			if x != tc.wantX || y != tc.wantY {
				t.Fatalf("got (%d, %d), want (%d, %d)", x, y, tc.wantX, tc.wantY)
			}
		})
	}
}
