//go:build windows

package services

import (
	"errors"
	"net"
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
		if current.IfIndex == 0 {
			continue
		}
		details := adapterMetadata{
			Metric:     int(current.Ipv4Metric),
			AutoMetric: true,
		}
		for gateway := current.FirstGatewayAddress; gateway != nil; gateway = gateway.Next {
			if ip := gateway.Address.IP(); ip != nil {
				if ipv4 := ip.To4(); ipv4 != nil && !ipv4.IsUnspecified() {
					details.Gateway = ipv4.String()
					break
				}
			}
		}
		seenDNS := make(map[string]struct{})
		for server := current.FirstDnsServerAddress; server != nil; server = server.Next {
			ip := server.Address.IP()
			if ip == nil {
				continue
			}
			ipv4 := ip.To4()
			if ipv4 == nil || ipv4.IsUnspecified() {
				continue
			}
			value := net.IP(ipv4).String()
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
	}
	return result
}
