//go:build darwin

package proxy

import (
	"fmt"
	"net"
	"syscall"
	"time"
)

// IP_BOUND_IF and IPV6_BOUND_IF are the Darwin socket options used by Apple
// networking APIs to keep a socket on a specific interface even when another
// service owns the system default route.
const (
	darwinIPBoundIf   = 25
	darwinIPv6BoundIf = 125
)

func boundDialer(adapter Adapter, timeout time.Duration) (*net.Dialer, error) {
	return boundNetworkDialer(adapter, timeout, "tcp4")
}

func boundNetworkDialer(adapter Adapter, timeout time.Duration, network string) (*net.Dialer, error) {
	ipv6 := network == "tcp6" || network == "udp6"
	sourceText := adapter.SourceIP
	ifIndex := adapter.IfIndex
	if ipv6 {
		sourceText = adapter.SourceIPv6
		ifIndex = adapter.IPv6IfIndex
	}
	source := net.ParseIP(sourceText)
	if source == nil || (ipv6 && source.To4() != nil) || (!ipv6 && source.To4() == nil) {
		return nil, fmt.Errorf("invalid source address %q for %s", sourceText, network)
	}
	if !ipv6 {
		source = source.To4()
	}
	dialer := &net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}
	if network == "udp4" || network == "udp6" {
		dialer.LocalAddr = &net.UDPAddr{IP: source}
	} else {
		dialer.LocalAddr = &net.TCPAddr{IP: source}
	}
	if ifIndex <= 0 {
		return dialer, nil
	}
	dialer.Control = func(_, _ string, raw syscall.RawConn) error {
		var optionErr error
		if err := raw.Control(func(fd uintptr) {
			if ipv6 {
				optionErr = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IPV6, darwinIPv6BoundIf, ifIndex)
			} else {
				optionErr = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IP, darwinIPBoundIf, ifIndex)
			}
		}); err != nil {
			return err
		}
		return optionErr
	}
	return dialer, nil
}

func enableTCPDialerTuning(dialer *net.Dialer) {
	previous := dialer.Control
	dialer.Control = func(network, address string, raw syscall.RawConn) error {
		if previous != nil {
			if err := previous(network, address, raw); err != nil {
				return err
			}
		}
		_ = raw.Control(func(fd uintptr) {
			_ = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_RCVBUF, tcpSocketBufferSize)
			_ = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_SNDBUF, tcpSocketBufferSize)
			_ = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_TCP, syscall.TCP_NODELAY, 1)
		})
		return nil
	}
}
