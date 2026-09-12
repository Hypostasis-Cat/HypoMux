//go:build windows

package startup

import (
	"os"
	"testing"
)

// Opt-in, read-only native smoke test. Normal tests stay independent of the
// host's WLAN service and hardware. No network names or profile XML are logged.
func TestNativeWiFiReadOnly(t *testing.T) {
	if os.Getenv("HYPOMUX_TEST_WLAN_READONLY") != "1" {
		t.Skip("opt-in native WLAN smoke test")
	}
	session, err := openWiFiSession()
	if err != nil {
		t.Fatal(err)
	}
	defer session.close()
	interfaces, err := session.interfaces()
	if err != nil {
		t.Fatal(err)
	}
	for _, adapter := range interfaces {
		if adapter.id == "" || adapter.name == "" {
			t.Fatal("native adapter identity was empty")
		}
		if _, err := session.profiles(adapter.id); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("Read-only WLAN API validated for %d interfaces; no connection requested", len(interfaces))
}
