package proxy

import (
	"bytes"
	"net/netip"
	"testing"
)

func TestUDPReplyBufferPreservesAddressAndPacketBoundaries(t *testing.T) {
	for _, target := range []string{"192.0.2.1:443", "[2001:db8::1]:443", "[::ffff:192.0.2.1]:443"} {
		addr := netip.MustParseAddrPort(target)
		t.Run(target, func(t *testing.T) {
			packet, headerSize, ok := newSOCKSUDPReplyBuffer(addr)
			if !ok || len(packet)-headerSize != maxSOCKSUDPDatagramBytes {
				t.Fatal("invalid packet buffer")
			}
			for _, size := range []int{1200, 1, 8192, 17, maxSOCKSUDPDatagramBytes} {
				payload := bytes.Repeat([]byte{byte(size)}, size)
				// Simulate the upstream socket writing directly into the payload region.
				n, err := bytes.NewReader(payload).Read(packet[headerSize:])
				if err != nil {
					t.Fatal(err)
				}
				want, ok := packSOCKSUDPReply(addr, payload)
				if !ok || !bytes.Equal(packet[:headerSize+n], want) {
					t.Fatalf("header, payload or datagram boundary changed at length %d", size)
				}
			}
		})
	}
	for _, target := range []string{"invalid", "192.0.2.1:0", "[2001:db8::1]:65536"} {
		addr, err := netip.ParseAddrPort(target)
		if err != nil {
			// Unparseable input can never reach the encoder.
			continue
		}
		if _, _, ok := newSOCKSUDPReplyBuffer(addr); ok {
			t.Fatalf("accepted %q", target)
		}
	}
}

var udpReplyBenchmarkSink []byte

// Measures packet preparation only, not network or end-to-end throughput.
func BenchmarkUDPReplyPreparation(b *testing.B) {
	payload := bytes.Repeat([]byte{0xa5}, 1200)
	target := netip.MustParseAddrPort("192.0.2.1:443")
	b.Run("per-packet-allocation", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			packet, _ := packSOCKSUDPReply(target, payload)
			udpReplyBenchmarkSink = packet
		}
	})
	b.Run("flow-buffer", func(b *testing.B) {
		packet, headerSize, _ := newSOCKSUDPReplyBuffer(target)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			// Include a payload copy to avoid attributing gains to an empty loop.
			// Production receives directly into this region from the socket.
			copy(packet[headerSize:], payload)
			udpReplyBenchmarkSink = packet[:headerSize+len(payload)]
		}
	})
}
