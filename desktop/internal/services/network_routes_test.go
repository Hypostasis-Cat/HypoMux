package services

import (
	"net/netip"
	"reflect"
	"slices"
	"testing"
)

func routeFixture(prefix, alias string, metric uint64) networkRoute {
	return networkRoute{Prefix: netip.MustParsePrefix(prefix), Alias: alias, Metric: metric,
		InterfaceIndex: 45, Connected: true, MetadataKnown: true}
}

func TestNetworkRouteTakeoverScenarios(t *testing.T) {
	physical := routeFixture("0.0.0.0/0", "Ethernet", 25)
	physical.Hardware, physical.InterfaceIndex = true, 10
	v6physical := physical
	v6physical.Prefix = netip.MustParsePrefix("::/0")
	renamed := routeFixture("0.0.0.0/0", "工作网络", 1)
	renamed.Description = "WireGuard Tunnel"
	unknownTunnel := renamed
	unknownTunnel.Description, unknownTunnel.InterfaceType = "Vendor Network", 131
	disconnected := routeFixture("0.0.0.0/0", "Radmin VPN", 1)
	disconnected.Connected = false
	physicalVPNName := physical
	physicalVPNName.Alias = "VPN Internet"
	for _, tc := range []struct {
		name     string
		routes   []networkRoute
		want     []string
		wantRisk bool
	}{
		{"installed Radmin backup", []networkRoute{physical, routeFixture("0.0.0.0/0", "Radmin VPN", 9257)}, nil, false},
		{"Radmin preferred", []networkRoute{physical, routeFixture("0.0.0.0/0", "Radmin VPN", 1)}, []string{"Radmin VPN"}, true},
		{"equal cost", []networkRoute{physical, routeFixture("0.0.0.0/0", "Radmin VPN", 25)}, []string{"Radmin VPN"}, true},
		{"interface metric included", []networkRoute{physical, routeFixture("0.0.0.0/0", "Radmin VPN", 100)}, nil, false},
		{"disconnected VPN", []networkRoute{physical, disconnected}, nil, false},
		{"active proxy", []networkRoute{physical, routeFixture("0.0.0.0/0", "Mihomo", 1)}, []string{"Mihomo"}, true},
		{"own stale backup", []networkRoute{physical, routeFixture("0.0.0.0/0", "HypoMux-Tun", 9257)}, []string{"HypoMux-Tun"}, false},
		{"only VPN route", []networkRoute{routeFixture("0.0.0.0/0", "Radmin VPN", 9257)}, []string{"Radmin VPN"}, true},
		{"no connected routes", []networkRoute{disconnected}, nil, false},
		{"renamed VPN description", []networkRoute{physical, renamed}, []string{"工作网络"}, true},
		{"unknown tunnel type is not VPN evidence", []networkRoute{physical, unknownTunnel}, nil, true},
		{"physical name is not evidence", []networkRoute{physicalVPNName}, nil, false},
		{"Hyper-V host uplink is allowed", []networkRoute{routeFixture("0.0.0.0/0", "vEthernet (External)", 1)}, nil, true},
		{"unknown virtual uplink is allowed", []networkRoute{routeFixture("0.0.0.0/0", "Vendor Network", 1)}, nil, true},
		{"split defaults beat metric", []networkRoute{physical, routeFixture("0.0.0.0/1", "VPN", 9000), routeFixture("128.0.0.0/1", "VPN", 9000)}, []string{"VPN"}, true},
		{"one split half", []networkRoute{physical, routeFixture("128.0.0.0/1", "VPN", 9000)}, []string{"VPN"}, true},
		{"fragmented default after LAN exclusions", []networkRoute{physical, routeFixture("128.0.0.0/2", "Meta Tunnel", 9000), routeFixture("32.0.0.0/3", "Meta Tunnel", 9000)}, []string{"Meta Tunnel"}, true},
		{"IPv6 default", []networkRoute{physical, v6physical, routeFixture("::/0", "VPN", 1)}, []string{"VPN"}, true},
		{"IPv6 backup", []networkRoute{v6physical, routeFixture("::/0", "VPN", 9000)}, nil, false},
		{"IPv6 split default", []networkRoute{v6physical, routeFixture("::/1", "VPN", 9000)}, []string{"VPN"}, true},
		{"VPN private route is not global takeover", []networkRoute{physical, routeFixture("10.0.0.0/8", "VPN", 1)}, nil, false},
		{"link-local device network", []networkRoute{physical, routeFixture("169.254.0.0/16", "Device Network", 1)}, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			aliases, risks := assessNetworkRoutes(tc.routes)
			if !reflect.DeepEqual(aliases, tc.want) || (len(risks) > 0) != tc.wantRisk {
				t.Fatalf("aliases=%v risks=%v, want=%v risk=%v", aliases, risks, tc.want, tc.wantRisk)
			}
			// Enumeration order is not a priority signal.
			reversed := slices.Clone(tc.routes)
			slices.Reverse(reversed)
			otherAliases, otherRisks := assessNetworkRoutes(reversed)
			if !reflect.DeepEqual(aliases, otherAliases) || !reflect.DeepEqual(risks, otherRisks) {
				t.Fatal("route enumeration order changed the decision")
			}
			service := testTunService(t, tunPlatformSnapshot{
				PrivilegeBrokerAvailable: true, WFPReady: true, DefaultRouteAliases: aliases, NetworkRisks: risks,
			})
			snapshot, err := service.Preflight([]string{"ethernet"})
			wantReady := len(tc.want) == 0 || reflect.DeepEqual(tc.want, []string{"HypoMux-Tun"})
			if err != nil || snapshot.Ready != wantReady {
				t.Fatalf("ready=%t err=%v issues=%+v", snapshot.Ready, err, snapshot.Issues)
			}
		})
	}
}

