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

func boundDialer(adapter Adapter, timeout time.Duration) (*net.Dialer, error) {
	source := net.ParseIP(adapter.SourceIP).To4()
	if source == nil {
		return nil, fmt.Errorf("invalid source IPv4 address %q", adapter.SourceIP)
	}
	dialer := &net.Dialer{
		Timeout:   timeout,
		KeepAlive: 30 * time.Second,
		LocalAddr: &net.TCPAddr{IP: source},
	}
	if adapter.IfIndex <= 0 {
		return dialer, nil
	}
	// Windows expects IP_UNICAST_IF as an interface index in network byte
	// order. LocalAddr additionally makes the selected source address explicit.
	networkOrderIndex := int(bits.ReverseBytes32(uint32(adapter.IfIndex)))
	dialer.Control = func(_, _ string, raw syscall.RawConn) error {
		var controlErr error
		if err := raw.Control(func(handle uintptr) {
			controlErr = windows.SetsockoptInt(
				windows.Handle(handle),
				windows.IPPROTO_IP,
				ipUnicastIF,
				networkOrderIndex,
			)
		}); err != nil {
			return err
		}
		return controlErr
	}
	return dialer, nil
}
