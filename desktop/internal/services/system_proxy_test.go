//go:build windows

package services

import "testing"

func TestHypoMuxProxyOwnershipPatternIsNarrow(t *testing.T) {
	for _, value := range []string{
		"http=127.0.0.1:10801;https=127.0.0.1:10801;socks=127.0.0.1:10800",
		"http=127.0.0.1:1;https=127.0.0.1:2;socks=127.0.0.1:3",
	} {
		if !hypoMuxProxyServerPattern.MatchString(value) {
			t.Fatalf("expected owned proxy syntax: %s", value)
		}
	}
	for _, value := range []string{
		"127.0.0.1:10801",
		"http=192.0.2.1:10801;https=127.0.0.1:10801;socks=127.0.0.1:10800",
		"http=127.0.0.1:10801;https=127.0.0.1:10801;socks=127.0.0.1:10800;extra=1",
	} {
		if hypoMuxProxyServerPattern.MatchString(value) {
			t.Fatalf("unexpected ownership match: %s", value)
		}
	}
}
