//go:build windows

package services

import (
	"context"
	"encoding/binary"
	"fmt"
	"math/bits"
	"net"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	diagnosticProbeCount = 10
	ipUnicastIf          = 31
)

var (
	iphlpapiDLL         = windows.NewLazySystemDLL("iphlpapi.dll")
	icmpCreateFileProc  = iphlpapiDLL.NewProc("IcmpCreateFile")
	icmpCloseHandleProc = iphlpapiDLL.NewProc("IcmpCloseHandle")
	icmpSendEcho2ExProc = iphlpapiDLL.NewProc("IcmpSendEcho2Ex")
)

type windowsDiagnosticProbe struct{}

type icmpEchoReply struct {
	Address       uint32
	Status        uint32
	RoundTripTime uint32
	DataSize      uint16
	Reserved      uint16
	Data          uintptr
	Options       struct {
		TTL         byte
		TOS         byte
		Flags       byte
		OptionsSize byte
		OptionsData uintptr
	}
}

func newDiagnosticProbe() diagnosticProbe {
	return windowsDiagnosticProbe{}
}

func (windowsDiagnosticProbe) ICMP(ctx context.Context, source string, target string) icmpProbeResult {
	sourceIP := net.ParseIP(source).To4()
	targetIP := net.ParseIP(target).To4()
	if sourceIP == nil || targetIP == nil {
		return icmpProbeResult{Status: "unavailable", LossRate: 100, Note: "invalid IPv4 address"}
	}
	handle, _, _ := icmpCreateFileProc.Call()
	if handle == 0 || handle == ^uintptr(0) {
		return icmpProbeResult{Status: "unavailable", LossRate: 100, Note: "IcmpCreateFile failed"}
	}
	defer icmpCloseHandleProc.Call(handle)

	payload := []byte("HypoMux-Diagnostic-Probe")
	reply := make([]byte, int(unsafe.Sizeof(icmpEchoReply{}))+len(payload)+16)
	var rtts []int
	bindFailed := false
	for index := 0; index < diagnosticProbeCount; index++ {
		select {
		case <-ctx.Done():
			return summarizeICMP(rtts, index, bindFailed, "cancelled")
		default:
		}
		count, _, callErr := icmpSendEcho2ExProc.Call(
			handle, 0, 0, 0,
			uintptr(binary.LittleEndian.Uint32(sourceIP)),
			uintptr(binary.LittleEndian.Uint32(targetIP)),
			uintptr(unsafe.Pointer(&payload[0])), uintptr(len(payload)),
			0, uintptr(unsafe.Pointer(&reply[0])), uintptr(len(reply)), 1000,
		)
		if count > 0 {
			response := (*icmpEchoReply)(unsafe.Pointer(&reply[0]))
			if response.Status == 0 {
				rtts = append(rtts, int(response.RoundTripTime))
			}
		} else if errno, ok := callErr.(syscall.Errno); ok && (errno == 1231 || errno == 1214) {
			bindFailed = true
		}
	}
	return summarizeICMP(rtts, diagnosticProbeCount, bindFailed, "")
}

func summarizeICMP(rtts []int, sent int, bindFailed bool, note string) icmpProbeResult {
	if sent <= 0 {
		return icmpProbeResult{Status: "unavailable", LossRate: 100, Note: note}
	}
	received, total, minimum, maximum := len(rtts), 0, 0, 0
	for index, value := range rtts {
		total += value
		if index == 0 || value < minimum {
			minimum = value
		}
		if value > maximum {
			maximum = value
		}
	}
	loss := ((sent - received) * 100) / sent
	average, jitter := 0, 0
	if received > 0 {
		average = total / received
		jitter = maximum - minimum
	}
	status := "available"
	if loss >= 100 || bindFailed {
		status = "unavailable"
	} else if loss >= 5 || jitter > 100 {
		status = "unstable"
	}
	if bindFailed {
		note = "source bind failed (WinError 1231)"
	}
	return icmpProbeResult{
		Status: status, LossRate: loss, AvgLatencyMS: average, JitterMS: jitter,
		Sent: sent, Received: received, Note: note,
	}
}

func (windowsDiagnosticProbe) BoundTCP(ctx context.Context, adapter AdapterView) (bool, string) {
	endpoints := []string{"223.5.5.5:443", "1.12.12.12:443", "8.8.8.8:443"}
	var failures []string
	for _, endpoint := range endpoints {
		dialCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		dialer := net.Dialer{
			Timeout:   2 * time.Second,
			LocalAddr: &net.TCPAddr{IP: net.ParseIP(adapter.Address)},
			Control: func(_, _ string, raw syscall.RawConn) error {
				var optionErr error
				controlErr := raw.Control(func(fd uintptr) {
					networkOrderIndex := bits.ReverseBytes32(uint32(adapter.IfIndex))
					optionErr = windows.SetsockoptInt(
						windows.Handle(fd), windows.IPPROTO_IP, ipUnicastIf, int(networkOrderIndex),
					)
				})
				if controlErr != nil {
					return controlErr
				}
				return optionErr
			},
		}
		connection, err := dialer.DialContext(dialCtx, "tcp4", endpoint)
		cancel()
		if err == nil {
			local := connection.LocalAddr().String()
			_ = connection.Close()
			return true, fmt.Sprintf("TCP %s via %s (ifIndex %d)", endpoint, local, adapter.IfIndex)
		}
		failures = append(failures, fmt.Sprintf("%s: %v", endpoint, err))
	}
	if len(failures) > 2 {
		failures = failures[len(failures)-2:]
	}
	return false, fmt.Sprintf("绑定 TCP 失败：%v", failures)
}
