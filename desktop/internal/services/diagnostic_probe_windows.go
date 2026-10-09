//go:build windows

package services

import (
	"context"
	"encoding/binary"
	"fmt"
	"math/bits"
	"net"
	"syscall"
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
	if ip := net.ParseIP(source); ip != nil && ip.To4() == nil {
		return probeICMPv6(ctx, source, target)
	}
	sourceIP := net.ParseIP(source).To4()
	targetIP := net.ParseIP(target).To4()
	if sourceIP == nil || targetIP == nil {
		return icmpProbeResult{Status: "unavailable", LossRate: -1, Note: "invalid IPv4 address"}
	}
	handle, _, _ := icmpCreateFileProc.Call()
	if handle == 0 || handle == ^uintptr(0) {
		return icmpProbeResult{Status: "unavailable", LossRate: -1, Note: "IcmpCreateFile failed"}
	}
	defer icmpCloseHandleProc.Call(handle)

	payload := []byte("HypoMux-Diagnostic-Probe")
	reply := make([]byte, int(unsafe.Sizeof(icmpEchoReply{}))+len(payload)+16)
	var rtts []int
	bindFailed := false
	note := ""
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
			} else {
				note = fmt.Sprintf("ICMP reply status: %d", response.Status)
			}
		} else if errno, ok := callErr.(syscall.Errno); ok {
			note = diagnosticICMPError(uint32(errno))
			// Timeouts represent unanswered probes; other API failures do not
			// establish that a packet was sent and must not become packet loss.
			if errno != 11010 {
				bindFailed = true
			}
		} else {
			bindFailed = true
			note = fmt.Sprintf("ICMP API returned no reply: %v", callErr)
		}
	}
	return summarizeICMP(rtts, diagnosticProbeCount, bindFailed, note)
}

func diagnosticICMPError(code uint32) string {
	if code == 11010 {
		return "ICMP 请求超时 (IP_REQ_TIMED_OUT 11010)，目标未回应；不代表所有网络流量丢失"
	}
	return fmt.Sprintf("ICMP API/status error %d", code)
}

func summarizeICMP(rtts []int, sent int, bindFailed bool, note string) icmpProbeResult {
	if sent <= 0 {
		return icmpProbeResult{Status: "unavailable", LossRate: -1, Note: note}
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
		loss = -1
	}
	return icmpProbeResult{
		Status: status, LossRate: loss, AvgLatencyMS: average, JitterMS: jitter,
		Sent: sent, Received: received, Note: note,
	}
}

func (windowsDiagnosticProbe) BoundEgress(ctx context.Context, adapter AdapterView) diagnosticEgressResult {
	targets := []diagnosticTLSTarget{{"223.5.5.5:443", "dns.alidns.com"}, {"1.12.12.12:443", "doh.pub"}, {"8.8.8.8:443", "dns.google"}}
	network, source, ifIndex := "tcp4", adapter.Address, adapter.IfIndex
	if source == "" && adapter.SourceIPv6 != "" {
		network, source, ifIndex = "tcp6", adapter.SourceIPv6, adapter.IPv6IfIndex
		targets = []diagnosticTLSTarget{{"[2400:3200::1]:443", "dns.alidns.com"}, {"[2001:4860:4860::8888]:443", "dns.google"}}
	}
	sourceIP := net.ParseIP(source)
	if sourceIP == nil || ifIndex <= 0 {
		return diagnosticEgressResult{Detail: "所选网卡缺少有效源地址或接口索引，无法验证绑定出口"}
	}
	dialer := net.Dialer{
		LocalAddr: &net.TCPAddr{IP: sourceIP},
		Control: func(_, _ string, raw syscall.RawConn) error {
			var optionErr error
			controlErr := raw.Control(func(fd uintptr) {
				if network == "tcp6" {
					optionErr = windows.SetsockoptInt(windows.Handle(fd), windows.IPPROTO_IPV6, ipUnicastIf, ifIndex)
				} else {
					optionErr = windows.SetsockoptInt(windows.Handle(fd), windows.IPPROTO_IP, ipUnicastIf, int(bits.ReverseBytes32(uint32(ifIndex))))
				}
			})
			if controlErr != nil {
				return controlErr
			}
			return optionErr
		},
	}
	dial := func(ctx context.Context, endpoint string) (net.Conn, error) {
		conn, err := dialer.DialContext(ctx, network, endpoint)
		if err != nil {
			return nil, err
		}
		local, ok := conn.LocalAddr().(*net.TCPAddr)
		if !ok || !local.IP.Equal(sourceIP) {
			conn.Close()
			return nil, fmt.Errorf("连接源地址与所选网卡不符")
		}
		return conn, nil
	}
	result := probeDiagnosticTargets(ctx, targets, func(ctx context.Context, target diagnosticTLSTarget) diagnosticEgressResult {
		return probeDiagnosticTLS(ctx, target, dial, nil)
	})
	result.Detail = fmt.Sprintf("ifIndex %d: %s", ifIndex, result.Detail)
	return result
}
