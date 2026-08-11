//go:build windows

package proxy

import (
	"fmt"
	"math/bits"
	"net"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

const ipUnicastIF = 31
const ipv6UnicastIF = 31

func boundDialer(adapter Adapter, timeout time.Duration) (*net.Dialer, error) {
	return boundNetworkDialer(adapter, timeout, "tcp4")
}

func boundNetworkDialer(adapter Adapter, timeout time.Duration, network string) (*net.Dialer, error) {
	dialer := &net.Dialer{
		Timeout:   timeout,
		KeepAlive: 30 * time.Second,
	}
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
	if network == "udp4" || network == "udp6" {
		dialer.LocalAddr = &net.UDPAddr{IP: source}
	} else {
		dialer.LocalAddr = &net.TCPAddr{IP: source}
	}
	if ifIndex <= 0 {
		return dialer, nil
	}
	// IPv4 expects IP_UNICAST_IF in network byte order. IPv6 expects
	// IPV6_UNICAST_IF in host byte order. LocalAddr additionally makes the
	// selected source address explicit for both families.
	dialer.Control = func(_, _ string, raw syscall.RawConn) error {
		var controlErr error
		if err := raw.Control(func(handle uintptr) {
			if ipv6 {
				controlErr = windows.SetsockoptInt(
					windows.Handle(handle),
					windows.IPPROTO_IPV6,
					ipv6UnicastIF,
					ifIndex,
				)
			} else {
				networkOrderIndex := int(bits.ReverseBytes32(uint32(ifIndex)))
				controlErr = windows.SetsockoptInt(
					windows.Handle(handle),
					windows.IPPROTO_IP,
					ipUnicastIF,
					networkOrderIndex,
				)
			}
		}); err != nil {
			return err
		}
		return controlErr
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
		// These options are an optimization only. Preserve connectivity if a
		// driver or endpoint security policy rejects any of them.
		_ = raw.Control(func(handle uintptr) {
			socket := windows.Handle(handle)
			_ = windows.SetsockoptInt(socket, windows.SOL_SOCKET, windows.SO_RCVBUF, tcpSocketBufferSize)
			_ = windows.SetsockoptInt(socket, windows.SOL_SOCKET, windows.SO_SNDBUF, tcpSocketBufferSize)
			_ = windows.SetsockoptInt(socket, windows.IPPROTO_TCP, windows.TCP_NODELAY, 1)
		})
		return nil
	}
}
