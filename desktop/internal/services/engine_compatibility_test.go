package services

import "testing"

func TestWFPCompatibilityClassifierIsStrict(t *testing.T) {
	for _, message := range []string{
		"FwpmEngineOpen0 failed",
		"Windows Filtering Platform provider unavailable",
		"BFE service is stopped",
		"WFP strict route filter rejected",
	} {
		if !isWFPCompatibilityError(message) {
			t.Fatalf("expected WFP compatibility error: %q", message)
		}
	}
	for _, message := range []string{
		"adapter disconnected",
		"configuration file missing",
		"ordinary upstream timeout",
		"process exited with code 1",
	} {
		if isWFPCompatibilityError(message) {
			t.Fatalf("ordinary failure was misclassified as WFP: %q", message)
		}
	}
}
