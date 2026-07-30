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
	ipv6 := network == "tcp6" || network == "udp6"
	sourceText := adapter.SourceIP
	if ipv6 {
		sourceText = adapter.SourceIPv6
	}
	source := net.ParseIP(sourceText)
	if source == nil || (ipv6 && source.To4() != nil) || (!ipv6 && source.To4() == nil) {
		return nil, fmt.Errorf("invalid source address %q for %s", sourceText, network)
	}
	if !ipv6 {
		source = source.To4()
	}
	dialer := &net.Dialer{
		Timeout:   timeout,
		KeepAlive: 30 * time.Second,
	}
	if network == "udp4" || network == "udp6" {
		dialer.LocalAddr = &net.UDPAddr{IP: source}
	} else {
		dialer.LocalAddr = &net.TCPAddr{IP: source}
	}
	return dialer, nil
}
