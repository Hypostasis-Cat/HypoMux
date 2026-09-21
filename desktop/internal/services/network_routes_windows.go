//go:build windows

package services

import (
	"errors"
	"fmt"
	"net/netip"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Read each family independently; a failure must not discard the other
// family's evidence. Cache metadata per interface, not per route.
func readNetworkRoutes() ([]networkRoute, error) {
	reader := newNetworkRouteReader(true)
	return collectNetworkRoutes(reader.readFamily)
}

func readAddressNetworkRoutes(ipv6 bool) ([]networkRoute, error) {
	reader := newNetworkRouteReader(false)
	return reader.readAddressFamily(ipv6)
}

type networkRouteReader struct {
	metadata             map[uint32]windows.MibIfRow2
	metadataErrors       map[uint32]error
	reportMetadataErrors bool
	getTable             func(uint16, **windows.MibIpForwardTable2) error
	freeTable            func(unsafe.Pointer)
	getInterface         func(*windows.MibIpInterfaceRow) error
	getAdapter           func(uint32, *windows.MibIfRow2) error
}

func newNetworkRouteReader(reportMetadataErrors bool) *networkRouteReader {
	return &networkRouteReader{
		metadata: map[uint32]windows.MibIfRow2{}, metadataErrors: map[uint32]error{},
		reportMetadataErrors: reportMetadataErrors,
		getTable:             windows.GetIpForwardTable2, freeTable: windows.FreeMibTable,
		getInterface: windows.GetIpInterfaceEntry, getAdapter: windows.GetIfEntry2Ex,
	}
}

func (r *networkRouteReader) readAddressFamily(ipv6 bool) ([]networkRoute, error) {
	family := uint16(windows.AF_INET)
	if ipv6 {
		family = windows.AF_INET6
	}
	return r.readFamily(family)
}

func collectNetworkRoutes(readFamily func(uint16) ([]networkRoute, error)) ([]networkRoute, error) {
	var routes []networkRoute
	var failures []error
	for _, family := range []uint16{windows.AF_INET, windows.AF_INET6} {
		familyRoutes, err := readFamily(family)
		routes = append(routes, familyRoutes...)
		if err != nil {
			failures = append(failures, err)
		}
	}
	return routes, errors.Join(failures...)
}

func (r *networkRouteReader) readFamily(family uint16) ([]networkRoute, error) {
	var table *windows.MibIpForwardTable2
	if err := r.getTable(family, &table); err != nil {
		// An absent/disabled IP stack is a valid empty family, not a failure
		// that should prevent a usable IPv4-only machine from starting TUN.
		if errors.Is(err, windows.ERROR_NOT_FOUND) || errors.Is(err, windows.ERROR_NOT_SUPPORTED) {
			return nil, nil
		}
		return nil, fmt.Errorf("读取地址族 %d 路由失败：%w", family, err)
	}
	if table == nil {
		return nil, fmt.Errorf("地址族 %d 返回了空路由表", family)
	}
	defer r.freeTable(unsafe.Pointer(table))
	interfaces := map[uint32]windows.MibIpInterfaceRow{}
	interfaceErrors := map[uint32]error{}
	var routes []networkRoute
	var failures []error
	for _, row := range table.Rows() {
		if row.InterfaceIndex == 0 || row.Loopback != 0 || row.ValidLifetime == 0 {
			continue
		}
		ip, ok := routePrefixAddress(row.DestinationPrefix.Prefix)
		prefix := netip.PrefixFrom(ip, int(row.DestinationPrefix.PrefixLength))
		if !ok || !prefix.IsValid() {
			failures = append(failures, fmt.Errorf("接口 %d 返回了无效路由前缀", row.InterfaceIndex))
			continue
		}
		if _, seen := interfaceErrors[row.InterfaceIndex]; !seen {
			entry := windows.MibIpInterfaceRow{Family: family, InterfaceLuid: row.InterfaceLuid, InterfaceIndex: row.InterfaceIndex}
			err := r.getInterface(&entry)
			interfaces[row.InterfaceIndex], interfaceErrors[row.InterfaceIndex] = entry, err
			if err != nil {
				failures = append(failures, fmt.Errorf("读取接口 %d 地址族 %d 失败：%w", row.InterfaceIndex, family, err))
			}
		}
		entry := interfaces[row.InterfaceIndex]
		if interfaceErrors[row.InterfaceIndex] != nil || entry.Connected == 0 {
			continue
		}
		if entry.DisableDefaultRoutes != 0 && prefix.Bits() == 0 {
			continue
		}
		if _, seen := r.metadataErrors[row.InterfaceIndex]; !seen {
			adapter := windows.MibIfRow2{InterfaceLuid: row.InterfaceLuid, InterfaceIndex: row.InterfaceIndex}
			err := r.getAdapter(windows.MibIfEntryNormalWithoutStatistics, &adapter)
			r.metadata[row.InterfaceIndex], r.metadataErrors[row.InterfaceIndex] = adapter, err
			if err != nil && r.reportMetadataErrors {
				failures = append(failures, fmt.Errorf("读取接口 %d 类型失败：%w", row.InterfaceIndex, err))
			}
		}
		adapter := r.metadata[row.InterfaceIndex]
		nextHop, _ := routePrefixAddress(row.NextHop)
		alias := windows.UTF16ToString(adapter.Alias[:])
		if alias == "" {
			alias = fmt.Sprintf("ifIndex=%d", row.InterfaceIndex)
		}
		routes = append(routes, networkRoute{
			Prefix: prefix.Masked(), NextHop: nextHop, InterfaceIndex: row.InterfaceIndex, Alias: alias,
			Description: windows.UTF16ToString(adapter.Description[:]), InterfaceType: adapter.Type,
			TunnelType:    adapter.TunnelType,
			MetadataKnown: r.metadataErrors[row.InterfaceIndex] == nil,
			Hardware:      adapter.InterfaceAndOperStatusFlags&1 != 0, Connected: true,
			Metric: uint64(row.Metric) + uint64(entry.Metric),
		})
	}
	return routes, errors.Join(failures...)
}

func routePrefixAddress(raw windows.RawSockaddrInet) (netip.Addr, bool) {
	switch raw.Family {
	case windows.AF_INET:
		v4 := (*windows.RawSockaddrInet4)(unsafe.Pointer(&raw))
		return netip.AddrFrom4(v4.Addr), true
	case windows.AF_INET6:
		v6 := (*windows.RawSockaddrInet6)(unsafe.Pointer(&raw))
		return netip.AddrFrom16(v6.Addr), true
	default:
		return netip.Addr{}, false
	}
}
