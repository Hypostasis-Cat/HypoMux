package services

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMigrationErrorIdentifiesAndPreservesLegacyFile(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows user profile migration")
	}
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HYPOMUX_DATA_DIR", filepath.Join(home, "new"))
	path := filepath.Join(home, ".hypomux", "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"socks_port":10800,"http_port":10800}`)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	s := NewSettingsService()
	if s.StartupError() == nil || s.StartupErrorPath() != path || !strings.Contains(s.StartupError().Error(), "http_port=10800") {
		t.Fatalf("incorrect migration guidance: %s / %v", s.StartupErrorPath(), s.StartupError())
	}
	if _, err := s.Update(DefaultSettings()); err == nil {
		t.Fatal("failed migration allowed overwrite")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(data) {
		t.Fatal("legacy input changed")
	}
	if _, err := os.Stat(s.ConfigPath()); !os.IsNotExist(err) {
		t.Fatalf("unexpected destination file: %v", err)
	}
}

func TestDuplicateAdapterSourcesAreActionable(t *testing.T) {
	adapters := []AdapterView{
		{Name: "Ethernet", IfIndex: 7, Address: "172.22.63.37"},
		{Name: "WLAN", IfIndex: 9, Address: "172.22.63.37"},
	}
	err := validateAdapterSources(adapters)
	if err == nil || !strings.Contains(err.Error(), "Ethernet") || !strings.Contains(err.Error(), "WLAN") || !strings.Contains(err.Error(), "172.22.63.37") {
		t.Fatalf("missing conflict detail: %v", err)
	}
	if err := validateAdapterSources(adapters[:1]); err != nil {
		t.Fatal(err)
	}
	adapters[1].Address = "172.22.63.38"
	if err := validateAdapterSources(adapters); err != nil {
		t.Fatal(err)
	}
	adapters[0].SourceIPv6 = "2001:db8::1"
	adapters[1].SourceIPv6 = "2001:db8:0:0::1"
	if err := validateAdapterSources(adapters); err == nil {
		t.Fatal("equivalent IPv6 sources accepted")
	}
}

func TestPreflightEnvironmentIsInformationalButConflictsStillBlock(t *testing.T) {
	platform := tunPlatformSnapshot{PrivilegeBrokerAvailable: true, WFPReady: true,
		RouteScanError: "timeout", NetworkRisks: []string{"Hyper-V virtual adapter", "ICS running"}}
	s := testTunService(t, platform)
	snapshot, err := s.Preflight([]string{"ethernet"})
	if err != nil || !snapshot.Ready {
		t.Fatalf("unexpected failure: %v %+v", err, snapshot)
	}
	for _, issue := range snapshot.Issues {
		if issue.Level != "info" {
			t.Fatalf("environment raised a warning: %+v", issue)
		}
	}
	platform.DefaultRouteAliases = []string{"Clash"}
	s.inspectPlatform = func(bool) tunPlatformSnapshot { return platform }
	snapshot, _ = s.Preflight([]string{"ethernet"})
	if snapshot.Ready || !hasTunIssue(snapshot, "foreign_tun") {
		t.Fatal("partial scan hid a real conflict")
	}
	s.listAdapters = func() ([]AdapterView, error) {
		return []AdapterView{
			{ID: "ethernet", Name: "Ethernet", Address: "172.22.63.37"},
			{ID: "wlan", Name: "WLAN", Address: "172.22.63.37"},
		}, nil
	}
	snapshot, _ = s.Preflight([]string{"ethernet", "wlan"})
	if snapshot.Ready || !hasTunIssue(snapshot, "duplicate_source_ip") {
		t.Fatal("duplicate source passed preflight")
	}
}
