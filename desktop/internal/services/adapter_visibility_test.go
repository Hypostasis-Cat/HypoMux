package services

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVirtualAdapterClassification(t *testing.T) {
	for _, tc := range []struct {
		name, description string
		virtual           bool
	}{
		{"Ethernet 2", "VMware Virtual Ethernet Adapter for VMnet8", true},
		{"Renamed", "Hyper-V Virtual Ethernet Adapter", true},
		{"vEthernet (Default Switch)", "", true},
		{"Ethernet", "VirtualBox Host-Only Ethernet Adapter", true},
		{"Work", "WireGuard Tunnel", true},
		{"VPN", "TAP-Windows Adapter V9", true},
		{"以太网", "Realtek PCIe GbE Family Controller", false},
		{"WLAN", "Intel(R) Wi-Fi 6 AX201 160MHz", false},
		{"USB Ethernet", "Remote NDIS based Internet Sharing Device", false},
		{"Unknown", "", false},
	} {
		if got := isVirtualAdapter(tc.name, tc.description); got != tc.virtual {
			t.Errorf("%q / %q: virtual=%t", tc.name, tc.description, got)
		}
	}
}

func TestHideVirtualAdaptersDefaultsAndPersistence(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	if !DefaultSettings().HideVirtualAdapters {
		t.Fatal("fresh default should hide virtual adapters")
	}
	path := filepath.Join(settingsDirectory(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"mode":"proxy","socks_port":10800,"http_port":10801}`), 0600); err != nil {
		t.Fatal(err)
	}
	s := NewSettingsService()
	if !s.Get().HideVirtualAdapters {
		t.Fatal("older settings did not get the default")
	}
	next := s.Get()
	next.HideVirtualAdapters = false
	if _, err := s.Update(next); err != nil {
		t.Fatal(err)
	}
	if NewSettingsService().Get().HideVirtualAdapters {
		t.Fatal("explicit false did not persist")
	}
	migrated, err := migrateLegacySettings([]byte(`{}`))
	if err != nil || !migrated.HideVirtualAdapters {
		t.Fatalf("legacy default: %+v %v", migrated, err)
	}
}
