//go:build !windows

package wails

import (
	"unsafe"

	"github.com/Hypostasis-Cat/HypoMux/desktop/internal/platform"
)

func unsupportedAppearance() platform.NativeAppearanceResult {
	return platform.NativeAppearanceResult{Fallback: true, Reason: "native Windows material is unavailable"}
}

func setNativeMaterial(_ unsafe.Pointer, _ string) platform.NativeAppearanceResult {
	return unsupportedAppearance()
}

func setNativeTheme(_ unsafe.Pointer, _ string) platform.NativeAppearanceResult {
	return unsupportedAppearance()
}

func setNativeAccent(_ unsafe.Pointer, _ string) platform.NativeAppearanceResult {
	return unsupportedAppearance()
}
