package services

import (
	"testing"
	"time"
)

func TestThroughputSharedSamplingWindow(t *testing.T) {
	start := time.Unix(1000, 0)
	sample := func(ms, down int64) telemetryResult {
		v := telemetryResult{StartedAt: start, SampledAt: start.Add(time.Duration(ms) * time.Millisecond)}
		v.Total.BytesDown, v.Total.BytesUp = down, down/2
		v.Adapters = []adapterTelemetry{{Name: "Ethernet", BytesDown: down, BytesUp: down / 2}}
		return v
	}
	var last telemetrySample
	for _, step := range []struct {
		ms, down int64
		want     float64
	}{
		{0, 10000, 0}, // Existing session traffic is not a speed sample.
		{800, 18000, 10000},
		{801, 18000, 10000}, // Another reader must not replace the baseline.
		{802, 20000, 10000}, // A relay buffer burst must not become a spike.
		{800, 18000, 10000}, // Duplicate response.
		{400, 14000, 10000}, // Out-of-order response.
		{1600, 26000, 10000},
		{2400, 26000, 0},     // Idle traffic returns to zero in the next window.
		{3400, 36000, 10000}, // Use actual elapsed time, not the poll interval.
	} {
		last.update(sample(step.ms, step.down))
		if last.downloadBPS != step.want || last.uploadBPS != step.want/2 ||
			last.adapterRates["Ethernet"] != [2]float64{step.want, step.want / 2} {
			t.Fatalf("at %d ms: total=%v/%v adapter=%v, want %v/%v", step.ms,
				last.downloadBPS, last.uploadBPS, last.adapterRates["Ethernet"], step.want, step.want/2)
		}
	}
}

func TestThroughputResetsBaseline(t *testing.T) {
	start := time.Unix(1000, 0)
	for _, reason := range []string{"restart", "counter reset", "long gap", "adapter reset"} {
		t.Run(reason, func(t *testing.T) {
			last := telemetrySample{
				startedAt: start, at: start.Add(time.Second), down: 1000, up: 500,
				adapters:    map[string][2]int64{"Ethernet": {1000, 500}},
				downloadBPS: 1000, uploadBPS: 500,
			}
			next := telemetryResult{StartedAt: start, SampledAt: start.Add(2 * time.Second)}
			next.Total.BytesDown, next.Total.BytesUp = 2000, 1000
			next.Adapters = []adapterTelemetry{{Name: "Ethernet", BytesDown: 2000, BytesUp: 1000}}
			switch reason {
			case "restart":
				next.StartedAt = start.Add(1500 * time.Millisecond)
			case "counter reset":
				next.Total.BytesDown, next.Total.BytesUp = 100, 50
			case "long gap":
				next.SampledAt = start.Add(time.Minute)
			case "adapter reset":
				next.Adapters[0].BytesDown, next.Adapters[0].BytesUp = 100, 50
			}
			last.update(next)
			if reason != "adapter reset" && (last.downloadBPS != 0 || last.uploadBPS != 0) {
				t.Fatalf("invalid interval produced rates: %+v", last)
			}
			if last.adapterRates["Ethernet"] != [2]float64{} {
				t.Fatalf("invalid adapter interval produced rates: %+v", last.adapterRates)
			}
			next.SampledAt = next.SampledAt.Add(time.Second)
			next.Total.BytesDown += 4000
			next.Total.BytesUp += 2000
			next.Adapters[0].BytesDown += 4000
			next.Adapters[0].BytesUp += 2000
			last.update(next)
			if last.downloadBPS != 4000 || last.uploadBPS != 2000 || last.adapterRates["Ethernet"] != [2]float64{4000, 2000} {
				t.Fatalf("measurement did not recover: %+v", last)
			}
		})
	}
}
