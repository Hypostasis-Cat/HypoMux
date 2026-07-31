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
