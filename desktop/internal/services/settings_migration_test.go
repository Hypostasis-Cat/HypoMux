package services

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestMigrateLegacySettingsPreservesSemantics(t *testing.T) {
	payload, err := json.Marshal(map[string]any{
		"run_mode":                      "tun",
		"selected_adapters":             []string{"以太网", "WLAN"},
		"socks_port":                    12080,
		"http_port":                     12081,
		"weighted_scheduler":            true,
		"wfp_strict_route":              false,
		"force_tun_connectivity_bypass": true,
		"blocked_domain_bypass":         true,
		"blocked_domain_expiry":         false,
		"dns_server":                    "1.1.1.1",
		"doh_provider":                  "google",
		"nic_bandwidth_limits":          map[string]int{"WLAN": 3},
		"routing_rules": []map[string]any{
			{"process_name": []string{"a.exe", "b.exe"}, "outbound": "aggregation"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	settings, err := migrateLegacySettings(payload)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Mode != "tun" || settings.SOCKSPort != 12080 || settings.HTTPPort != 12081 {
		t.Fatalf("core settings not migrated: %#v", settings)
	}
	if !settings.Weighted || settings.StrictRoute || !settings.ForceTUNBypass ||
		!settings.BlockedDomainBypass || settings.BlockedDomainExpiry {
		t.Fatalf("boolean semantics not migrated: %#v", settings)
	}
	if settings.DNSServer != "1.1.1.1" || settings.DNSPolicy != "google" ||
		settings.DNSEgressMode != DNSEgressAuto || settings.AdapterWeights["WLAN"] != 3 {
		t.Fatalf("network settings not migrated: %#v", settings)
	}
	if len(settings.RoutingRules) != 2 || settings.RoutingRules[0].Value != "a.exe" || settings.RoutingRules[1].Value != "b.exe" {
		t.Fatalf("multi-value routing rules were not preserved: %#v", settings.RoutingRules)
	}
}

func TestLegacyMigrationRollbackRestoresNewSettings(t *testing.T) {
	dataDir := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HYPOMUX_DATA_DIR", dataDir)
	t.Setenv("USERPROFILE", homeDir)
	t.Setenv("HOME", homeDir)
	legacyDir := filepath.Join(homeDir, ".hypomux")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, "config.json"), []byte(`{
	  "run_mode": "tun",
	  "socks_port": 13080,
	  "http_port": 13081,
	  "dns_server": "223.5.5.5",
	  "doh_provider": "auto"
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	service := NewSettingsService()
	current := service.Get()
	current.SOCKSPort = 14080
	current.HTTPPort = 14081
	if _, err := service.Update(current); err != nil {
		t.Fatal(err)
	}
	if _, err := service.MigrateLegacy(); err != nil {
		t.Fatal(err)
	}
	restored, err := service.RollbackLegacyMigration()
	if err != nil {
		t.Fatal(err)
	}
	if restored.SOCKSPort != 14080 || restored.HTTPPort != 14081 {
		t.Fatalf("rollback did not restore current settings: %#v", restored)
	}
}

func TestSettingsUpdateDefaultsMissingDNSEgressMode(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	service := NewSettingsService()
	next := service.Get()
	next.DNSEgressMode = ""
	updated, err := service.Update(next)
	if err != nil {
		t.Fatal(err)
	}
	if updated.DNSEgressMode != DNSEgressAuto {
		t.Fatalf("missing DNS egress mode was not migrated to auto: %#v", updated)
	}
}
