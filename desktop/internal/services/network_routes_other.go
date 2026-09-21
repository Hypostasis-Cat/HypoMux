//go:build !windows

package services

func readNetworkRoutes() ([]networkRoute, error) { return nil, nil }
