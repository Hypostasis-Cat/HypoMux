package diagnostic

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"
)

const (
	DefaultTargetIP = "223.5.5.5"
	DefaultCount    = 10
	MaxCount        = 100
)

var (
	DefaultTimeout = time.Second
	probePayload   = []byte("HypoMux-Diagnostic-Probe")
)

type Config struct {
	SourceIP string
	TargetIP string
	Count    int
	Timeout  time.Duration
}

type Result struct {
	Status       string `json:"status"`
	LossRate     int    `json:"loss_rate"`
	AvgLatencyMS int    `json:"avg_latency_ms"`
	JitterMS     int    `json:"jitter_ms"`
	Sent         int    `json:"sent"`
	Received     int    `json:"received"`
	SourceIP     string `json:"src_ip"`
	TargetIP     string `json:"target_ip"`
	Note         string `json:"note"`
}

type probeResult struct {
	roundTripTimeMS int
	success         bool
	bindError       bool
	errorCode       uint32
}

type prober interface {
	Probe(source, target [4]byte, payload []byte, timeout time.Duration) probeResult
	Close()
}

func Run(ctx context.Context, config Config) Result {
	config = withDefaults(config)
	base := Result{
		Status:   "unavailable",
		LossRate: 100,
		SourceIP: strings.TrimSpace(config.SourceIP),
		TargetIP: strings.TrimSpace(config.TargetIP),
	}

	source, ok := parseIPv4(base.SourceIP)
	if !ok {
		base.Note = "invalid --src-ip"
		return base
	}
	target, ok := parseIPv4(base.TargetIP)
	if !ok {
		base.Note = "invalid --target-ip"
		return base
	}

	icmp, err := newProber()
	if err != nil {
		base.Note = fmt.Sprintf("IcmpCreateFile failed: %v", err)
		return base
	}
	defer icmp.Close()

	rtts := make([]int, 0, config.Count)
	bindError := false
	var bindErrorCode uint32
	for range config.Count {
		select {
		case <-ctx.Done():
			result := summarize(base, rtts, base.Sent, bindError, bindErrorCode)
			result.Note = "cancelled"
			return result
		default:
		}

		base.Sent++
		probe := icmp.Probe(source, target, probePayload, config.Timeout)
		if probe.success {
			rtts = append(rtts, probe.roundTripTimeMS)
		}
		if probe.bindError {
			bindError = true
			bindErrorCode = probe.errorCode
		}
	}
	return summarize(base, rtts, base.Sent, bindError, bindErrorCode)
}

func withDefaults(config Config) Config {
	config.SourceIP = strings.TrimSpace(config.SourceIP)
	config.TargetIP = strings.TrimSpace(config.TargetIP)
	if config.TargetIP == "" {
		config.TargetIP = DefaultTargetIP
	}
	if config.Count <= 0 {
		config.Count = DefaultCount
	} else if config.Count > MaxCount {
		config.Count = MaxCount
	}
	if config.Timeout <= 0 {
		config.Timeout = DefaultTimeout
	} else if config.Timeout > 10*time.Second {
		config.Timeout = 10 * time.Second
	}
	return config
}

func parseIPv4(value string) ([4]byte, bool) {
	var result [4]byte
	ip := net.ParseIP(strings.TrimSpace(value)).To4()
	if ip == nil {
		return result, false
	}
	copy(result[:], ip)
	return result, true
}

func summarize(base Result, rtts []int, sent int, bindError bool, bindErrorCode uint32) Result {
	base.Sent = sent
	base.Received = len(rtts)
	if sent == 0 {
		base.LossRate = 100
	} else {
		base.LossRate = ((sent - base.Received) * 100) / sent
	}

	if len(rtts) > 0 {
		sum, minimum, maximum := 0, rtts[0], rtts[0]
		for _, rtt := range rtts {
			sum += rtt
			if rtt < minimum {
				minimum = rtt
			}
			if rtt > maximum {
				maximum = rtt
			}
		}
		base.AvgLatencyMS = sum / len(rtts)
		base.JitterMS = maximum - minimum
	}

	switch {
	case base.LossRate >= 100 || bindError:
		base.Status = "unavailable"
	case base.LossRate >= 5 || base.JitterMS > 100:
		base.Status = "unstable"
	default:
		base.Status = "available"
	}
	if bindError {
		base.Note = fmt.Sprintf("source bind failed (WinError %d)", bindErrorCode)
	}
	return base
}
