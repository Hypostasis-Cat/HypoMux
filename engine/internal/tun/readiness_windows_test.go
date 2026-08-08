//go:build windows

package tun

import (
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestRoutesCoverIPv4AcceptsDefaultOrBothSplitRoutes(t *testing.T) {
	const interfaceIndex = 42
	defaultRoute := testIPv4Route(interfaceIndex, 0, 0)
	if !routesCoverIPv4([]windows.MibIpForwardRow2{defaultRoute}, interfaceIndex) {
		t.Fatal("an exact IPv4 default route should cover the TUN data path")
	}
	low := testIPv4Route(interfaceIndex, 1, 0)
	high := testIPv4Route(interfaceIndex, 1, 128)
	if !routesCoverIPv4([]windows.MibIpForwardRow2{low, high}, interfaceIndex) {
		t.Fatal("both IPv4 /1 split routes should cover the TUN data path")
	}
	if routesCoverIPv4([]windows.MibIpForwardRow2{low}, interfaceIndex) {
		t.Fatal("one IPv4 /1 route must not be treated as complete takeover")
	}
	wrongInterface := testIPv4Route(interfaceIndex+1, 0, 0)
	if routesCoverIPv4([]windows.MibIpForwardRow2{wrongInterface}, interfaceIndex) {
		t.Fatal("another interface's default route must not satisfy TUN readiness")
	}
}

func testIPv4Route(interfaceIndex uint32, prefixLength uint8, firstByte byte) windows.MibIpForwardRow2 {
	row := windows.MibIpForwardRow2{InterfaceIndex: interfaceIndex}
	row.DestinationPrefix.PrefixLength = prefixLength
	address := (*windows.RawSockaddrInet4)(unsafe.Pointer(&row.DestinationPrefix.Prefix))
	address.Family = windows.AF_INET
	address.Addr[0] = firstByte
	return row
}
