//go:build windows

package tun

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

func tunPlatformReady() bool {
	device, addressReady := tunInterfaceWithExpectedAddress()
	if !addressReady {
		return false
	}
	var table *windows.MibIpForwardTable2
	if err := windows.GetIpForwardTable2(windows.AF_INET, &table); err != nil || table == nil {
		return false
	}
	defer windows.FreeMibTable(unsafe.Pointer(table))
	return routesCoverIPv4(table.Rows(), uint32(device.Index))
}

func routesCoverIPv4(routes []windows.MibIpForwardRow2, interfaceIndex uint32) bool {
	lowHalf := false
	highHalf := false
	for _, route := range routes {
		if route.InterfaceIndex != interfaceIndex {
			continue
		}
		if route.DestinationPrefix.PrefixLength == 0 {
			return true
		}
		if route.DestinationPrefix.PrefixLength != 1 ||
			route.DestinationPrefix.Prefix.Family != windows.AF_INET {
			continue
		}
		address := (*windows.RawSockaddrInet4)(
			unsafe.Pointer(&route.DestinationPrefix.Prefix),
		).Addr
		if address[0]&0x80 == 0 {
			lowHalf = true
		} else {
			highHalf = true
		}
	}
	return lowHalf && highHalf
}
