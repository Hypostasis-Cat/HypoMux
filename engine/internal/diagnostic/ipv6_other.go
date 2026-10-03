//go:build !windows

package diagnostic

import "fmt"

func newIPv6Prober() (ipv6Prober, error) {
	return nil, fmt.Errorf("IPv6 ICMP diagnostics require Windows")
}