func TestSpecificVPNRoutesReserveTunAddressSpace(t *testing.T) {
	disconnected := routeFixture("10.0.0.0/8", "Offline VPN", 1)
	disconnected.Connected = false
	routes := []networkRoute{
		routeFixture("0.0.0.0/0", "Ethernet", 1),
		routeFixture("0.0.0.0/1", "VPN", 1),
		routeFixture("128.0.0.0/1", "VPN", 1),
		routeFixture("172.16.0.0/12", "VPN", 1),
		routeFixture("10.255.255.0/30", "HypoMux-Tun", 1),
		disconnected,
	}
	address, err := selectTunIPv4Address(occupiedRoutePrefixes(routes))
	if err != nil || address != "10.255.255.1/30" {
		t.Fatalf("specific VPN subnet must be preserved: %s %v", address, err)
	}
}

func TestIPv6TransitionRoutesDoNotBlockTUN(t *testing.T) {
	for _, tc := range []struct {
		name    string
		subtype uint32
	}{
		{"Teredo Tunneling Pseudo-Interface", 14},
		{"Microsoft 6to4 Adapter", 11},
		{"Microsoft ISATAP Adapter", 13},
		{"renamed VPN Tunnel", 14}, // native subtype takes precedence over alias
		{"Generic Tunnel", 0},      // encapsulation alone is not VPN evidence
	} {
		t.Run(tc.name, func(t *testing.T) {
			physical := routeFixture("0.0.0.0/0", "Ethernet", 25)
			physical.Hardware = true
			transition := routeFixture("::/0", tc.name, 331)
			transition.InterfaceIndex, transition.InterfaceType, transition.TunnelType = 15, 131, tc.subtype
			// Exact reported shape: usable IPv4 uplink, only IPv6 default via
			// Teredo. Route metrics must not be compared across families.
			aliases, risks := assessNetworkRoutes([]networkRoute{physical, transition})
			if len(aliases) != 0 || len(risks) != 1 {
				t.Fatalf("transition route should be information only: %v %v", aliases, risks)
			}
			service := testTunService(t, tunPlatformSnapshot{
				PrivilegeBrokerAvailable: true, WFPReady: true, DefaultRouteAliases: aliases, NetworkRisks: risks,
			})
			snapshot, err := service.Preflight([]string{"ethernet"})
			if err != nil || !snapshot.Ready || snapshot.ForeignTUN != "" || hasTunIssue(snapshot, "foreign_tun") {
				t.Fatalf("system transition tunnel blocked startup: %+v %v", snapshot, err)
			}
			// Exempting the transition interface must not mask a real VPN.
			vpn := routeFixture("::/1", "WireGuard Tunnel", 9000)
			aliases, _ = assessNetworkRoutes([]networkRoute{physical, transition, vpn})
			if !reflect.DeepEqual(aliases, []string{"WireGuard Tunnel"}) {
				t.Fatalf("real VPN was hidden by transition exemption: %v", aliases)
			}
		})
	}
}
