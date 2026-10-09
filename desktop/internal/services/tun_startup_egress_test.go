package services

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func usbDNSAdapters() []AdapterView {
	return []AdapterView{
		{ID: "Phone", Name: "Phone", Address: "10.213.252.43", Metric: 25, Operational: true, Selected: true},
		{ID: "WLAN", Name: "WLAN", Address: "10.34.75.57", Metric: 45, Operational: true, Selected: true},
		{ID: "Ethernet", Name: "Ethernet", Address: "10.34.23.24", Metric: 35, Operational: true, Selected: true},
		{ID: "Disabled", Name: "Disabled", Metric: 1, Operational: true},
		{ID: "Disconnected", Name: "Disconnected", Metric: 1, Selected: true},
	}
}

func TestTUNDNSEgressUSBFailureFallsBackWithBoundQueries(t *testing.T) {
	for _, pool := range []bool{false, true} {
		t.Run(map[bool]string{false: "single", true: "pool"}[pool], func(t *testing.T) {
			adapters := usbDNSAdapters()
			preferred := tunDNSEgressDecision{Adapter: adapters[0], Mode: DNSEgressAuto, Source: "system_default_route"}
			var attempts []string
			var verified []bool
			resolve := func(_ context.Context, domain, adapter string) (dnsResolveResult, error) {
				if domain == "" || adapter == "" {
					t.Fatal("query lost its domain or interface binding")
				}
				if adapter == "Phone" {
					return dnsResolveResult{}, errors.New("bound DNS timeout")
				}
				return dnsResolveResult{Adapter: adapter, Transport: "udp", Server: "125.221.35.8:53"}, nil
			}
			decision, prepared, err := prepareTUNDNSEgress(context.Background(), preferred, adapters,
				func(ctx context.Context, adapter AdapterView) (preparedTUNDNS, error) {
					attempts = append(attempts, adapter.Name)
					if pool {
						return prepareVerifiedTUNDNSPool(ctx, adapter, false, resolve,
							func(_ context.Context, a AdapterView) ([]dnsResolveResult, error) {
								if a.ID != "Ethernet" {
									t.Fatal("built pool for failed interface", a.ID)
								}
								return []dnsResolveResult{{Transport: "doh", Server: "dns.alidns.com@223.5.5.5:443"}, {Transport: "udp", Server: "125.221.35.8:53"}}, nil
							})
					}
					result, _, err := prepareTUNDNS(ctx, adapter, false, resolve, nil)
					return preparedTUNDNS{Result: result}, err
				}, func(_ tunDNSEgressDecision, err error) { verified = append(verified, err == nil) })
			if err != nil || decision.Adapter.ID != "Ethernet" || decision.Source != "verified_fallback" ||
				prepared.Result.Adapter != "Ethernet" || prepared.Result.Server != "125.221.35.8:53" {
				t.Fatalf("decision=%+v prepared=%+v error=%v", decision, prepared, err)
			}
			if !reflect.DeepEqual(attempts, []string{"Phone", "Ethernet"}) || !reflect.DeepEqual(verified, []bool{false, true}) {
				t.Fatalf("attempts=%v verified=%v", attempts, verified)
			}
			if pool && len(prepared.Pool) != 2 {
				t.Fatal("configured pool was lost")
			}
		})
	}
}

func TestTUNDNSEgressAllFailedAndExplicitModes(t *testing.T) {
	for _, mode := range []string{DNSEgressAuto, DNSEgressAdapter, DNSEgressSystem} {
		t.Run(mode, func(t *testing.T) {
			adapters := usbDNSAdapters()
			var attempts []string
			_, result, err := prepareTUNDNSEgress(context.Background(), tunDNSEgressDecision{Adapter: adapters[0], Mode: mode}, adapters,
				func(_ context.Context, adapter AdapterView) (preparedTUNDNS, error) {
					attempts = append(attempts, adapter.ID)
					return preparedTUNDNS{}, errors.New("DNS timeout")
				}, nil)
			want := []string{"Phone"}
			if mode == DNSEgressAuto {
				want = append(want, "Ethernet", "WLAN")
			}
			if err == nil || !strings.Contains(err.Error(), "已取消启动") || result.Result.Server != "" || !reflect.DeepEqual(attempts, want) {
				t.Fatalf("attempts=%v result=%+v error=%v", attempts, result, err)
			}
			for _, name := range want {
				if !strings.Contains(err.Error(), name) {
					t.Fatal("failure omitted adapter", name)
				}
			}
		})
	}
}

func TestTUNDNSEgressKeepsHealthyPreferredAndStopsOnCancellation(t *testing.T) {
	adapters := usbDNSAdapters()
	preferred := tunDNSEgressDecision{Adapter: adapters[1], Mode: DNSEgressAuto, Source: "ipv4_default_rule"}
	calls := 0
	decision, _, err := prepareTUNDNSEgress(context.Background(), preferred, adapters,
		func(context.Context, AdapterView) (preparedTUNDNS, error) {
			calls++
			return preparedTUNDNS{}, nil
		}, nil)
	if err != nil || !reflect.DeepEqual(decision, preferred) || calls != 1 {
		t.Fatalf("preferred changed: %+v %v calls=%d", decision, err, calls)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	calls = 0
	_, _, err = prepareTUNDNSEgress(ctx, preferred, adapters,
		func(ctx context.Context, _ AdapterView) (preparedTUNDNS, error) {
			calls++
			<-ctx.Done()
			return preparedTUNDNS{}, ctx.Err()
		}, nil)
	if !errors.Is(err, context.DeadlineExceeded) || calls != 1 {
		t.Fatalf("cancelled startup continued: calls=%d error=%v", calls, err)
	}
}

func TestForcedTUNDNSPoolSkipsProbeButRequiresConfiguration(t *testing.T) {
	adapter := usbDNSAdapters()[0]
	result, err := prepareVerifiedTUNDNSPool(context.Background(), adapter, true, nil,
		func(context.Context, AdapterView) ([]dnsResolveResult, error) {
			return []dnsResolveResult{{Transport: "doh", Server: "dns.google@8.8.8.8:443"}}, nil
		})
	if err != nil || result.Result.Adapter != "Phone" || result.Result.Transport != "doh" {
		t.Fatalf("force changed policy: %+v %v", result, err)
	}
	_, err = prepareVerifiedTUNDNSPool(context.Background(), adapter, true, nil,
		func(context.Context, AdapterView) ([]dnsResolveResult, error) { return nil, nil })
	if err == nil {
		t.Fatal("force accepted empty DNS configuration")
	}
}
