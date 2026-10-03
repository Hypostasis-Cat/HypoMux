//go:build windows

package services

import "errors"

func systemDefaultDNSAdapterID() (string, error) {
	var failures []error
	for _, ipv6 := range []bool{false, true} {
		routes, readErr := readAddressNetworkRoutes(ipv6)
		if name, ok := defaultDNSRouteAdapter(routes); ok {
			return name, nil
		}
		failures = append(failures, errors.Join(readErr, errors.New("没有对应地址族的活动默认路由")))
	}
	return "", errors.Join(failures...)
}
