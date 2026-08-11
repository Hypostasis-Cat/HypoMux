package services

import (
	"testing"
	"time"
)

func TestShouldRecordPerformance(t *testing.T) {
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		last     time.Time
		down     float64
		up       float64
		expected bool
	}{
		{name: "below threshold", down: 10*1024*1024 - 1},
		{name: "first high throughput sample", down: 10 * 1024 * 1024, expected: true},
		{name: "rate limited", last: now.Add(-4 * time.Second), down: 20 * 1024 * 1024},
		{name: "interval elapsed", last: now.Add(-5 * time.Second), down: 8 * 1024 * 1024, up: 3 * 1024 * 1024, expected: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := shouldRecordPerformance(now, test.last, test.down, test.up); got != test.expected {
				t.Fatalf("shouldRecordPerformance() = %v, want %v", got, test.expected)
			}
		})
	}
}
