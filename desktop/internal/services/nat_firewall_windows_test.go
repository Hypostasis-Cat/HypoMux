//go:build windows

package services

import (
	"strings"
	"testing"
)

func TestFirewallRuleAllowsProgramUDP(t *testing.T) {
	executable := `C:\Program Files\HypoMux\HypoMux.exe`
	rule := `v2.35|Action=Allow|Active=TRUE|Dir=In|Protocol=17|App=C:\Program Files\HypoMux\HypoMux.exe|Name=HypoMux NAT Type Detection|`
	if !firewallRuleAllowsProgramUDP(rule, executable) {
		t.Fatal("expected matching inbound UDP rule")
	}
	if firewallRuleAllowsProgramUDP(strings.Replace(rule, "Protocol=17", "Protocol=6", 1), executable) {
		t.Fatal("TCP rule must not satisfy UDP allowance")
	}
}
