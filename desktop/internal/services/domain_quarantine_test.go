package services

import (
	"context"
	"errors"
	"testing"
	"time"
)

type recordedEngineRequests struct {
	methods []string
	params  []map[string]any
	err     error
}

func (r *recordedEngineRequests) request(_ context.Context, method string, params any, _ any) error {
	r.methods = append(r.methods, method)
	r.params = append(r.params, params.(map[string]any))
	return r.err
}

func quarantineTelemetry(expiry *bool, expiresAt time.Time, domains ...string) telemetryResult {
	var telemetry telemetryResult
	for _, domain := range domains {
		telemetry.DomainQuarantines = append(telemetry.DomainQuarantines, struct {
			Adapter   string    `json:"adapter"`
			Domain    string    `json:"domain"`
			Evidence  int       `json:"evidence"`
			ExpiresAt time.Time `json:"expires_at"`
		}{Adapter: "WLAN", Domain: domain, ExpiresAt: expiresAt})
	}
	telemetry.DomainIsolationExpiry = expiry
	return telemetry
}

func TestSyncDomainQuarantinesPushesChangedPolicyAndSkipsSample(t *testing.T) {
	service := newBlockedDomainTestService(t, false)
	expiresAt := service.now().Add(30 * time.Minute)
	reported := true
	requests := &recordedEngineRequests{}
	capabilities := []string{domainQuarantineUpdateMethod}

	syncDomainQuarantines(context.Background(), requests.request, service, capabilities, false,
		service.runtimeSampleToken(), quarantineTelemetry(&reported, expiresAt, "a.example.com"))
	if len(requests.methods) != 1 || requests.methods[0] != domainQuarantineUpdateMethod ||
		requests.params[0]["expiry"] != false {
		t.Fatalf("requests = %#v", requests)
	}
	requireBlockedDomainRecords(t, service, nil)

	// Once the engine reports the desktop's policy, samples apply without a request.
	reported = false
	syncDomainQuarantines(context.Background(), requests.request, service, capabilities, false,
		service.runtimeSampleToken(), quarantineTelemetry(&reported, expiresAt, "a.example.com"))
	if len(requests.methods) != 1 {
		t.Fatalf("unexpected extra request: %#v", requests)
	}
	requireBlockedDomainRecords(t, service, map[string]time.Time{"WLAN/a.example.com": expiresAt})
}

func TestSyncDomainQuarantinesHandlesEngineWithoutLivePolicy(t *testing.T) {
	service := newBlockedDomainTestService(t, false)
	expiresAt := service.now().Add(30 * time.Minute)
	requests := &recordedEngineRequests{}

	// An older core reports no policy: nothing to compare, the sample still applies.
	syncDomainQuarantines(context.Background(), requests.request, service, nil, false,
		service.runtimeSampleToken(), quarantineTelemetry(nil, expiresAt, "a.example.com"))
	want := map[string]time.Time{"WLAN/a.example.com": expiresAt}
	requireBlockedDomainRecords(t, service, want)

	reported := true
	syncDomainQuarantines(context.Background(), requests.request, service, nil, false,
		service.runtimeSampleToken(), quarantineTelemetry(&reported, expiresAt, "b.example.com"))
	if len(requests.methods) != 0 {
		t.Fatalf("sent an update the core does not support: %#v", requests)
	}
	requireBlockedDomainRecords(t, service, want)
}

func TestSyncDomainQuarantinesDropsSampleFromBeforeRemoval(t *testing.T) {
	service := newBlockedDomainTestService(t, false)
	expiresAt := service.now().Add(30 * time.Minute)
	syncBlockedDomains(t, service, BlockedDomainEntry{Adapter: "WLAN", Domain: "a.example.com", ExpiresAt: expiresAt})
	reported := false
	token := service.runtimeSampleToken()
	if err := service.Remove("WLAN", "a.example.com"); err != nil {
		t.Fatal(err)
	}
	syncDomainQuarantines(context.Background(), (&recordedEngineRequests{}).request, service, nil, false,
		token, quarantineTelemetry(&reported, expiresAt, "a.example.com"))
	requireBlockedDomainRecords(t, service, nil)
}

func TestEvictAndCommit(t *testing.T) {
	params := map[string]any{"clear": true}
	capabilities := []string{domainQuarantineUpdateMethod}
	tests := []struct {
		name         string
		capabilities []string
		requestErr   error
		wantRequests int
		wantCommit   bool
	}{
		{name: "connected core", capabilities: capabilities, wantRequests: 1, wantCommit: true},
		{name: "core failure keeps record", capabilities: capabilities, requestErr: errors.New("timeout"), wantRequests: 1},
		{name: "no connected core or older core", wantCommit: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requests := &recordedEngineRequests{err: test.requestErr}
			committed := false
			err := evictAndCommit(context.Background(), requests.request, test.capabilities, params, func() error {
				if len(requests.methods) != test.wantRequests {
					t.Fatal("commit ran before the core removed the record")
				}
				committed = true
				return nil
			})
			if !errors.Is(err, test.requestErr) || (test.requestErr == nil && err != nil) {
				t.Fatalf("error = %v", err)
			}
			if committed != test.wantCommit || len(requests.methods) != test.wantRequests {
				t.Fatalf("committed = %t, requests = %#v", committed, requests)
			}
		})
	}
}

func TestEvictDomainQuarantinesReportsBusyLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("waits for the lifecycle timeout")
	}
	service := &EngineService{lifecycleGate: make(chan struct{}, 1)}
	service.lifecycleGate <- struct{}{}
	committed := false
	err := service.evictDomainQuarantines(map[string]any{"clear": true}, func() error {
		committed = true
		return nil
	})
	if err == nil || committed {
		t.Fatalf("err = %v, committed = %t", err, committed)
	}
}
