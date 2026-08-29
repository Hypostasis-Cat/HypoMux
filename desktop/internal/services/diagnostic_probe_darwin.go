//go:build darwin

package services

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var (
	darwinPingPacketsPattern = regexp.MustCompile(`([0-9]+) packets transmitted, ([0-9]+) packets received, ([0-9.]+)% packet loss`)
	darwinPingRTTPattern     = regexp.MustCompile(`(?:round-trip|rtt) min/avg/max/(?:stddev|mdev) = ([0-9.]+)/([0-9.]+)/([0-9.]+)/([0-9.]+) ms`)
)

type darwinDiagnosticProbe struct{}

func newDiagnosticProbe() diagnosticProbe {
	return darwinDiagnosticProbe{}
}

func (darwinDiagnosticProbe) ICMP(ctx context.Context, source string, target string) icmpProbeResult {
	if net.ParseIP(source).To4() == nil || net.ParseIP(target).To4() == nil {
		return icmpProbeResult{Status: "unavailable", LossRate: 100, Note: "invalid IPv4 address"}
	}
	probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	output, commandErr := exec.CommandContext(
		probeCtx, "/sbin/ping", "-n", "-S", source, "-c", "10", "-W", "1000", target,
	).CombinedOutput()
	result := parseDarwinPing(string(output))
	if probeCtx.Err() != nil {
		result.Note = "ping cancelled or timed out"
		if result.Sent == 0 {
			result.Status, result.LossRate = "unavailable", 100
		}
	} else if commandErr != nil && result.Sent == 0 {
		result.Status, result.LossRate = "unavailable", 100
		result.Note = strings.TrimSpace(string(output))
		if result.Note == "" {
			result.Note = commandErr.Error()
		}
	}
	return result
}

func parseDarwinPing(output string) icmpProbeResult {
	result := icmpProbeResult{Status: "unavailable", LossRate: 100}
	packets := darwinPingPacketsPattern.FindStringSubmatch(output)
	if len(packets) == 4 {
		result.Sent, _ = strconv.Atoi(packets[1])
		result.Received, _ = strconv.Atoi(packets[2])
		loss, _ := strconv.ParseFloat(packets[3], 64)
		result.LossRate = int(loss + 0.5)
	}
	rtt := darwinPingRTTPattern.FindStringSubmatch(output)
	if len(rtt) == 5 {
		average, _ := strconv.ParseFloat(rtt[2], 64)
		minimum, _ := strconv.ParseFloat(rtt[1], 64)
		maximum, _ := strconv.ParseFloat(rtt[3], 64)
		result.AvgLatencyMS = int(average + 0.5)
		result.JitterMS = int(maximum - minimum + 0.5)
	}
	if result.Received > 0 {
		result.Status = "available"
		if result.LossRate >= 5 || result.JitterMS > 100 {
			result.Status = "unstable"
		}
	}
	return result
}

func (darwinDiagnosticProbe) BoundTCP(ctx context.Context, adapter AdapterView) (bool, string) {
	endpoints := []string{"1.1.1.1:443", "8.8.8.8:443", "223.5.5.5:443"}
	failures := make([]string, 0, len(endpoints))
	for _, endpoint := range endpoints {
		dialCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		dialer := net.Dialer{
			Timeout:   2 * time.Second,
			LocalAddr: &net.TCPAddr{IP: net.ParseIP(adapter.Address)},
			Control: func(_, _ string, raw syscall.RawConn) error {
				var optionErr error
				if err := raw.Control(func(fd uintptr) {
					optionErr = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IP, 25, adapter.IfIndex)
				}); err != nil {
					return err
				}
				return optionErr
			},
		}
		connection, err := dialer.DialContext(dialCtx, "tcp4", endpoint)
		cancel()
		if err == nil {
			local := connection.LocalAddr().String()
			_ = connection.Close()
			return true, fmt.Sprintf("TCP %s via %s (%s)", endpoint, local, adapter.ID)
		}
		failures = append(failures, fmt.Sprintf("%s: %v", endpoint, err))
	}
	if len(failures) > 2 {
		failures = failures[len(failures)-2:]
	}
	return false, fmt.Sprintf("绑定 TCP 失败：%v", failures)
}
