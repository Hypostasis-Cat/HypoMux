//go:build windows

package services

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRealWindowsAdapterDiagnostic(t *testing.T) {
	if os.Getenv("HYPOMUX_RUN_DIAGNOSTIC_TEST") != "1" {
		t.Skip("set HYPOMUX_RUN_DIAGNOSTIC_TEST=1 for the explicit Windows adapter diagnostic")
	}
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	settings := NewSettingsService()
	adapters := NewAdapterService(settings)
	available, err := adapters.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(available) == 0 {
		t.Skip("no active IPv4 or IPv6 adapter is available")
	}
	if name := os.Getenv("HYPOMUX_NETWORK_TEST_ADAPTER"); name != "" {
		found := false
		for index, adapter := range available {
			if adapter.Name == name {
				available[0], available[index] = available[index], available[0]
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("requested diagnostic adapter is unavailable: %s", name)
		}
	}
	if (available[0].Address != "" && available[0].Metric < 0) || (available[0].Address == "" && available[0].IPv6Metric < 0) {
		t.Fatalf("Windows adapter metadata was not resolved: %+v", available[0])
	}

	logs := newSupportLogStore(filepath.Join(t.TempDir(), "logs", "app.log"))
	service := NewDiagnosticsService(settings, adapters, nil, logs, nil)
	snapshot, err := service.Run([]string{available[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.State != "completed" || snapshot.Completed != 1 || len(snapshot.Results) != 1 {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
	result := snapshot.Results[0]
	t.Logf("native diagnostic source=%s target=%s sent=%d received=%d bound_tcp=%t detail=%s", result.Address, result.TargetIP, result.Sent, result.Received, result.BoundTCPOK, result.BoundTCPDetail)
	target := diagnosticTargetIPv4
	if available[0].Address == "" {
		target = diagnosticTargetIPv6
	}
	if result.Sent != 10 || result.TargetIP != target {
		t.Fatalf("real ICMP probe did not preserve the v2.2.0 contract: %+v", result)
	}
	checks := 4
	if available[0].Address != "" && available[0].SourceIPv6 != "" {
		checks++
	}
	if result.BoundTCPDetail == "" || len(result.Checks) != checks {
		t.Fatalf("bound TCP evidence or configuration checks are missing: %+v", result)
	}
	if len(logs.Snapshot().Sessions) != 1 {
		t.Fatal("diagnostic support-log session was not recorded")
	}
}
