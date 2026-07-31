//go:build windows

package wails

import (
	"fmt"
	"strconv"
	"strings"
	"unsafe"

	"github.com/Hypostasis-Cat/HypoMux/desktop/internal/platform"
	"golang.org/x/sys/windows"
)

const (
	dwmUseImmersiveDarkMode = 20
	dwmBorderColour         = 34
	dwmSystemBackdropType   = 38
	dwmColourNone           = 0xFFFFFFFE
)

var dwmSetWindowAttribute = windows.NewLazySystemDLL("dwmapi.dll").NewProc("DwmSetWindowAttribute")

func setDWMAttribute(hwnd unsafe.Pointer, attribute uint32, value *uint32) error {
	if hwnd == nil {
		return fmt.Errorf("native window is not ready")
	}
	result, _, callError := dwmSetWindowAttribute.Call(
		uintptr(hwnd),
		uintptr(attribute),
		uintptr(unsafe.Pointer(value)),
		unsafe.Sizeof(*value),
	)
	if result != 0 {
		return fmt.Errorf("DwmSetWindowAttribute returned HRESULT 0x%X: %v", result, callError)
	}
	return nil
}

func nativeResult(err error) platform.NativeAppearanceResult {
	if err != nil {
		return platform.NativeAppearanceResult{Fallback: true, Reason: err.Error()}
	}
	return platform.NativeAppearanceResult{Applied: true}
}

func setNativeMaterial(hwnd unsafe.Pointer, material string) platform.NativeAppearanceResult {
	values := map[string]uint32{
		"mica":      2,
		"acrylic":   3,
		"tabbed":    4,
		"wallpaper": 1,
		"solid":     1,
	}
	value, ok := values[strings.ToLower(material)]
	if !ok {
		return platform.NativeAppearanceResult{Fallback: true, Reason: "unknown window material"}
	}
	return nativeResult(setDWMAttribute(hwnd, dwmSystemBackdropType, &value))
}

func setNativeTheme(hwnd unsafe.Pointer, mode string) platform.NativeAppearanceResult {
	var value uint32
	if strings.EqualFold(mode, "dark") {
		value = 1
	}
	return nativeResult(setDWMAttribute(hwnd, dwmUseImmersiveDarkMode, &value))
}

func setNativeAccent(hwnd unsafe.Pointer, colour string) platform.NativeAppearanceResult {
	hex := strings.TrimPrefix(colour, "#")
	if len(hex) != 6 {
		return platform.NativeAppearanceResult{Fallback: true, Reason: "accent must use #RRGGBB"}
	}
	if _, err := strconv.ParseUint(hex, 16, 32); err != nil {
		return platform.NativeAppearanceResult{Fallback: true, Reason: "invalid accent colour"}
	}
	// The accent belongs to Fluent/CSS controls. Applying it to
	// DWMWA_BORDER_COLOR creates a bright one-pixel frame around the whole
	// frameless window, so explicitly suppress the native border instead.
	value := uint32(dwmColourNone)
	return nativeResult(setDWMAttribute(hwnd, dwmBorderColour, &value))
}
