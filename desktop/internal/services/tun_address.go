package services

import (
	"fmt"
	"net"
	"net/netip"
)

// Inspect all interfaces, including hidden/disabled virtual interfaces and
// adapters not selected for aggregation. Never change their configuration.
func availableTunIPv4Address() (string, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return "", fmt.Errorf("检查 TUN 地址冲突失败：%w", err)
	}
	var occupied []netip.Prefix
	for _, device := range interfaces {
		if device.Name == "HypoMux-Tun" {
			continue // The core removes its own stale device before activation.
		}
		addresses, err := device.Addrs()
		if err != nil {
			return "", fmt.Errorf("检查网卡 %s 的地址失败：%w", device.Name, err)
		}
		for _, address := range addresses {
			prefix, err := netip.ParsePrefix(address.String())
			if err != nil {
				return "", fmt.Errorf("读取网卡 %s 的地址 %q 失败：%w", device.Name, address, err)
			}
			if prefix.Addr().Is4() {
				occupied = append(occupied, prefix.Masked())
			}
		}
	}
	return selectTunIPv4Address(occupied)
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
	return "", fmt.Errorf("找不到不与现有网卡网段重叠的 TUN IPv4 地址；请检查 VPN、虚拟网卡和局域网地址配置")
}
