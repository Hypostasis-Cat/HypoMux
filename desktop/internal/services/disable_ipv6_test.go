package services

import (
	"github.com/Hypostasis-Cat/HypoMux/desktop/internal/engineclient"
	"testing"
)

func TestDisableIPv6SettingsPersistAndRemainOptIn(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	s := NewSettingsService()
	if s.Get().DisableIPv6 {
		t.Fatal("old/default settings must retain IPv6")
	}
	for _, disabled := range []bool{true, false} {
		before := s.Get()
		saved, err := s.UpdateFields(AppSettings{DisableIPv6: disabled}, []string{"disable_ipv6"})
		if err != nil {
			t.Fatal(err)
		}
		if saved.DisableIPv6 != disabled || NewSettingsService().Get().DisableIPv6 != disabled {
			t.Fatal("IPv6 setting not persisted")
		}
		if saved.DNSPolicy != before.DNSPolicy || saved.TUNStack != before.TUNStack {
			t.Fatal("unrelated settings changed")
		}
	}
}

func TestDisableIPv6CannotSilentlyUseOlderCore(t *testing.T) {
	old := engineclient.Hello{EngineVersion: "2.8.2"}
	if err := validateIPv6PolicyCore(true, old, "tun"); err == nil {
		t.Fatal("older core silently accepted policy")
	}
	if err := validateIPv6PolicyCore(false, old, "tun"); err != nil {
		t.Fatal("unchanged setting rejected legacy core")
	}
	current := engineclient.Hello{ModeFeatures: map[string][]string{"tun_tcp_pool": {"disable_ipv6"}, "proxy": {"disable_ipv6"}}}
	for _, mode := range []string{"tun", "proxy"} {
		if err := validateIPv6PolicyCore(true, current, mode); err != nil {
			t.Fatal(err)
		}
	}
}
