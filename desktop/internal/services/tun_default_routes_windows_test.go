//go:build windows

package services

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

func TestTunDefaultRouteSelection(t *testing.T) {
	for _, tc := range []struct {
		name                                          string
		vpnMetric, vpnInterfaceMetric, ethernetMetric int
		connected                                     bool
		alias                                         string
		want                                          []string
	}{
		{"installed Radmin backup", 9256, 1, 25, true, "Radmin VPN", []string{}},
		{"Radmin preferred", 0, 1, 25, true, "Radmin VPN", []string{"Radmin VPN"}},
		{"equal cost is a conflict", 24, 1, 25, true, "Radmin VPN", []string{"Radmin VPN"}},
		{"interface metric participates", 0, 100, 25, true, "Radmin VPN", []string{}},
		{"disconnected VPN", 0, 1, 25, false, "Radmin VPN", []string{}},
		{"active proxy still blocked", 0, 1, 25, true, "Mihomo", []string{"Mihomo"}},
		{"own stale route still cleaned", 9256, 1, 25, true, "HypoMux-Tun", []string{"HypoMux-Tun"}},
		{"only VPN route available", 9256, 1, -1, true, "Radmin VPN", []string{"Radmin VPN"}},
		{"no connected default routes", 0, 1, -1, false, "Radmin VPN", []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := "Disconnected"
			if tc.connected {
				state = "Connected"
			}
			ethernetState := "Connected"
			if tc.ethernetMetric < 0 {
				ethernetState = "Disconnected"
			}
			// Exercise the production PowerShell selector with read-only fixture
			// cmdlets so the regression covers PowerShell's object semantics too.
			script := fmt.Sprintf(`
$ErrorActionPreference = 'Stop'
function Get-NetIPInterface {
  [PSCustomObject]@{ InterfaceIndex = 45; ConnectionState = '%s'; InterfaceMetric = %d }
  [PSCustomObject]@{ InterfaceIndex = 10; ConnectionState = '%s'; InterfaceMetric = %d }
}
function Get-NetRoute {
  [PSCustomObject]@{ InterfaceIndex = 45; InterfaceAlias = '%s'; RouteMetric = %d; State = 'Alive' }
  [PSCustomObject]@{ InterfaceIndex = 10; InterfaceAlias = 'Ethernet'; RouteMetric = 0; State = 'Alive' }
}
`, state, tc.vpnInterfaceMetric, ethernetState, tc.ethernetMetric, tc.alias, tc.vpnMetric)
			output, err := runPreflightPowerShell(script + tunDefaultRouteInspectionScript + "\nConvertTo-Json -InputObject @($aliases) -Compress")
			if err != nil {
				t.Fatalf("route selection failed: %v; %s", err, output)
			}
			var aliases []string
			if err := json.Unmarshal(output, &aliases); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(aliases, tc.want) {
				t.Fatalf("aliases = %v, want %v", aliases, tc.want)
			}
			service := testTunService(t, tunPlatformSnapshot{
				PrivilegeBrokerAvailable: true, WFPReady: true, DefaultRouteAliases: aliases,
				NetworkRisks: []string{"active foreign virtual adapter: Radmin VPN"},
			})
			snapshot, err := service.Preflight([]string{"ethernet"})
			if err != nil {
				t.Fatal(err)
			}
			wantReady := len(tc.want) == 0 || tc.alias == "HypoMux-Tun"
			if snapshot.Ready != wantReady {
				t.Fatalf("ready = %t, issues = %+v", snapshot.Ready, snapshot.Issues)
			}
		})
	}
}
