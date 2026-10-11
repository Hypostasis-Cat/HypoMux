package proxy

import (
	"net"
	"net/netip"
	"testing"
	"time"
)

var benchmarkUDPAddress *net.UDPAddr
var benchmarkUDPAddrPort netip.AddrPort

// Compare the address APIs on a real loopback socket. Each iteration exchanges
// one datagram, so this measures syscall/address cost, not hotspot throughput.
func BenchmarkUDPRelayAddressRead(b *testing.B) {
	for _, valueAddress := range []bool{false, true} {
		name := "ReadFromUDP"
		if valueAddress {
			name = "ReadFromUDPAddrPort"
		}
		b.Run(name, func(b *testing.B) {
			receiver, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
			if err != nil {
				b.Fatal(err)
			}
			defer receiver.Close()
			sender, err := net.DialUDP("udp4", nil, receiver.LocalAddr().(*net.UDPAddr))
			if err != nil {
				b.Fatal(err)
			}
			defer sender.Close()
			_ = receiver.SetReadDeadline(time.Now().Add(time.Minute))
			payload, buffer := make([]byte, 1200), make([]byte, 1500)
			b.ReportAllocs()
			b.SetBytes(int64(len(payload)))
			b.ResetTimer()
			for b.Loop() {
				if _, err := sender.Write(payload); err != nil {
					b.Fatal(err)
				}
				var count int
				if valueAddress {
					count, benchmarkUDPAddrPort, err = receiver.ReadFromUDPAddrPort(buffer)
				} else {
					count, benchmarkUDPAddress, err = receiver.ReadFromUDP(buffer)
				}
				if err != nil || count != len(payload) {
					b.Fatalf("read %d bytes: %v", count, err)
				}
			}
		})
	}
}
