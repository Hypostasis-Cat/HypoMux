//go:build windows

package services

import (
	"context"
	"net"
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func probeICMPv6(ctx context.Context, source, target string) icmpProbeResult {
	srcIP, dstIP := net.ParseIP(source), net.ParseIP(target)
	if srcIP == nil || dstIP == nil || srcIP.To4() != nil || dstIP.To4() != nil || srcIP.IsUnspecified() || srcIP.IsMulticast() || srcIP.IsLinkLocalUnicast() || dstIP.IsUnspecified() || dstIP.IsMulticast() || dstIP.IsLinkLocalUnicast() {
		return icmpProbeResult{Status: "unavailable", LossRate: -1, Note: "invalid IPv6 source or target"}
	}
	create := iphlpapiDLL.NewProc("Icmp6CreateFile")
	send := iphlpapiDLL.NewProc("Icmp6SendEcho2")
	handle, _, _ := create.Call()
	if handle == 0 || handle == ^uintptr(0) {
		return icmpProbeResult{Status: "unavailable", LossRate: -1, Note: "Icmp6CreateFile failed"}
	}
	defer icmpCloseHandleProc.Call(handle)
	src, dst := windows.RawSockaddrInet6{Family: windows.AF_INET6}, windows.RawSockaddrInet6{Family: windows.AF_INET6}
	copy(src.Addr[:], srcIP.To16())
	copy(dst.Addr[:], dstIP.To16())
	type echoReply struct {
		Address       [28]byte
		Status        uint32
		RoundTripTime uint32
	}
	payload := []byte("HypoMux-IPv6-Diagnostic")
	reply := make([]byte, int(unsafe.Sizeof(echoReply{}))+len(payload)+64)
	sent, received, total, minimum, maximum := 0, 0, 0, 0, 0
	for range diagnosticProbeCount {
		if ctx.Err() != nil {
			break
		}
		timeout := time.Second
		if deadline, ok := ctx.Deadline(); ok {
			timeout = min(timeout, time.Until(deadline))
		}
		if timeout <= 0 {
			break
		}
		sent++
		count, _, _ := send.Call(handle, 0, 0, 0, uintptr(unsafe.Pointer(&src)), uintptr(unsafe.Pointer(&dst)), uintptr(unsafe.Pointer(&payload[0])), uintptr(uint16(len(payload))), 0, uintptr(unsafe.Pointer(&reply[0])), uintptr(uint32(len(reply))), uintptr(max(1, timeout/time.Millisecond)))
		runtime.KeepAlive(src)
		runtime.KeepAlive(dst)
		runtime.KeepAlive(payload)
		runtime.KeepAlive(reply)
		if count == 0 {
			continue
		}
		result := (*echoReply)(unsafe.Pointer(&reply[0]))
		if result.Status != 0 {
			continue
		}
		rtt := int(result.RoundTripTime)
		if received == 0 || rtt < minimum {
			minimum = rtt
		}
		if rtt > maximum {
			maximum = rtt
		}
		received++
		total += rtt
	}
	if sent == 0 {
		return icmpProbeResult{Status: "unavailable", LossRate: -1, Note: "cancelled"}
	}
	loss, latency := (sent-received)*100/sent, 0
	if received > 0 {
		latency = total / received
	}
	status := "available"
	if received == 0 {
		status = "unavailable"
	} else if loss >= 5 || maximum-minimum > 100 {
		status = "unstable"
	}
	return icmpProbeResult{Status: status, LossRate: loss, AvgLatencyMS: latency, JitterMS: maximum - minimum, Sent: sent, Received: received}
}
