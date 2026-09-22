//go:build windows

package services

func protectAIData(data []byte, encrypt bool) ([]byte, error) {
	return protectHotspotPreferences(data, encrypt)
}
