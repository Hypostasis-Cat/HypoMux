package services

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Hypostasis-Cat/HypoMux/desktop/internal/engineclient"
)

func startTestRuntimeBindingMonitor(t *testing.T, service *EngineService) (tick, stop func()) {
	t.Helper()
	done, exited := make(chan struct{}), make(chan struct{})
	ticks := make(chan time.Time)
	go func() {
		defer close(exited)
		service.runRuntimeBindingMonitor(done, ticks)
	}()
	stop = sync.OnceFunc(func() {
		close(done)
		select {
		case <-exited:
		case <-time.After(time.Second):
			t.Error("backend monitor did not exit")
		}
	})
	t.Cleanup(stop)
	tick = func() {
		t.Helper()
		select {
		case ticks <- time.Now():
		case <-exited:
			t.Fatal("backend monitor exited before the tick")
		case <-time.After(time.Second):
			t.Fatal("backend monitor did not consume the tick")
		}
	}
	return tick, stop
}

func TestRuntimeBindingMonitorScansWithoutSnapshotsAndStopsScanningAfterStop(t *testing.T) {
	old := runtimeBindingFixture()
	var scans atomic.Int64
	scanned := make(chan struct{}, 1)
	service := &EngineService{lifecycleGate: make(chan struct{}, 1), runtimeBindings: []AdapterView{old},
		runtimeBindingEnabled: true, runtimeAdapters: func() ([]AdapterView, error) {
			scans.Add(1)
			scanned <- struct{}{}
			return []AdapterView{old}, nil
		}}
	tick, stop := startTestRuntimeBindingMonitor(t, service)
	tick()
	select {
	case <-scanned:
	case <-time.After(time.Second):
		t.Fatal("backend did not scan without a Snapshot call")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := service.acquireLifecycle(ctx); err != nil {
		t.Fatal(err)
	}
	service.releaseLifecycle()
	service.cancelRuntimeBindingRefresh()
	tick()
	stop()
	if err := service.acquireLifecycle(ctx); err != nil {
		t.Fatal(err)
	}
	service.releaseLifecycle()
	if scans.Load() != 1 {
		t.Fatal("stopped session still scanned adapters", scans.Load())
	}
}

func TestRuntimeBindingMonitorSkipsInactiveAndBusySessions(t *testing.T) {
	for _, scenario := range []string{"stopped", "closing", "no_bindings", "busy"} {
		t.Run(scenario, func(t *testing.T) {
			old := runtimeBindingFixture()
			var scans atomic.Int64
			service := &EngineService{lifecycleGate: make(chan struct{}, 1), runtimeBindings: []AdapterView{old},
				runtimeBindingEnabled: true, runtimeAdapters: func() ([]AdapterView, error) {
					scans.Add(1)
					return []AdapterView{old}, nil
				}}
			switch scenario {
			case "stopped":
				service.runtimeBindingEnabled = false
			case "closing":
				service.closing = true
			case "no_bindings":
				service.runtimeBindings = nil
			case "busy":
				service.lifecycleGate <- struct{}{}
			}
			tick, stop := startTestRuntimeBindingMonitor(t, service)
			tick()
			stop()
			if scenario == "busy" {
				service.releaseLifecycle()
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := service.acquireLifecycle(ctx); err != nil {
				t.Fatal(err)
			}
			service.releaseLifecycle()
			if scans.Load() != 0 {
				t.Fatal("inactive/busy session scanned adapters", scans.Load())
			}
		})
	}
}

func TestRuntimeBindingMonitorExitsWhenClientPermanentlyCloses(t *testing.T) {
	service := &EngineService{client: engineclient.New(), bindingMonitorDone: make(chan struct{})}
	t.Cleanup(service.client.Close)
	go service.watchRuntimeBindings()
	service.client.Close()
	select {
	case <-service.bindingMonitorDone:
	case <-time.After(time.Second):
		t.Fatal("closing the client left the backend monitor running")
	}
}

func runtimeBindingFixture() AdapterView {
	return AdapterView{ID: "wifi", Name: "WLAN", SourceIPv6: "2001:db8::1", IPv6IfIndex: 7,
		DNSServers: []string{"fe80::1%7"}, Operational: true, Selected: true, Weight: 1}
}

func TestRuntimeBindingChangeIncludesBothFamiliesIndicesAndDNS(t *testing.T) {
	old := runtimeBindingFixture()
	old.Address, old.IfIndex = "192.0.2.1", 6
	for _, field := range []string{"ipv6", "ipv4", "index6", "index4", "dns", "lost_v4", "lost_v6"} {
		t.Run(field, func(t *testing.T) {
			next := cloneRuntimeBindings([]AdapterView{old})[0]
			switch field {
			case "ipv6":
				next.SourceIPv6 = "2001:db8::2"
			case "ipv4":
				next.Address = "192.0.2.2"
			case "index6":
				next.IPv6IfIndex++
			case "index4":
				next.IfIndex++
			case "dns":
				next.DNSServers[0] = "2001:db8::53"
			case "lost_v4":
				next.Address = ""
			case "lost_v6":
				next.SourceIPv6 = ""
			}
			changed, err := runtimeBindingsChanged([]AdapterView{old}, []AdapterView{next})
			if !changed || err != nil {
				t.Fatal(changed, err)
			}
		})
	}
	old.DNSServers = nil
	next := old
	next.Weight, next.IPv6Metric, next.Description = 5, 30, "renamed description"
	next.DNSServers = []string{}
	changed, err := runtimeBindingsChanged([]AdapterView{old}, []AdapterView{next, {ID: "other", SourceIPv6: "2001:db8::99"}})
	if changed || err != nil {
		t.Fatal("unrelated metadata or nil/empty DNS triggered recovery", changed, err)
	}
	if changed, err = runtimeBindingsChanged([]AdapterView{old}, nil); changed || err == nil {
		t.Fatal("missing NIC must wait for a usable binding", changed, err)
	}
	peer := old
	peer.ID, peer.Name = "peer", "peer"
	next.SourceIPv6 = "2001:db8::2"
	if changed, err = runtimeBindingsChanged([]AdapterView{peer, old}, []AdapterView{next}); !changed || err != nil {
		t.Fatal("missing peer blocked recovery of the still-selected usable NIC", changed, err)
	}
}

func TestRuntimeBindingRecoveryGatesCleanupStartupAndOwnedHotspot(t *testing.T) {
	for _, scenario := range []string{"changed", "unchanged", "missing", "scan_error", "stopped", "status_error", "stop_error", "start_error", "cancel_after_stop", "hotspot", "hotspot_error"} {
		t.Run(scenario, func(t *testing.T) {
			old := runtimeBindingFixture()
			next := cloneRuntimeBindings([]AdapterView{old})[0]
			next.SourceIPv6 = "2001:db8::2"
			service := &EngineService{runtimeBindings: []AdapterView{old}, runtimeBindingMode: "tun", runtimeBindingEnabled: true}
			service.runtimeAdapters = func() ([]AdapterView, error) {
				if scenario == "scan_error" {
					return nil, errors.New("inspection incomplete")
				}
				if scenario == "missing" {
					return nil, nil
				}
				if scenario == "unchanged" {
					return []AdapterView{old}, nil
				}
				return []AdapterView{next}, nil
			}
			config := HotspotConfig{SSID: "owned", Password: "owned-secret", Band: "auto"}
			if strings.HasPrefix(scenario, "hotspot") {
				service.hotspot = &hotspotSession{config: config, status: HotspotStatus{State: "running"}}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var calls []string
			err := service.refreshRuntimeBindings(ctx, runtimeBindingActions{
				status: func(context.Context) (string, error) {
					calls = append(calls, "status")
					if scenario == "stopped" {
						return "stopped", nil
					}
					if scenario == "status_error" {
						return "", errors.New("Core unavailable")
					}
					return "running", nil
				},
				stop: func(context.Context) error {
					calls = append(calls, "stop")
					if scenario == "stop_error" {
						return errors.New("cleanup failed")
					}
					if scenario == "cancel_after_stop" {
						cancel()
					}
					return nil
				},
				start: func(_ context.Context, mode string) error {
					if mode != "tun" {
						t.Fatal("recovery changed the active mode", mode)
					}
					calls = append(calls, "start")
					if scenario == "start_error" {
						return errors.New("new address disappeared")
					}
					return nil
				},
				hotspot: func(_ context.Context, got HotspotConfig) error {
					if got != config {
						t.Fatal("recovery changed the owned hotspot")
					}
					calls = append(calls, "hotspot")
					if scenario == "hotspot_error" {
						return errors.New("sharing unavailable")
					}
					return nil
				},
			})
			want := []string{"status", "stop", "start"}
			wantErr := false
			switch scenario {
			case "unchanged", "missing":
				want = nil
			case "scan_error":
				want, wantErr = nil, true
			case "stopped":
				want = []string{"status"}
			case "status_error":
				want, wantErr = []string{"status"}, true
			case "stop_error", "cancel_after_stop":
				want, wantErr = []string{"status", "stop"}, true
			case "start_error":
				wantErr = true
			case "hotspot", "hotspot_error":
				want = append(want, "hotspot")
				wantErr = scenario == "hotspot_error"
			}
			if !reflect.DeepEqual(calls, want) || (err != nil) != wantErr {
				t.Fatal(calls, want, err)
			}
			if scenario == "missing" && !strings.Contains(service.runtimeBindingNotice, "等待网卡") {
				t.Fatal("missing interface was not reported")
			}
			if scenario == "cancel_after_stop" && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		})
	}
}

func TestRuntimeBindingStopCancelsScanAndPreventsLateRestart(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	releaseScan := sync.OnceFunc(func() { close(release) })
	t.Cleanup(releaseScan)
	var scans atomic.Int64
	old := runtimeBindingFixture()
	service := &EngineService{lifecycleGate: make(chan struct{}, 1), runtimeBindings: []AdapterView{old},
		runtimeBindingMode: "proxy", runtimeBindingEnabled: true}
	service.runtimeAdapters = func() ([]AdapterView, error) {
		scans.Add(1)
		close(entered)
		<-release
		next := old
		next.SourceIPv6 = "2001:db8::2"
		return []AdapterView{next}, nil
	}
	tick, stop := startTestRuntimeBindingMonitor(t, service)
	tick()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("background scan did not start")
	}
	tick()
	service.cancelRuntimeBindingRefresh()
	releaseScan()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := service.acquireLifecycle(ctx); err != nil {
		t.Fatal("recovery retained lifecycle ownership after Stop", err)
	}
	service.releaseLifecycle()
	tick()
	stop()
	service.mu.Lock()
	enabled, refreshing, notice := service.runtimeBindingEnabled, service.runtimeBindingRefresh, service.runtimeBindingNotice
	service.mu.Unlock()
	if scans.Load() != 1 || enabled || refreshing || notice != "" {
		t.Fatal("late recovery survived explicit Stop", scans.Load(), enabled, refreshing, notice)
	}
}

func TestRuntimeBindingPoolUpdateRetainsOldExplicitBindingAndCopiesDNS(t *testing.T) {
	old := runtimeBindingFixture()
	service := &EngineService{runtimeBindings: cloneRuntimeBindings([]AdapterView{old}), runtimeBindingEnabled: true}
	next, added := old, old
	next.SourceIPv6 = "2001:db8::2"
	added.ID, added.Name = "new", "new"
	service.rememberAdditionalRuntimeBindings([]AdapterView{next, added})
	added.DNSServers[0] = "2001:db8::99"
	if len(service.runtimeBindings) != 2 || service.runtimeBindings[0].SourceIPv6 != "2001:db8::1" || service.runtimeBindings[1].DNSServers[0] != "fe80::1%7" {
		t.Fatal("pool update hid a stale explicit channel or aliased DNS metadata", service.runtimeBindings)
	}
}

func TestRuntimeBindingReturningSelectionIgnoresUnselectedOrUnusableNICs(t *testing.T) {
	old := runtimeBindingFixture()
	for _, scenario := range []string{"selected", "unselected", "down", "no_address"} {
		t.Run(scenario, func(t *testing.T) {
			next := old
			next.ID, next.Name = "returning", "returning"
			switch scenario {
			case "unselected":
				next.Selected = false
			case "down":
				next.Operational = false
			case "no_address":
				next.SourceIPv6 = ""
			}
			changed, err := runtimeBindingsChanged([]AdapterView{old}, []AdapterView{old, next})
			if err != nil || changed != (scenario == "selected") {
				t.Fatal(changed, err)
			}
		})
	}
}

func TestRuntimeBindingRetryWaitsForAddressAndPreservesHotspot(t *testing.T) {
	old := runtimeBindingFixture()
	next := old
	next.SourceIPv6 = "2001:db8::2"
	available := []AdapterView{next}
	config := HotspotConfig{SSID: "owned", Password: "owned-secret", Band: "auto"}
	s := &EngineService{runtimeBindings: []AdapterView{old}, runtimeBindingMode: "tun", runtimeBindingEnabled: true,
		hotspot:         &hotspotSession{config: config, status: HotspotStatus{State: "running"}},
		runtimeAdapters: func() ([]AdapterView, error) { return available, nil }}
	starts, stops, restores := 0, 0, 0
	actions := runtimeBindingActions{
		status: func(context.Context) (string, error) { return "running", nil },
		stop:   func(context.Context) error { stops++; s.hotspot = nil; return nil },
		start: func(context.Context, string) error {
			starts++
			if starts == 1 {
				return errors.New("temporary failure")
			}
			s.rememberRuntimeBindingsLocked("tun", available, AdapterView{})
			return nil
		},
		hotspot: func(_ context.Context, got HotspotConfig) error {
			restores++
			if got != config {
				t.Fatal("lost original hotspot configuration")
			}
			return nil
		},
	}
	if err := s.refreshRuntimeBindings(context.Background(), actions); err == nil {
		t.Fatal("expected startup failure")
	}
	available = nil
	if err := s.refreshRuntimeBindings(context.Background(), actions); err != nil {
		t.Fatal(err)
	}
	if starts != 1 || stops != 1 || restores != 0 {
		t.Fatal("attempted recovery before an address returned")
	}
	available = []AdapterView{old} // Restoring the old address must also retry.
	if err := s.refreshRuntimeBindings(context.Background(), actions); err != nil {
		t.Fatal(err)
	}
	if starts != 2 || stops != 2 || restores != 1 || s.runtimeBindingRecovery != nil {
		t.Fatal("recovery intent was lost or not retired")
	}
}

func TestRuntimeBindingStopHotspotRevokesPendingHotspotRestore(t *testing.T) {
	config := HotspotConfig{SSID: "owned", Password: "owned-secret", Band: "auto"}
	s := &EngineService{lifecycleGate: make(chan struct{}, 1), runtimeBindingRecovery: &runtimeBindingRecovery{hotspot: &config}}
	if _, err := s.StopHotspot(); err != nil {
		t.Fatal(err)
	}
	if s.runtimeBindingRecovery == nil || s.runtimeBindingRecovery.hotspot != nil {
		t.Fatal("StopHotspot must revoke only the pending hotspot restore")
	}
}
