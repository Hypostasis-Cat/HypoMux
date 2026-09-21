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

// Only broad routes are candidates for a global takeover, but every more
// specific route participates in deciding whether a candidate can win.
func assessNetworkRoutes(routes []networkRoute) (aliases, risks []string) {
	aliasSet, riskSet := map[string]bool{}, map[string]bool{}
	for _, r := range routes {
		if r.Connected && r.Prefix.IsValid() && r.Prefix.Bits() <= 3 && r.ownTUN() {
			aliasSet[r.Alias] = true // preserve stale-device cleanup even for backup routes
		}
	}
	for _, r := range routes {
		if !r.Connected || !r.Prefix.IsValid() || r.Prefix.Bits() > 3 || r.ownTUN() {
			continue
		}
		var overrides []netip.Prefix
		for _, other := range routes {
			if !other.Connected || !other.Prefix.Overlaps(r.Prefix) {
				continue
			}
			if other.Prefix.Bits() > r.Prefix.Bits() ||
				(other.Prefix.Bits() == r.Prefix.Bits() && other.Metric < r.Metric) {
				overrides = append(overrides, other.Prefix)
			}
		}
		if _, wins := findUnoccupiedPrefix(r.Prefix.Masked(), r.Prefix.Addr().BitLen(), overrides); !wins {
			continue
		}
		if r.tunnel() {
			aliasSet[r.Alias] = true
		}
		if r.tunnel() || !r.MetadataKnown || !r.Hardware {
			riskSet[fmt.Sprintf("接口 %s 的优先路由 %s（接口索引 %d，总跃点 %d）", r.Alias, r.Prefix.Masked(), r.InterfaceIndex, r.Metric)] = true
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

// Search a prefix tree, pruning covered subtrees. This is bounded by the
// supplied prefixes and address width, not by the number of host addresses.
func findUnoccupiedPrefix(pool netip.Prefix, bits int, occupied []netip.Prefix) (netip.Prefix, bool) {
	if !pool.IsValid() || bits < pool.Bits() || bits > pool.Addr().BitLen() {
		return netip.Prefix{}, false
	}
	pool = pool.Masked()
	var overlapping []netip.Prefix
	for _, existing := range occupied {
		if !pool.Overlaps(existing) {
			continue
		}
		if existing.Bits() <= pool.Bits() {
			return netip.Prefix{}, false
		}
		overlapping = append(overlapping, existing)
	}
	if len(overlapping) == 0 {
		return netip.PrefixFrom(pool.Addr(), bits), true
	}
	if pool.Bits() == bits {
		return netip.Prefix{}, false
	}
	childBits := pool.Bits() + 1
	if free, ok := findUnoccupiedPrefix(netip.PrefixFrom(pool.Addr(), childBits), bits, overlapping); ok {
		return free, true
	}
	address := pool.Addr().AsSlice()
	address[pool.Bits()/8] |= 1 << (7 - pool.Bits()%8)
	right, _ := netip.AddrFromSlice(address)
	return findUnoccupiedPrefix(netip.PrefixFrom(right, childBits), bits, overlapping)
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
