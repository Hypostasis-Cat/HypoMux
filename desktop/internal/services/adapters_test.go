package services

import "testing"

func TestIsHypoMuxManagedAdapter(t *testing.T) {
	tests := map[string]bool{
		"HypoMux-Tun":   true,
		" hypomux-tun ": true,
		"WLAN":          false,
		"Other TUN":     false,
	}
	for name, expected := range tests {
		if actual := isHypoMuxManagedAdapter(name); actual != expected {
			t.Fatalf("isHypoMuxManagedAdapter(%q) = %v; want %v", name, actual, expected)
		}
	}
}

func TestIsVirtualTunnelAdapter(t *testing.T) {
	tests := map[string]bool{
		"utun0":   true,
		" utun9 ": true,
		"UTUN12":  true,
		"utun":    false,
		"utunVPN": false,
		"en0":     false,
		"Wi-Fi":   false,
	}
	for name, expected := range tests {
		if actual := isVirtualTunnelAdapter(name); actual != expected {
			t.Fatalf("isVirtualTunnelAdapter(%q) = %v; want %v", name, actual, expected)
		}
	}
}
