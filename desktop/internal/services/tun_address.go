package services

import (
	"encoding/binary"
	"fmt"
	"net"
	"net/netip"
	"sort"
	"strings"
)

// Inspect all interfaces, including hidden/disabled virtual interfaces and
// adapters not selected for aggregation. Never change their configuration.
func availableTunIPv4Address() (string, error) {
	occupied, err := occupiedTunNetworks(false)
	if err != nil {
		return "", err
	}
	return selectTunIPv4Address(occupied)
}

func availableTunIPv6Address() (string, error) {
	occupied, err := occupiedTunNetworks(true)
	if err != nil {
		return "", err
	}
	return selectTunIPv6Address(occupied)
}

func occupiedTunNetworks(ipv6 bool) ([]netip.Prefix, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("检查 TUN 地址冲突失败：%w", err)
	}
	var occupied []netip.Prefix
	var ownIndices = map[uint32]bool{}
	for _, device := range interfaces {
		if strings.EqualFold(device.Name, "HypoMux-Tun") {
			ownIndices[uint32(device.Index)] = true
			continue // The core removes its own stale device before activation.
		}
		addresses, err := device.Addrs()
		if err != nil {
			return nil, fmt.Errorf("检查网卡 %s 的地址失败：%w", device.Name, err)
		}
		for _, address := range addresses {
			prefix, err := netip.ParsePrefix(address.String())
			if err != nil {
				return nil, fmt.Errorf("读取网卡 %s 的地址 %q 失败：%w", device.Name, address, err)
			}
			if prefix.Addr().Is6() == ipv6 {
				occupied = append(occupied, prefix.Masked())
			}
		}
	}
	routes, err := readAddressNetworkRoutes(ipv6)
	if err != nil {
		return nil, fmt.Errorf("检查 TUN 路由地址冲突失败：%w", err)
	}
	// The optional descriptive metadata may be unreadable. Interface indices
	// from net.Interfaces still identify our stale device without guessing.
	for i := range routes {
		if ownIndices[routes[i].InterfaceIndex] {
			routes[i].Alias = "HypoMux-Tun"
		}
	}
	return append(occupied, occupiedRoutePrefixes(routes)...), nil
}

func selectTunIPv6Address(occupied []netip.Prefix) (string, error) {
	// Preserve the historical ULA when free. Stay outside the fc00::/18
	// FakeIP range; never consume another interface's or VPN's ULA subnet.
	candidates := []string{"fdfe:dcba:9876::1/126"}
	for subnet := 1; subnet < 256; subnet++ {
		candidates = append(candidates, fmt.Sprintf("fdfe:dcba:%x::1/126", 0x9876+subnet))
	}
	for subnet := 0; subnet < 256; subnet++ {
		candidates = append(candidates, fmt.Sprintf("fd%02x:dcba:9876::1/126", subnet))
	}
	for _, candidate := range candidates {
		prefix := netip.MustParsePrefix(candidate)
		conflict := false
		for _, existing := range occupied {
			if prefix.Overlaps(existing) {
				conflict = true
				break
			}
		}
		if !conflict {
			return candidate, nil
		}
	}
	if free, ok := findUnoccupiedPrefix(netip.MustParsePrefix("fd00::/8"), 126, occupied); ok {
		return free.Addr().Next().String() + "/126", nil
	}
	return "", fmt.Errorf("找不到不与现有网卡及路由重叠的 TUN IPv6 地址；请检查 VPN 与局域网地址配置")
}

func selectTunIPv4Address(occupied []netip.Prefix) (string, error) {
	// Keep the historical address when available. Use RFC1918 space only;
	// 198.18.0.0/15 is reserved by our FakeIP pool, and CGNAT may be an uplink.
	candidates := []string{"172.19.0.1/30"}
	for subnet := 16; subnet <= 31; subnet++ {
		candidates = append(candidates, fmt.Sprintf("172.%d.255.1/30", subnet))
	}
	for subnet := 255; subnet >= 0; subnet-- {
		candidates = append(candidates, fmt.Sprintf("10.%d.255.1/30", subnet))
	}
	for _, candidate := range candidates {
		prefix := netip.MustParsePrefix(candidate)
		conflict := false
		for _, existing := range occupied {
			if prefix.Overlaps(existing) {
				conflict = true
				break
			}
		}
		if !conflict {
			return candidate, nil
		}
	}
	// Fixed candidates can all be occupied while a hole remains elsewhere
	// (for example a VPN excluding one LAN from hundreds of split routes).
	// Walk occupied intervals rather than probing millions of /30 candidates.
	type interval struct{ first, last uint64 }
	var used []interval
	for _, existing := range occupied {
		if !existing.IsValid() || !existing.Addr().Is4() {
			continue
		}
		bytes := existing.Masked().Addr().As4()
		first := uint64(binary.BigEndian.Uint32(bytes[:]))
		used = append(used, interval{first, first + (uint64(1) << (32 - existing.Bits())) - 1})
	}
	sort.Slice(used, func(i, j int) bool { return used[i].first < used[j].first })
	for _, pool := range []string{"172.16.0.0/12", "10.0.0.0/8", "192.168.0.0/16"} {
		prefix := netip.MustParsePrefix(pool)
		bytes := prefix.Addr().As4()
		cursor := uint64(binary.BigEndian.Uint32(bytes[:]))
		limit := cursor + (uint64(1) << (32 - prefix.Bits()))
		for _, reserved := range used {
			if reserved.last < cursor {
				continue
			}
			if reserved.first > cursor+3 || cursor+4 > limit {
				break
			}
			cursor = (reserved.last + 4) &^ uint64(3)
		}
		if cursor+4 <= limit {
			binary.BigEndian.PutUint32(bytes[:], uint32(cursor+1))
			return netip.AddrFrom4(bytes).String() + "/30", nil
		}
	}
	return "", fmt.Errorf("找不到不与现有网卡及路由重叠的 TUN IPv4 地址；请检查 VPN、虚拟网卡和局域网地址配置")
}
