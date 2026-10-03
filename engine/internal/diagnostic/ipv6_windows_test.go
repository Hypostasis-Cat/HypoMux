//go:build windows

package diagnostic

import (
	"context"
	"testing"
	"time"
	"unsafe"
)

func TestIPv6ICMPRealLoopback(t *testing.T) {
	if unsafe.Offsetof(ipv6EchoReply{}.Status) != 28 || unsafe.Offsetof(ipv6EchoReply{}.RoundTripTime) != 32 {
		t.Fatal("ICMPV6_ECHO_REPLY SDK layout mismatch")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result := Run(ctx, Config{SourceIP: "::1", TargetIP: "::1", Count: 1, Timeout: time.Second})
	if result.Sent != 1 || result.Received != 1 || result.Status != "available" {
		t.Fatalf("IPv6 ICMP loopback=%+v", result)
	}
}
