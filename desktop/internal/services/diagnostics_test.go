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
	tlsOK     bool
	tcpDetail string
	started   chan struct{}
	block     bool
	egress    func(AdapterView) diagnosticEgressResult
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

func (p *fakeDiagnosticProbe) BoundEgress(_ context.Context, adapter AdapterView) diagnosticEgressResult {
	if p.egress != nil {
		return p.egress(adapter)
	}
	return diagnosticEgressResult{TCP: p.tcpOK, TLS: p.tlsOK, Detail: p.tcpDetail}
}

func TestDiagnosticsEvidenceClassification(t *testing.T) {
	for _, tc := range []struct {
		name     string
		tcp, tls bool
		received int
		want     string
	}{
		{"ICMP blocked but TLS works", true, true, 0, "available"},
		{"local TCP accept without TLS", true, false, 0, "limited"},
		{"only ICMP responds", false, false, 10, "limited"},
		{"all targets fail", false, false, 0, "unverified"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestDiagnostics(t, &fakeDiagnosticProbe{tcpOK: tc.tcp, tlsOK: tc.tls,
				icmp: icmpProbeResult{Sent: 10, Received: tc.received, LossRate: (10 - tc.received) * 10}})
			r, err := s.Run([]string{"ethernet"})
			if err != nil || r.Results[0].Status != tc.want || r.Results[0].BoundTCPOK != tc.tcp {
				t.Fatalf("result=%+v err=%v", r, err)
			}
		})
	}
}

func TestDiagnosticsIPv6SuccessSurvivesIPv4Failure(t *testing.T) {
	s := newTestDiagnostics(t, &fakeDiagnosticProbe{icmp: icmpProbeResult{Sent: 10, LossRate: 100},
		egress: func(a AdapterView) diagnosticEgressResult {
			if a.Address == "" {
				return diagnosticEgressResult{TCP: true, TLS: true, Detail: "v6 verified"}
			}
			return diagnosticEgressResult{Detail: "v4 timeout"}
		}})
	r := s.runAdapter(context.Background(), AdapterView{Address: "192.0.2.1", SourceIPv6: "2001:db8::1"})
	if r.Status != "available" || !r.BoundTCPOK || r.LossRate != 100 {
		t.Fatalf("%+v", r)
	}
	if r.Checks[4].Level != "warn" || r.Checks[5].Level != "pass" {
		t.Fatalf("%+v", r.Checks)
	}
}

func TestDiagnosticsInvalidatesChangedAdapter(t *testing.T) {
	s := newTestDiagnostics(t, &fakeDiagnosticProbe{tcpOK: true, tlsOK: true})
	list := s.listAdapters
	calls := 0
	s.listAdapters = func() ([]AdapterView, error) {
		adapters, err := list()
		calls++
		if calls > 1 {
			adapters[0].Address = "192.0.2.11"
		}
		return adapters, err
	}
	r, err := s.Run([]string{"ethernet"})
	if err != nil || r.Results[0].Status != "unverified" {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestDiagnosticsReservesRunDuringDiscovery(t *testing.T) {
	s := newTestDiagnostics(t, &fakeDiagnosticProbe{})
	entered, release := make(chan struct{}), make(chan struct{})
	list := s.listAdapters
	s.listAdapters = func() ([]AdapterView, error) { close(entered); <-release; return list() }
	done := make(chan struct{})
	go func() { defer close(done); _, _ = s.Run([]string{"ethernet"}) }()
	<-entered
	if _, err := s.Run([]string{"ethernet"}); err == nil {
		t.Error("accepted overlapping run")
	}
	s.Cancel()
	close(release)
	<-done
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

func TestDiagnosticsUsesTLSWithoutTreatingICMPAsOutage(t *testing.T) {
	service := newTestDiagnostics(t, &fakeDiagnosticProbe{
		icmp: icmpProbeResult{
			Status: "unstable", LossRate: 20, AvgLatencyMS: 42,
			JitterMS: 140, Sent: 10, Received: 8,
		},
		tcpOK: true, tlsOK: true, tcpDetail: "TCP + TLS verified via 192.0.2.10",
	})
	snapshot, err := service.Run([]string{"ethernet"})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.State != "completed" || len(snapshot.Results) != 1 {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
	result := snapshot.Results[0]
	if result.Status != "available" || result.LossRate != 20 || result.AvgLatencyMS != 42 || result.JitterMS != 140 {
		t.Fatalf("v2.2.0 result semantics changed: %+v", result)
	}
	if len(result.Checks) != 6 || result.Checks[1].Level != "pass" {
		t.Fatalf("configuration checks missing: %+v", result.Checks)
	}
}

func TestTCPFailureDoesNotInventICMPLoss(t *testing.T) {
	service := newTestDiagnostics(t, &fakeDiagnosticProbe{
		icmp:  icmpProbeResult{Status: "available", Sent: 5, Received: 5, LossRate: 0},
		tcpOK: false, tcpDetail: "bind failed",
	})
	result := service.runAdapter(context.Background(), AdapterView{})
	if result.Status != "limited" || result.LossRate != 0 {
		t.Fatalf("TCP status overwrote ICMP measurements: %+v", result)
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
