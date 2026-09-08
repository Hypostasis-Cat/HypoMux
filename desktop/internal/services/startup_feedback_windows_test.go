//go:build windows

package services

import (
	"context"
	"strings"
	"testing"
)

func TestPartialNetworkInspectionPreservesCompletedEvidence(t *testing.T) {
	output := []byte("{\"aliases\":[\"Clash\"],\"risks\":[],\"errors\":[]}\n{\"aliases\":[\"Clash\"],\"risks\":[\"Hyper-V\"],\"errors\":[]}\n")
	for _, tail := range []string{"", "{\"aliases\":", `{"aliases":["changed",42]}`} {
		aliases, risks, detail := decodeNetworkInspection(append(append([]byte(nil), output...), tail...), context.DeadlineExceeded)
		if len(aliases) != 1 || aliases[0] != "Clash" || len(risks) != 1 || detail == "" {
			t.Fatalf("lost completed evidence: %v %v %s", aliases, risks, detail)
		}
	}
}

func TestEmptyNetworkInspectionIsIncomplete(t *testing.T) {
	_, _, detail := decodeNetworkInspection(nil, nil)
	if detail == "" {
		t.Fatal("empty output reported as a completed check")
	}
}

func TestICMPFailurePreservesRealErrorAndUnavailableMeasurements(t *testing.T) {
	for _, code := range []string{"1214", "1231"} {
		result := summarizeICMP(nil, 5, true, "ICMP failed (WinError "+code+")")
		if result.LossRate != -1 || !strings.Contains(result.Note, code) {
			t.Fatalf("invented diagnostic result: %+v", result)
		}
	}
	if result := summarizeICMP(nil, 5, false, "timeout"); result.LossRate != 100 {
		t.Fatalf("real timeouts lost: %+v", result)
	}
}
