//go:build !windows

package services

import "context"

func probeTUNDataPathPlatform(context.Context, []string) tunConnectivityCheck {
	return tunConnectivityCheck{
		Stage:    "tun_data_path",
		Endpoint: tunDataPathURL,
		Outbound: "system-tun",
		Error:    "independent TUN data-path probing is only available on Windows",
	}
}
