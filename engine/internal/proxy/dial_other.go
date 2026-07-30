//go:build !windows

package proxy

import (
	"fmt"
	"net"
	"time"
)

func boundDialer(adapter Adapter, timeout time.Duration) (*net.Dialer, error) {
	return boundNetworkDialer(adapter, timeout, "tcp4")
}

func boundNetworkDialer(adapter Adapter, timeout time.Duration, network string) (*net.Dialer, error) {
	source := net.ParseIP(adapter.SourceIP).To4()
	if source == nil {
		return nil, fmt.Errorf("invalid source IPv4 address %q", adapter.SourceIP)
	}
	dialer := &net.Dialer{
		Timeout:   timeout,
		KeepAlive: 30 * time.Second,
	}
	if network == "udp4" {
		dialer.LocalAddr = &net.UDPAddr{IP: source}
	} else {
		dialer.LocalAddr = &net.TCPAddr{IP: source}
	}
	return dialer, nil
}
