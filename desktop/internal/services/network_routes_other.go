//go:build !windows

package services

func readNetworkRoutes() ([]networkRoute, error) { return nil, nil }

func readAddressNetworkRoutes(ipv6 bool) ([]networkRoute, error) { return nil, nil }
