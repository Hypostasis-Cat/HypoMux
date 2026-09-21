package services

import (
	"fmt"
	"net/netip"
	"regexp"
	"sort"
	"strings"
)

// A read-only snapshot, independent of Windows APIs so real-world network
// layouts can become deterministic regression fixtures without a VPN driver.
type networkRoute struct {
	Prefix         netip.Prefix
	NextHop        netip.Addr
	InterfaceIndex uint32
	Alias          string
	Description    string
	InterfaceType  uint32
	TunnelType     uint32
	MetadataKnown  bool
	Hardware       bool
	Connected      bool
	Metric         uint64 // route metric + address-family-specific interface metric
}

var tunnelNamePattern = regexp.MustCompile(`(?i)(\b(tun|tap|vpn)\b|meta tunnel|wintun|wireguard|tailscale|mihomo|clash|famatech radmin)`)

func (r networkRoute) ownTUN() bool { return strings.EqualFold(r.Alias, "HypoMux-Tun") }

func (r networkRoute) tunnel() bool {
	// PPP can be a normal broadband uplink, and Hyper-V can be the host's only
	// uplink. Non-hardware alone is not proof of a competing VPN.
	if r.MetadataKnown && r.Hardware {
		return false
	}
	if r.MetadataKnown && r.InterfaceType == 131 {
		// IANA/Windows automatic IPv6 transition mechanisms: 6to4, ISATAP,
		// Teredo. Their default routes are not third-party VPN evidence,
		// including when they are the machine's only IPv6 route.
		switch r.TunnelType {
		case 11, 13, 14:
			return false
		}
	}
	// IF_TYPE_TUNNEL and the generic word "Tunnel" describe encapsulation,
	// not ownership or conflict. Unknown tunnels remain informational until
	// there is additional evidence identifying a competing TUN/VPN product.
	return tunnelNamePattern.MatchString(r.Alias + " " + r.Description)
}

// Inspect the eight /3 regions of each family. Fragmented defaults can use
// /2 or /3 after exclusions, not just /1. These routes beat /0 regardless of
// metric. Smaller destination-specific routes are not global takeover.
func assessNetworkRoutes(routes []networkRoute) (aliases, risks []string) {
	aliasSet, riskSet := map[string]bool{}, map[string]bool{}
	for _, r := range routes {
		if r.Connected && r.Prefix.IsValid() && r.Prefix.Bits() <= 3 && r.ownTUN() {
			aliasSet[r.Alias] = true // preserve stale-device cleanup even for backup routes
		}
	}
	for _, target := range []string{
		"0.0.0.0", "32.0.0.0", "64.0.0.0", "96.0.0.0", "128.0.0.0", "160.0.0.0", "192.0.0.0", "224.0.0.0",
		"::", "2000::", "4000::", "6000::", "8000::", "a000::", "c000::", "e000::",
	} {
		address := netip.MustParseAddr(target)
		bestBits, bestMetric := -1, ^uint64(0)
		var winners []networkRoute
		for _, r := range routes {
			if !r.Connected || !r.Prefix.IsValid() || r.Prefix.Bits() > 3 || !r.Prefix.Contains(address) {
				continue
			}
			bits := r.Prefix.Bits()
			if bits > bestBits || (bits == bestBits && r.Metric < bestMetric) {
				bestBits, bestMetric, winners = bits, r.Metric, nil
			}
			if bits == bestBits && r.Metric == bestMetric {
				winners = append(winners, r)
			}
		}
		for _, r := range winners {
			if r.ownTUN() {
				continue
			}
			if r.tunnel() {
				aliasSet[r.Alias] = true
			}
			if r.tunnel() || !r.MetadataKnown || !r.Hardware {
				riskSet[fmt.Sprintf("接口 %s 的优先路由 %s（接口索引 %d，总跃点 %d）", r.Alias, r.Prefix.Masked(), r.InterfaceIndex, r.Metric)] = true
			}
		}
	}
	for alias := range aliasSet {
		aliases = append(aliases, alias)
	}
	for risk := range riskSet {
		risks = append(risks, risk)
	}
	sort.Strings(aliases)
	sort.Strings(risks)
	return
}

func occupiedRoutePrefixes(routes []networkRoute) []netip.Prefix {
	var occupied []netip.Prefix
	for _, r := range routes {
		// Defaults and split defaults describe reachability, not allocated LAN
		// space. Specific VPN routes must not be stolen by our connected /30.
		if r.Connected && !r.ownTUN() && r.Prefix.IsValid() && r.Prefix.Bits() > 1 {
			occupied = append(occupied, r.Prefix.Masked())
		}
	}
	return occupied
}
