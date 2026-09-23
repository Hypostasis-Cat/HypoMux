package wails

// trayMenuPosition uses logical screen coordinates, including negative monitor
// origins. Prefer the upper-right of the invocation point, then flip and clamp.
func trayMenuPosition(anchorX, anchorY, width, height, workX, workY, workWidth, workHeight int) (int, int) {
	const gap = 6
	left, top := workX+gap, workY+gap
	right, bottom := workX+workWidth-gap, workY+workHeight-gap
	x, y := anchorX+gap, anchorY-height-gap
	if x+width > right {
		x = anchorX - width - gap
	}
	if y < top {
		y = anchorY + gap
	}
	return max(left, min(x, right-width)), max(top, min(y, bottom-height))
}
