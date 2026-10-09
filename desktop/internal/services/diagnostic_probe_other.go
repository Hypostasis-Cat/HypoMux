//go:build !windows

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
		Status: "unavailable", LossRate: -1,
		Note: "selected-interface ICMP diagnostics require Windows",
	}
}

func (unsupportedDiagnosticProbe) BoundEgress(_ context.Context, adapter AdapterView) diagnosticEgressResult {
	return diagnosticEgressResult{Detail: fmt.Sprintf("%s：绑定出口诊断需要 Windows", adapter.Name)}
}
