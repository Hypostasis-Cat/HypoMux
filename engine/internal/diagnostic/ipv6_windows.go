//go:build windows

package diagnostic

import (
	"fmt"
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var icmp6CreateFile = ipHelperAPI.NewProc("Icmp6CreateFile")
var icmp6SendEcho2 = ipHelperAPI.NewProc("Icmp6SendEcho2")

type ipv6EchoReply struct {
	Address       [28]byte
	Status        uint32
	RoundTripTime uint32
}

func newIPv6Prober() (ipv6Prober, error) {
	handle, _, err := icmp6CreateFile.Call()
	if handle == 0 || handle == invalidICMPHandle {
		return nil, fmt.Errorf("%w", err)
	}
	return &windowsProber{handle: handle}, nil
}

func (p *windowsProber) ProbeIPv6(source, target [16]byte, ifIndex uint32, payload []byte, timeout time.Duration) probeResult {
	src := windows.RawSockaddrInet6{Family: windows.AF_INET6, Addr: source}
	dst := windows.RawSockaddrInet6{Family: windows.AF_INET6, Addr: target}
	// Scope IDs are only meaningful for scoped addresses; the explicit global
	// source binds the request to its selected interface.
	reply := make([]byte, int(unsafe.Sizeof(ipv6EchoReply{}))+len(payload)+64)
	count, _, err := icmp6SendEcho2.Call(p.handle, 0, 0, 0, uintptr(unsafe.Pointer(&src)), uintptr(unsafe.Pointer(&dst)), uintptr(unsafe.Pointer(&payload[0])), uintptr(uint16(len(payload))), 0, uintptr(unsafe.Pointer(&reply[0])), uintptr(uint32(len(reply))), uintptr(max(1, timeout/time.Millisecond)))
	runtime.KeepAlive(src)
	runtime.KeepAlive(dst)
	runtime.KeepAlive(payload)
	runtime.KeepAlive(reply)
	if count == 0 {
		code := winErrorCode(err)
		return probeResult{bindError: code == errorNetworkUnreachable || code == errorInvalidNetname, errorCode: code}
	}
	result := (*ipv6EchoReply)(unsafe.Pointer(&reply[0]))
	return probeResult{success: result.Status == ipSuccess, roundTripTimeMS: int(result.RoundTripTime)}
}
