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
		{"all candidates occupied", []string{"172.16.0.0/12", "10.0.0.0/8"}, ""},
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
