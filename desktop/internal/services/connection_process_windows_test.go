//go:build windows

package services

import "testing"

func TestNetworkPortDecoding(t *testing.T) {
	if got := networkPort(0x50C3); got != 50000 {
		t.Fatalf("networkPort(0x50C3) = %d; want 50000", got)
	}
}
