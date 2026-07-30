//go:build windows

package diagnostic

import (
	"fmt"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	ipSuccess               = 0
	errorNetworkUnreachable = 1231
	errorInvalidNetname     = 1214
)

var (
	ipHelperAPI       = windows.NewLazySystemDLL("iphlpapi.dll")
	icmpCreateFile    = ipHelperAPI.NewProc("IcmpCreateFile")
	icmpCloseHandle   = ipHelperAPI.NewProc("IcmpCloseHandle")
	icmpSendEcho2Ex   = ipHelperAPI.NewProc("IcmpSendEcho2Ex")
	invalidICMPHandle = ^uintptr(0)
)

// Only the stable prefix is read. It is shared by ICMP_ECHO_REPLY and
// ICMP_ECHO_REPLY32, avoiding pointer-width layout differences after RTT.
type echoReplyPrefix struct {
	Address       uint32
	Status        uint32
	RoundTripTime uint32
}

type windowsProber struct {
	handle uintptr
}

func newProber() (prober, error) {
	handle, _, callErr := icmpCreateFile.Call()
	if handle == 0 || handle == invalidICMPHandle {
		return nil, fmt.Errorf("%w", callErr)
	}
	return &windowsProber{handle: handle}, nil
}

func (p *windowsProber) Close() {
	if p.handle != 0 && p.handle != invalidICMPHandle {
		icmpCloseHandle.Call(p.handle)
		p.handle = 0
	}
}

func (p *windowsProber) Probe(source, target [4]byte, payload []byte, timeout time.Duration) probeResult {
	// Microsoft requires enough room for one reply, the request payload,
	// eight ICMP error bytes, and an IO_STATUS_BLOCK. Keep a generous fixed
	// margin because only the reply prefix is consumed.
	reply := make([]byte, 64+len(payload))
	sourceAddress := ipv4Address(source)
	targetAddress := ipv4Address(target)
	timeoutMS := uint32(timeout / time.Millisecond)
	if timeoutMS == 0 {
		timeoutMS = 1
	}

	count, _, callErr := icmpSendEcho2Ex.Call(
		p.handle,
		0,
		0,
		0,
		uintptr(sourceAddress),
		uintptr(targetAddress),
		uintptr(unsafe.Pointer(&payload[0])),
		uintptr(uint16(len(payload))),
		0,
		uintptr(unsafe.Pointer(&reply[0])),
		uintptr(uint32(len(reply))),
		uintptr(timeoutMS),
	)
	if count == 0 {
		code := winErrorCode(callErr)
		return probeResult{
			bindError: code == errorNetworkUnreachable || code == errorInvalidNetname,
			errorCode: code,
		}
	}

	prefix := (*echoReplyPrefix)(unsafe.Pointer(&reply[0]))
	if prefix.Status != ipSuccess {
		return probeResult{}
	}
	return probeResult{
		roundTripTimeMS: int(prefix.RoundTripTime),
		success:         true,
	}
}

func ipv4Address(ip [4]byte) uint32 {
	return uint32(ip[0]) |
		uint32(ip[1])<<8 |
		uint32(ip[2])<<16 |
		uint32(ip[3])<<24
}

func winErrorCode(err error) uint32 {
	if errno, ok := err.(syscall.Errno); ok {
		return uint32(errno)
	}
	return 0
}
