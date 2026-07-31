package platform

// DesktopHost is the only boundary the desktop shell may use for native window
// and lifecycle operations. Network aggregation and privileged work do not
// belong in this interface.
type DesktopHost interface {
	Minimise()
	ToggleMaximise()
	HideToTray()
	Show()
	Quit()
	SetWindowMaterial(material string) NativeAppearanceResult
	SetWindowTheme(mode string) NativeAppearanceResult
	SetWindowAccent(colour string) NativeAppearanceResult
	OpenJSONFile(title string) (string, error)
	SaveJSONFile(title string, filename string) (string, error)
	SaveTextFile(title string, filename string) (string, error)
	OpenDirectory(path string) error
}

// NativeAppearanceResult tells the web layer whether Windows accepted the
// requested DWM value. CSS materials remain the readable fallback.
type NativeAppearanceResult struct {
	Applied  bool   `json:"applied"`
	Fallback bool   `json:"fallback"`
	Reason   string `json:"reason,omitempty"`
}
