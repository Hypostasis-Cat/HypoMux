package services

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestClassifyNAT(t *testing.T) {
	tests := []struct {
		mapping   string
		filtering string
		want      string
	}{
		{natMappingDirect, natEndpointIndependent, "direct"},
		{natEndpointIndependent, natEndpointIndependent, "full_cone"},
		{natEndpointIndependent, natAddressDependent, "restricted_cone"},
		{natEndpointIndependent, natAddressPortDependent, "port_restricted_cone"},
		{natAddressPortDependent, natEndpointIndependent, "symmetric"},
		{natAddressDependent, natAddressDependent, "unknown"},
		{natBehaviorInconclusive, natEndpointIndependent, "inconclusive"},
	}
	for _, test := range tests {
		if got := classifyNAT(test.mapping, test.filtering); got != test.want {
			t.Fatalf("classifyNAT(%q, %q) = %q, want %q", test.mapping, test.filtering, got, test.want)
		}
	}
}

func TestHostFirewallMakesRestrictiveFilteringInconclusive(t *testing.T) {
	result := NATDetectionResult{
		State: "completed", NATType: "port_restricted_cone",
		MappingBehavior: natEndpointIndependent, FilteringBehavior: natAddressPortDependent,
	}
	limited := applyHostFirewallReliability(&result, NATFirewallState{Supported: true, Enabled: true})
	if !limited || !result.HostFirewallLimited || result.NATType != "inconclusive" || result.FilteringBehavior != natBehaviorInconclusive {
		t.Fatalf("expected host-firewall-limited result, got %+v", result)
	}
}

func TestAllowedFirewallKeepsFilteringResult(t *testing.T) {
	result := NATDetectionResult{FilteringBehavior: natAddressPortDependent}
	if applyHostFirewallReliability(&result, NATFirewallState{Supported: true, Enabled: true, Allowed: true}) {
		t.Fatalf("allowed firewall must not invalidate filtering: %+v", result)
	}
}

func TestDiagnosticsNATRunPersistsResult(t *testing.T) {
	service := newTestDiagnostics(t, &fakeDiagnosticProbe{})
	service.detectNAT = func(_ context.Context, adapter AdapterView, _ []NATServer) NATDetectionResult {
		return NATDetectionResult{
			State: "completed", AdapterID: adapter.ID, Name: adapter.Name, Address: adapter.Address,
			NATType: "full_cone", MappingBehavior: natEndpointIndependent,
			FilteringBehavior: natEndpointIndependent, PublicEndpoint: "198.51.100.8:42000",
			Server: "stun.example:3478", Detail: "test fixture",
		}
	}
	result, err := service.RunNAT("ethernet", natServerAutoID)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "completed" || result.NATType != "full_cone" || result.PublicEndpoint == "" {
		t.Fatalf("unexpected NAT result: %+v", result)
	}
	if latest := service.NATLatest(); latest.NATType != result.NATType || latest.AdapterID != "ethernet" {
		t.Fatalf("NAT result was not persisted: %+v", latest)
	}
}

func TestDiagnosticsNATCanBeCancelled(t *testing.T) {
	service := newTestDiagnostics(t, &fakeDiagnosticProbe{})
	started := make(chan struct{})
	service.detectNAT = func(ctx context.Context, _ AdapterView, _ []NATServer) NATDetectionResult {
		close(started)
		<-ctx.Done()
		return NATDetectionResult{State: "cancelled"}
	}
	done := make(chan NATDetectionResult, 1)
	go func() {
		result, _ := service.RunNAT("ethernet", natServerAutoID)
		done <- result
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("NAT detection did not start")
	}
	service.CancelNAT()
	select {
	case result := <-done:
		if result.State != "cancelled" {
			t.Fatalf("expected cancelled NAT detection, got %+v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("NAT detection did not stop")
	}
}

func TestDiagnosticsNATRejectsConcurrentRun(t *testing.T) {
	service := newTestDiagnostics(t, &fakeDiagnosticProbe{})
	started := make(chan struct{})
	release := make(chan struct{})
	service.detectNAT = func(_ context.Context, _ AdapterView, _ []NATServer) NATDetectionResult {
		close(started)
		<-release
		return NATDetectionResult{State: "completed", NATType: "full_cone"}
	}
	done := make(chan struct{})
	go func() {
		_, _ = service.RunNAT("ethernet", natServerAutoID)
		close(done)
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first NAT detection did not start")
	}
	if _, err := service.RunNAT("ethernet", natServerAutoID); err == nil || !strings.Contains(err.Error(), "已在运行") {
		t.Fatalf("expected concurrent run rejection, got %v", err)
	}
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("first NAT detection did not finish")
	}
}
