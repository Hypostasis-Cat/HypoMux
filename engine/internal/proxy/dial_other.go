//go:build !windows

package proxy

import (
	"fmt"
	"net"
	"time"
)

func boundDialer(adapter Adapter, timeout time.Duration) (*net.Dialer, error) {
	source := net.ParseIP(adapter.SourceIP).To4()
	if source == nil {
		return nil, fmt.Errorf("invalid source IPv4 address %q", adapter.SourceIP)
	}
	return &net.Dialer{
		Timeout:   timeout,
		KeepAlive: 30 * time.Second,
		LocalAddr: &net.TCPAddr{IP: source},
	}, nil
}
