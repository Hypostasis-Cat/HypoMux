package services

import "time"

// Share a measurement window across status readers. Closely spaced requests
// must not consume the baseline and amplify individual relay buffer writes.
const minimumThroughputInterval = 500 * time.Millisecond

// update is called with EngineService.mu held.
func (s *telemetrySample) update(next telemetryResult) {
	if next.SampledAt.IsZero() {
		return
	}
	if !s.at.IsZero() && (next.StartedAt.Before(s.startedAt) ||
		(next.StartedAt.Equal(s.startedAt) && !next.SampledAt.After(s.at))) {
		return
	}
	elapsed := next.SampledAt.Sub(s.at)
	valid := !s.at.IsZero() && next.StartedAt.Equal(s.startedAt) &&
		elapsed > 0 && elapsed < 30*time.Second &&
		next.Total.BytesDown >= s.down && next.Total.BytesUp >= s.up
	if valid && elapsed < minimumThroughputInterval {
		return
	}
	current := telemetrySample{
		startedAt: next.StartedAt, at: next.SampledAt,
		down: next.Total.BytesDown, up: next.Total.BytesUp,
		adapters:     make(map[string][2]int64, len(next.Adapters)),
		adapterRates: make(map[string][2]float64, len(next.Adapters)),
	}
	if valid {
		current.downloadBPS = float64(next.Total.BytesDown-s.down) / elapsed.Seconds()
		current.uploadBPS = float64(next.Total.BytesUp-s.up) / elapsed.Seconds()
	}
	for _, adapter := range next.Adapters {
		if previous, ok := s.adapters[adapter.Name]; ok && valid &&
			adapter.BytesDown >= previous[0] && adapter.BytesUp >= previous[1] {
			current.adapterRates[adapter.Name] = [2]float64{
				float64(adapter.BytesDown-previous[0]) / elapsed.Seconds(),
				float64(adapter.BytesUp-previous[1]) / elapsed.Seconds(),
			}
		}
		current.adapters[adapter.Name] = [2]int64{adapter.BytesDown, adapter.BytesUp}
	}
	*s = current
}
