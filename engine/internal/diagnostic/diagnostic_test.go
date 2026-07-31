package diagnostic

import (
	"context"
	"testing"
)

func TestRunRejectsInvalidAddressesWithoutProbing(t *testing.T) {
	result := Run(context.Background(), Config{
		SourceIP: "not-an-ip",
		TargetIP: DefaultTargetIP,
	})
	if result.Status != "unavailable" || result.Note != "invalid --src-ip" {
		t.Fatalf("invalid source result = %#v", result)
	}
	if result.Sent != 0 || result.LossRate != 100 {
		t.Fatalf("invalid source counters = %#v", result)
	}

	result = Run(context.Background(), Config{
		SourceIP: "192.0.2.1",
		TargetIP: "::1",
	})
	if result.Note != "invalid --target-ip" {
		t.Fatalf("invalid target result = %#v", result)
	}
}

func TestSummarizePreservesDiagnosticThresholds(t *testing.T) {
	base := Result{SourceIP: "192.0.2.1", TargetIP: DefaultTargetIP}

	available := summarize(base, []int{10, 15, 20}, 3, false, 0)
	if available.Status != "available" || available.AvgLatencyMS != 15 || available.JitterMS != 10 {
		t.Fatalf("available result = %#v", available)
	}

	unstableLoss := summarize(base, make([]int, 19), 20, false, 0)
	if unstableLoss.Status != "unstable" || unstableLoss.LossRate != 5 {
		t.Fatalf("loss result = %#v", unstableLoss)
	}

	unstableJitter := summarize(base, []int{1, 102}, 2, false, 0)
	if unstableJitter.Status != "unstable" || unstableJitter.JitterMS != 101 {
		t.Fatalf("jitter result = %#v", unstableJitter)
	}

	unavailable := summarize(base, nil, 10, true, 1231)
	if unavailable.Status != "unavailable" || unavailable.Note != "source bind failed (WinError 1231)" {
		t.Fatalf("bind failure result = %#v", unavailable)
	}
}

func TestWithDefaultsClampsUntrustedRequestValues(t *testing.T) {
	config := withDefaults(Config{SourceIP: " 192.0.2.1 ", Count: MaxCount + 1})
	if config.SourceIP != "192.0.2.1" || config.TargetIP != DefaultTargetIP {
		t.Fatalf("defaults = %#v", config)
	}
	if config.Count != MaxCount || config.Timeout != DefaultTimeout {
		t.Fatalf("limits = %#v", config)
	}
}
