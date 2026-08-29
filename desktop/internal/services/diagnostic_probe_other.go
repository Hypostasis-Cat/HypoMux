//go:build !windows && !darwin

package services

import (
	"context"
	"fmt"
)

type unsupportedDiagnosticProbe struct{}

func newDiagnosticProbe() diagnosticProbe {
	return unsupportedDiagnosticProbe{}
}

func (unsupportedDiagnosticProbe) ICMP(_ context.Context, _, _ string) icmpProbeResult {
	return icmpProbeResult{
		Status: "unavailable", LossRate: 100,
		Note: "selected-interface ICMP diagnostics require Windows",
	}
}

func (unsupportedDiagnosticProbe) BoundTCP(_ context.Context, adapter AdapterView) (bool, string) {
	return false, fmt.Sprintf("%s：绑定 TCP 诊断需要 Windows", adapter.Name)
}
