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
		t.Skip("no active IPv4 adapter is available")
	}
	if available[0].Metric < 0 {
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
	if result.Sent != 10 || result.TargetIP != diagnosticTargetIPv4 {
		t.Fatalf("real ICMP probe did not preserve the v2.2.0 contract: %+v", result)
	}
	if result.BoundTCPDetail == "" || len(result.Checks) != 4 {
		t.Fatalf("bound TCP evidence or configuration checks are missing: %+v", result)
	}
	if len(logs.Snapshot().Sessions) != 1 {
		t.Fatal("diagnostic support-log session was not recorded")
	}
}
