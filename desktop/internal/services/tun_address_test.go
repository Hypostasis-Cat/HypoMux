package services

import (
	"net/netip"
	"testing"
)

func TestSelectTunIPv4Address(t *testing.T) {
	for _, test := range []struct {
		name     string
		occupied []string
		want     string
	}{
		{"default", nil, "172.19.0.1/30"},
		{"Hyper-V issue 75", []string{"172.19.0.1/20"}, "172.16.255.1/30"},
		{"subnet overlap without equal host", []string{"172.19.0.2/24"}, "172.16.255.1/30"},
		{"VPN covers 172 space", []string{"172.16.0.0/12"}, "10.255.255.1/30"},
		{"fallback to remaining private pool", []string{"172.16.0.0/12", "10.0.0.0/8"}, "192.168.0.1/30"},
		{"all private pools occupied", []string{"172.16.0.0/12", "10.0.0.0/8", "192.168.0.0/16"}, ""},
		{"fragmented default with small LAN gap", []string{"128.0.0.0/2", "10.0.0.0/9", "10.128.0.0/10", "10.192.0.0/11", "10.224.0.0/12", "10.240.0.0/13", "10.248.0.0/14", "10.252.0.0/15", "10.254.0.0/16", "10.255.128.0/17"}, "10.255.0.1/30"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var prefixes []netip.Prefix
			for _, value := range test.occupied {
				prefixes = append(prefixes, netip.MustParsePrefix(value))
			}
			got, err := selectTunIPv4Address(prefixes)
			if got != test.want || (err != nil) != (test.want == "") {
				t.Fatalf("address = %q, error = %v; want %q", got, err, test.want)
			}
		})
	}
}

func TestSelectTunIPv6Address(t *testing.T) {
	for _, tc := range []struct{ occupied, want string }{
		{"", "fdfe:dcba:9876::1/126"},
		{"fdfe:dcba:9876::/64", "fdfe:dcba:9877::1/126"},
		{"fdfe:dcba::/32", "fd00:dcba:9876::1/126"},
		{"fc00::/18", "fdfe:dcba:9876::1/126"},
		{"fd00::/8", ""},
	} {
		var occupied []netip.Prefix
		if tc.occupied != "" {
			occupied = append(occupied, netip.MustParsePrefix(tc.occupied))
		}
		got, err := selectTunIPv6Address(occupied)
		if got != tc.want || (err != nil) != (tc.want == "") {
			t.Fatalf("occupied=%s got=%s err=%v want=%s", tc.occupied, got, err, tc.want)
		}
	}
}
