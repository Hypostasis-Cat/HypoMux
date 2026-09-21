package services

import (
	"net/netip"
	"testing"
)

func TestLocalDiscoveryStaysOutsideTun(t *testing.T) {
	for _, policy := range []string{"auto", "off", "system"} {
		for _, ipv6 := range []bool{false, true} {
			exclusions := tunRouteExclusions(dnsResolveResult{Transport: "udp", Server: "1.1.1.1"}, policy, ipv6)
			for _, tc := range []struct {
				address  string
				excluded bool
			}{
				{"224.0.0.251", true},     // mDNS
				{"239.255.255.250", true}, // SSDP
				{"224.0.0.252", true},     // LLMNR
				{"255.255.255.255", true},
				{"169.254.10.20", true},
				{"ff02::fb", ipv6},
				{"fe80::1234", ipv6},
				{"1.1.1.1", policy != "system"},
				{"8.8.8.8", false},
				{"192.168.1.20", false}, // user private-network rules remain applicable
				{"10.0.0.1", false},
				{"198.18.0.1", false}, // FakeIP must still enter TUN
				{"fc00::1234", false},
				{"fd12::1234", false},
			} {
				found := false
				for _, prefix := range exclusions {
					if netip.MustParsePrefix(prefix).Contains(netip.MustParseAddr(tc.address)) {
						found = true
					}
				}
				if found != tc.excluded {
					t.Fatalf("policy=%s ipv6=%v address=%s excluded=%v want=%v", policy, ipv6, tc.address, found, tc.excluded)
				}
			}
		}
	}
}
