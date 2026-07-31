package wails

import "github.com/Hypostasis-Cat/HypoMux/desktop/internal/platform"

func (d *DesktopHost) SetWindowMaterial(material string) platform.NativeAppearanceResult {
	return setNativeMaterial(d.window.NativeWindow(), material)
}

func (d *DesktopHost) SetWindowTheme(mode string) platform.NativeAppearanceResult {
	return setNativeTheme(d.window.NativeWindow(), mode)
}

func (d *DesktopHost) SetWindowAccent(colour string) platform.NativeAppearanceResult {
	return setNativeAccent(d.window.NativeWindow(), colour)
}
