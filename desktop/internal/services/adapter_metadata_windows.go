//go:build windows

package services

import (
	"errors"
	"net"
	"strconv"
	"unsafe"

	"golang.org/x/sys/windows"
)

var getIPInterfaceEntry = windows.NewLazySystemDLL("iphlpapi.dll").NewProc("GetIpInterfaceEntry")

// adapterPlatformMetadata reads the IP Helper tables in-process. The previous
// implementation launched PowerShell/Get-NetIPConfiguration for every list,
// which made the first page load and the five-second adapter refresh visibly
// block for seconds.
func adapterPlatformMetadata() map[int]adapterMetadata {
	const flags = windows.GAA_FLAG_INCLUDE_GATEWAYS |
		windows.GAA_FLAG_SKIP_ANYCAST |
		windows.GAA_FLAG_SKIP_MULTICAST

	size := uint32(15 * 1024)
	var buffer []byte
	var first *windows.IpAdapterAddresses
	for attempt := 0; attempt < 3; attempt++ {
		buffer = make([]byte, size)
		first = (*windows.IpAdapterAddresses)(unsafe.Pointer(&buffer[0]))
		err := windows.GetAdaptersAddresses(
			windows.AF_UNSPEC,
			flags,
			0,
			first,
			&size,
		)
		if err == nil {
			break
		}
		if !errors.Is(err, windows.ERROR_BUFFER_OVERFLOW) {
			return map[int]adapterMetadata{}
		}
		first = nil
	}
	if first == nil {
		return map[int]adapterMetadata{}
	}

	result := make(map[int]adapterMetadata)
	for current := first; current != nil; current = current.Next {
		if current.IfIndex == 0 && current.Ipv6IfIndex == 0 {
			continue
		}
		details := adapterMetadata{
			IsTunnel:       current.IfType == windows.IF_TYPE_TUNNEL,
			Description:    windows.UTF16PtrToString(current.Description),
			Metric:         int(current.Ipv4Metric),
			AutoMetric:     true,
			IPv6IfIndex:    int(current.Ipv6IfIndex),
			IPv6Metric:     int(current.Ipv6Metric),
			IPv6AutoMetric: true,
		}
		for gateway := current.FirstGatewayAddress; gateway != nil; gateway = gateway.Next {
			if ip := gateway.Address.IP(); ip != nil {
				if ipv4 := ip.To4(); ipv4 != nil && !ipv4.IsUnspecified() {
					details.Gateway = ipv4.String()
					break
				}
			}
		}
		for gateway := current.FirstGatewayAddress; gateway != nil; gateway = gateway.Next {
			if ip := gateway.Address.IP(); ip != nil && ip.To4() == nil && !ip.IsUnspecified() {
				details.IPv6Gateway = ip.String()
				if ip.IsLinkLocalUnicast() {
					details.IPv6Gateway += "%" + strconv.Itoa(details.IPv6IfIndex)
				}
				break
			}
		}
		// The IPv6 source is chosen after the gateway so that a ULA-only link
		// without an IPv6 default route is not mistaken for IPv6 egress.
		details.PreferredIPv6 = preferredIPv6Source(current.FirstUnicastAddress, details.IPv6Gateway != "")
		seenDNS := make(map[string]struct{})
		for server := current.FirstDnsServerAddress; server != nil; server = server.Next {
			ip := server.Address.IP()
			if ip == nil {
				continue
			}
			if ip.IsUnspecified() || ip.IsMulticast() {
				continue
			}
			value := ip.String()
			if ip.IsLinkLocalUnicast() && ip.To4() == nil {
				value += "%" + strconv.Itoa(details.IPv6IfIndex)
			}
			if _, exists := seenDNS[value]; exists {
				continue
			}
			seenDNS[value] = struct{}{}
			details.DNSServers = append(details.DNSServers, value)
		}
		row := windows.MibIpInterfaceRow{
			Family:         windows.AF_INET,
			InterfaceLuid:  current.Luid,
			InterfaceIndex: current.IfIndex,
		}
		if status, _, _ := getIPInterfaceEntry.Call(uintptr(unsafe.Pointer(&row))); status == 0 {
			details.Metric = int(row.Metric)
			details.AutoMetric = row.UseAutomaticMetric != 0
		}
		result[int(current.IfIndex)] = details
		if current.Ipv6IfIndex != 0 {
			row6 := windows.MibIpInterfaceRow{Family: windows.AF_INET6, InterfaceLuid: current.Luid, InterfaceIndex: current.Ipv6IfIndex}
			if err := windows.GetIpInterfaceEntry(&row6); err == nil {
				details.IPv6Metric = int(row6.Metric)
				details.IPv6AutoMetric = row6.UseAutomaticMetric != 0
			}
			result[int(current.Ipv6IfIndex)] = details
			if current.IfIndex != 0 {
				result[int(current.IfIndex)] = details
			}
		}
	}
	return result
}

func preferredIPv6Source(first *windows.IpAdapterUnicastAddress, hasIPv6Gateway bool) string {
	var preferred net.IP
	preferredPriority := 4
	for address := first; address != nil; address = address.Next {
		ip := address.Address.IP()
		if ip == nil || ip.To4() != nil || ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || address.DadState != windows.IpDadStatePreferred || address.ValidLifetime == 0 || address.PreferredLifetime == 0 {
			continue
		}
		// A unique local address (fc00::/7) only reaches the Internet through a
		// router that advertises an IPv6 default route (NAT66/NPTv6). Routers
		// that hand out a ULA prefix without IPv6 uplink leave Windows with no
		// ::/0 on this interface, so every bound dial fails with WSAENETUNREACH
		// and the adapter is wrongly reported as IPv6-capable.
		if ip.IsPrivate() && !hasIPv6Gateway {
			continue
		}
		// Binding the Core to a rotating privacy address needlessly retires
		// live connections when a stable preferred address is still available.
		// Public reachability takes precedence; temporary addresses remain a
		// valid fallback when no stable address exists in the same scope.
		priority := 0
		if ip.IsPrivate() {
			priority += 2
		}
		if address.SuffixOrigin == windows.IpSuffixOriginRandom {
			priority++
		}
		if preferred == nil || priority < preferredPriority || (priority == preferredPriority && ip.String() < preferred.String()) {
			preferred = ip
			preferredPriority = priority
		}
	}
	if preferred == nil {
		return ""
	}
	return preferred.String()
}
