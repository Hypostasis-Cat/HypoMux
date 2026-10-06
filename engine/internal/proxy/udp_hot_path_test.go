package proxy

import (
	"bytes"
	"encoding/binary"
	"net"
	"net/netip"
	"testing"
	"time"
)

func TestParseSOCKSUDPIPv4MappedAddressReusesIPv4FlowKey(t *testing.T) {
	ipv4 := netip.MustParseAddrPort("192.0.2.1:443")
	mapped := netip.MustParseAddr("::ffff:192.0.2.1").As16()
	wire := append([]byte{0, 0, 0, 4}, mapped[:]...)
	wire = binary.BigEndian.AppendUint16(wire, ipv4.Port())
	wire = append(wire, []byte("datagram")...)
	packet, ok := parseSOCKSUDPPacket(wire)
	if !ok || packet.addr != ipv4 || string(packet.payload) != "datagram" {
		t.Fatal("mapped request did not share the native IPv4 flow key")
	}
	reply, ok := packSOCKSUDPReply(packet.addr, packet.payload)
	want := append([]byte{0, 0, 0, 1, 192, 0, 2, 1, 1, 187}, []byte("datagram")...)
	if !ok || !bytes.Equal(reply, want) {
		t.Fatal("mapped request was not replied to as IPv4")
	}
}

// Keep socket syscalls out of allocation measurements while exercising the
// complete existing-flow forwarding path, including telemetry and failover.
type udpHotPathConn struct {
	net.Conn
	writes int
}

func (c *udpHotPathConn) Write(payload []byte) (int, error) {
	c.writes++
	return len(payload), nil
}

func TestUDPExistingFlowForwardDoesNotAllocate(t *testing.T) {
	for _, strategy := range []string{StrategyRoundRobin, StrategyLatency} {
		for _, silent := range []bool{false, true} {
			// Latency-first silent flows enter the comparative-probe recovery
			// path, whose candidate selection has separate allocations.
			if strategy == StrategyLatency && silent {
				continue
			}
			name := strategy + "/responsive"
			if silent {
				name = strategy + "/silent"
			}
			t.Run(name, func(t *testing.T) {
				addr := netip.MustParseAddrPort("192.0.2.1:443")
				s := newScheduler([]Adapter{{Name: "a", SourceIP: "127.0.0.1"}}, false)
				s.setLatency(newLatencyTable())
				s.setStrategy(strategy)
				conn := &udpHotPathConn{}
				a := &udpAssociation{
					server:    &Server{registry: newRegistry(nil)},
					scheduler: s,
					flows:     make(map[netip.AddrPort]*udpFlow),
				}
				flow := &udpFlow{association: a, addr: addr, target: addr.String(), adapter: s.adapters[0], connection: conn, session: &connection{}}
				replied := time.Now()
				if silent {
					replied = replied.Add(-time.Hour)
				}
				flow.lastReply.Store(replied.UnixNano())
				a.flows[addr] = flow
				packet := socksUDPPacket{addr: addr, payload: []byte("datagram")}
				allocs := testing.AllocsPerRun(1000, func() { a.forward(nil, packet) })
				if allocs != 0 {
					t.Fatalf("existing-flow forwarding allocated %g times per packet", allocs)
				}
				if conn.writes != 1001 || flow.session.bytesUp.Load() != uint64(conn.writes*len(packet.payload)) || a.flows[addr] != flow {
					t.Fatal("forwarding lost/replayed a datagram or replaced a flow without failure evidence")
				}
			})
		}
	}
}

func BenchmarkParseSOCKSUDPPacket(b *testing.B) {
	for _, target := range []string{"192.0.2.1:443", "[2001:db8::1]:443", "[::ffff:192.0.2.1]:443"} {
		b.Run(target, func(b *testing.B) {
			addr := netip.MustParseAddrPort(target)
			wire := []byte{0, 0, 0}
			if addr.Addr().Is4() {
				ip := addr.Addr().As4()
				wire = append(wire, 1)
				wire = append(wire, ip[:]...)
			} else {
				ip := addr.Addr().As16()
				wire = append(wire, 4)
				wire = append(wire, ip[:]...)
			}
			wire = binary.BigEndian.AppendUint16(wire, addr.Port())
			wire = append(wire, []byte("datagram")...)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				packet, ok := parseSOCKSUDPPacket(wire)
				if !ok || len(packet.payload) != 8 {
					b.Fatal("invalid datagram")
				}
			}
		})
	}
}
