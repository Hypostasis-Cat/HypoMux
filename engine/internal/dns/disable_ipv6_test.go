package dns

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestDisableIPv6ReturnsEmptyAAAAButPreservesA(t *testing.T) {
	var dials atomic.Int64
	r, err := New(context.Background(), Config{DisableIPv6: true, Policy: PolicyOff, LegacyServers: []string{"192.0.2.53"}, QueryTimeout: time.Second}, answeringDialer(t, &dials, "192.0.2.44", 60, 0))
	if err != nil {
		t.Fatal(err)
	}
	result, err := r.Resolve(context.Background(), Query{Domain: "test.example", RecordType: RecordAAAA, Binding: loopbackBinding})
	if err != nil || result.Address != "" || len(result.Addresses) != 0 || dials.Load() != 0 {
		t.Fatalf("AAAA was sent upstream: %+v %v dials=%d", result, err, dials.Load())
	}
	result, err = r.Resolve(context.Background(), Query{Domain: "test.example", RecordType: RecordA, Binding: loopbackBinding})
	if err != nil || result.Address != "192.0.2.44" || dials.Load() != 1 {
		t.Fatalf("A lookup damaged: %+v %v", result, err)
	}
}
