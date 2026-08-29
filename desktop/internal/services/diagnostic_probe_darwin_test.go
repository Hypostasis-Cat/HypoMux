//go:build darwin

package services

import "testing"

func TestParseDarwinPing(t *testing.T) {
	input := `--- 1.1.1.1 ping statistics ---
10 packets transmitted, 9 packets received, 10.0% packet loss
round-trip min/avg/max/stddev = 12.250/20.500/48.750/4.000 ms
`
	got := parseDarwinPing(input)
	if got.Status != "unstable" || got.Sent != 10 || got.Received != 9 || got.LossRate != 10 || got.AvgLatencyMS != 21 || got.JitterMS != 37 {
		t.Fatalf("unexpected ping result: %#v", got)
	}
}
