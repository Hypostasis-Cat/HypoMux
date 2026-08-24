package services

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

type fakeDiagnosticProbe struct {
	icmp      icmpProbeResult
	tcpOK     bool
	tcpDetail string
	started   chan struct{}
	block     bool
}

func (p *fakeDiagnosticProbe) ICMP(ctx context.Context, _, _ string) icmpProbeResult {
	if p.started != nil {
		close(p.started)
		p.started = nil
	}
	if p.block {
		<-ctx.Done()
		return icmpProbeResult{Status: "unavailable", LossRate: 100, Note: "cancelled"}
	}
	return p.icmp
}

func (p *fakeDiagnosticProbe) BoundTCP(_ context.Context, _ AdapterView) (bool, string) {
	return p.tcpOK, p.tcpDetail
}

func newTestDiagnostics(t *testing.T, probe diagnosticProbe) *DiagnosticsService {
	t.Helper()
	logs := newSupportLogStore(filepath.Join(t.TempDir(), "logs", "app.log"))
	adapter := AdapterView{
		ID: "ethernet", Name: "Ethernet", Address: "192.0.2.10", IfIndex: 12,
		Gateway: "192.0.2.1", DNSServers: []string{"223.5.5.5"},
		Metric: 25, AutoMetric: true, Operational: true,
	}
	return &DiagnosticsService{
		logs: logs, probe: probe,
		natServers:   newNATServerStore(filepath.Join(t.TempDir(), "nat_servers.json")),
		listAdapters: func() ([]AdapterView, error) { return []AdapterView{adapter}, nil },
		latest: DiagnosticSnapshot{
			State: "idle", TargetIP: diagnosticTargetIPv4, Results: []DiagnosticResult{},
		},
	}
}

func TestDiagnosticsUsesBoundTCPAsAuthoritativeResult(t *testing.T) {
	service := newTestDiagnostics(t, &fakeDiagnosticProbe{
		icmp: icmpProbeResult{
			Status: "unstable", LossRate: 20, AvgLatencyMS: 42,
			JitterMS: 140, Sent: 10, Received: 8,
		},
		tcpOK: true, tcpDetail: "TCP 223.5.5.5:443 via 192.0.2.10",
	})
	snapshot, err := service.Run([]string{"ethernet"})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.State != "completed" || len(snapshot.Results) != 1 {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
	result := snapshot.Results[0]
	if result.Status != "available" || result.LossRate != 0 || result.AvgLatencyMS != 42 || result.JitterMS != 140 {
		t.Fatalf("v2.2.0 result semantics changed: %+v", result)
	}
	if len(result.Checks) != 4 || result.Checks[1].Level != "pass" {
		t.Fatalf("configuration checks missing: %+v", result.Checks)
	}
}

func TestDiagnosticsCanBeCancelled(t *testing.T) {
	started := make(chan struct{})
	service := newTestDiagnostics(t, &fakeDiagnosticProbe{started: started, block: true})
	done := make(chan DiagnosticSnapshot, 1)
	go func() {
		snapshot, _ := service.Run([]string{"ethernet"})
		done <- snapshot
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("diagnostic did not start")
	}
	service.Cancel()
	select {
	case snapshot := <-done:
		if snapshot.State != "cancelled" {
			t.Fatalf("expected cancelled, got %s", snapshot.State)
		}
	case <-time.After(time.Second):
		t.Fatal("diagnostic did not stop")
	}
}
