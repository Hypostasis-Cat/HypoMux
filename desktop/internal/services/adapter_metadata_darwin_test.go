//go:build darwin

package services

import (
	"reflect"
	"testing"
)

func TestParseDarwinServiceOrder(t *testing.T) {
	input := `(1) Thunderbolt Bridge
(Hardware Port: Thunderbolt Bridge, Device: bridge0)

(2) Wi-Fi
(Hardware Port: Wi-Fi, Device: en0)

(3) VPN
(Hardware Port: VPN, Device: )
`
	want := []darwinNetworkService{
		{Name: "Thunderbolt Bridge", HardwarePort: "Thunderbolt Bridge", Device: "bridge0"},
		{Name: "Wi-Fi", HardwarePort: "Wi-Fi", Device: "en0"},
	}
	if got := parseDarwinServiceOrder(input); !reflect.DeepEqual(got, want) {
		t.Fatalf("service order = %#v, want %#v", got, want)
	}
}

func TestParseDarwinDNSAssociatesScopedResolver(t *testing.T) {
	input := `resolver #1
  nameserver[0] : 1.1.1.1
  if_index : 15 (en0)

resolver #2
  nameserver[0] : 8.8.8.8
  nameserver[1] : 1.1.1.1
  if_index : 15 (en0)
`
	want := map[int][]string{15: {"1.1.1.1", "8.8.8.8"}}
	if got := parseDarwinDNS(input); !reflect.DeepEqual(got, want) {
		t.Fatalf("DNS metadata = %#v, want %#v", got, want)
	}
}
