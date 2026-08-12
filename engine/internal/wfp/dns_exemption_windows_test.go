//go:build windows

package wfp

import (
	"runtime"
	"testing"
	"unsafe"
)

func TestWFPStructureLayoutsAMD64(t *testing.T) {
	if runtime.GOARCH != "amd64" {
		t.Skip("SDK layout assertions currently cover the Windows amd64 release target")
	}
	assertSize := func(name string, got, want uintptr) {
		t.Helper()
		if got != want {
			t.Fatalf("%s size = %d, want %d", name, got, want)
		}
	}
	assertSize("value", unsafe.Sizeof(value{}), 16)
	assertSize("condition", unsafe.Sizeof(filterCondition{}), 40)
	assertSize("filter", unsafe.Sizeof(filter{}), 200)
	assertSize("session", unsafe.Sizeof(session{}), 72)
	assertSize("sublayer", unsafe.Sizeof(subLayer{}), 72)
}

func TestBuildDNSRulesIsNarrowAndDeduplicated(t *testing.T) {
	rules := buildRules([]Adapter{
		{Name: "Ethernet", SourceIP: "192.168.10.24", IfIndex: 12},
		{Name: "duplicate", SourceIP: "192.168.10.24", IfIndex: 12},
		{Name: "invalid", SourceIP: "not-an-ip", IfIndex: 9},
	})
	if len(rules) != 2 {
		t.Fatalf("rules = %#v", rules)
	}
	if rules[0].protocol != ipProtoUDP || rules[1].protocol != ipProtoTCP {
		t.Fatalf("protocols = %#v", rules)
	}
}

func TestBuildDNSRulesUsesHostOrderIP(t *testing.T) {
	rules := buildRules([]Adapter{
		{Name: "Ethernet", SourceIP: "192.168.10.24", IfIndex: 12},
	})
	if len(rules) == 0 {
		t.Fatal("buildRules returned no rules")
	}
	// WFP expects FWPM_CONDITION_IP_LOCAL_ADDRESS values in host byte order
	// (ntohl of the network-order bytes), so 192.168.10.24 must be 0xC0A80A18.
	const want = uint32(0xC0A80A18)
	if rules[0].sourceIP != want {
		t.Fatalf("sourceIP = 0x%08X, want 0x%08X (host byte order)", rules[0].sourceIP, want)
	}
}
