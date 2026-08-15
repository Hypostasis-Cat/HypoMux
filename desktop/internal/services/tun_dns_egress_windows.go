//go:build windows

package services

import (
	"errors"
	"net"
	"unsafe"

	"golang.org/x/sys/windows"
)

func systemDefaultDNSAdapterID() (string, error) {
	var table *windows.MibIpForwardTable2
	if err := windows.GetIpForwardTable2(windows.AF_INET, &table); err != nil {
		return "", err
	}
	if table == nil {
		return "", errors.New("Windows 返回了空路由表")
	}
	defer windows.FreeMibTable(unsafe.Pointer(table))

	var bestIndex uint32
	var bestMetric uint64
	found := false
	for _, route := range table.Rows() {
		if route.DestinationPrefix.PrefixLength != 0 || route.InterfaceIndex == 0 || route.Loopback != 0 {
			continue
		}
		interfaceRow := windows.MibIpInterfaceRow{
			Family:         windows.AF_INET,
			InterfaceLuid:  route.InterfaceLuid,
			InterfaceIndex: route.InterfaceIndex,
		}
		if err := windows.GetIpInterfaceEntry(&interfaceRow); err != nil ||
			interfaceRow.Connected == 0 || interfaceRow.DisableDefaultRoutes != 0 {
			continue
		}
		metric := uint64(route.Metric) + uint64(interfaceRow.Metric)
		if !found || metric < bestMetric || (metric == bestMetric && route.InterfaceIndex < bestIndex) {
			bestIndex = route.InterfaceIndex
			bestMetric = metric
			found = true
		}
	}
	if !found {
		return "", errors.New("没有活动的 IPv4 默认路由")
	}
	adapter, err := net.InterfaceByIndex(int(bestIndex))
	if err != nil {
		return "", err
	}
	if adapter.Name == "" {
		return "", errors.New("默认路由没有接口名称")
	}
	return adapter.Name, nil
}
