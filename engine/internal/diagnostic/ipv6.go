package diagnostic

import (
	"context"
	"fmt"
	"net"
	"time"
)

type ipv6Prober interface {
	ProbeIPv6(source, target [16]byte, ifIndex uint32, payload []byte, timeout time.Duration) probeResult
	Close()
}

func runIPv6(ctx context.Context, config Config, base Result) Result {
	source, target := net.ParseIP(base.SourceIP), net.ParseIP(base.TargetIP)
	if source == nil || source.To4() != nil || source.IsUnspecified() || source.IsMulticast() || source.IsLinkLocalUnicast() {
		base.Note = "invalid --src-ip"
		return base
	}
	if target == nil || target.To4() != nil || target.IsUnspecified() || target.IsMulticast() || target.IsLinkLocalUnicast() {
		base.Note = "invalid --target-ip"
		return base
	}
	prober, err := newIPv6Prober()
	if err != nil {
		base.Note = fmt.Sprintf("Icmp6CreateFile failed: %v", err)
		return base
	}
	defer prober.Close()
	var src, dst [16]byte
	copy(src[:], source.To16())
	copy(dst[:], target.To16())
	var rtts []int
	bindError := false
	var bindCode uint32
	for range config.Count {
		if ctx.Err() != nil {
			result := summarize(base, rtts, base.Sent, bindError, bindCode)
			result.Note = "cancelled"
			return result
		}
		timeout := config.Timeout
		if deadline, ok := ctx.Deadline(); ok {
			timeout = min(timeout, time.Until(deadline))
		}
		if timeout <= 0 {
			break
		}
		base.Sent++
		result := prober.ProbeIPv6(src, dst, uint32(max(0, config.IfIndex)), probePayload, timeout)
		if result.success {
			rtts = append(rtts, result.roundTripTimeMS)
		}
		if result.bindError {
			bindError, bindCode = true, result.errorCode
		}
	}
	return summarize(base, rtts, base.Sent, bindError, bindCode)
}
