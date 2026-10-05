package proxy

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Hypostasis-Cat/HypoMux/engine/internal/dns"
)

func dualStackTestResolver(t *testing.T, server *Server, v4, v6 []string, delayA time.Duration) {
	t.Helper()
	root, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	resolver, err := dns.New(root, dns.Config{Policy: dns.PolicyOff, QueryTimeout: 4 * time.Second}, func(ctx context.Context, network string, _ string, binding dns.Binding) (net.Conn, error) {
		if binding.SourceIPv6 != server.config.Adapters[0].SourceIPv6 {
			t.Error("DNS lost IPv6 source binding")
		}
		client, peer := net.Pipe()
		go func() {
			defer peer.Close()
			peer.SetDeadline(time.Now().Add(2 * time.Second))
			var query []byte
			if strings.HasPrefix(network, "tcp") {
				var length uint16
				if binary.Read(peer, binary.BigEndian, &length) != nil {
					return
				}
				query = make([]byte, length)
				if _, err := io.ReadFull(peer, query); err != nil {
					return
				}
			} else {
				buffer := make([]byte, 4096)
				count, err := peer.Read(buffer)
				if err != nil {
					return
				}
				query = buffer[:count]
			}
			if len(query) < 12 {
				return
			}
			typeID := binary.BigEndian.Uint16(query[len(query)-4:])
			addresses := v4
			if typeID == 28 {
				addresses = v6
			} else if delayA > 0 {
				select {
				case <-time.After(delayA):
				case <-ctx.Done():
					return
				}
			}
			reply := append([]byte(nil), query...)
			reply[2], reply[3], reply[6], reply[7] = 0x81, 0x80, 0, byte(len(addresses))
			for _, text := range addresses {
				ip := net.ParseIP(text)
				if typeID == 1 {
					ip = ip.To4()
				} else {
					ip = ip.To16()
				}
				reply = append(reply, 0xc0, 0x0c)
				reply = binary.BigEndian.AppendUint16(reply, typeID)
				reply = append(reply, 0, 1, 0, 0, 0, 60)
				reply = binary.BigEndian.AppendUint16(reply, uint16(len(ip)))
				reply = append(reply, ip...)
			}
			if strings.HasPrefix(network, "tcp") {
				binary.Write(peer, binary.BigEndian, uint16(len(reply)))
			}
			peer.Write(reply)
		}()
		return client, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	server.resolver = resolver
}

func TestIPv6OnlyNAT64DomainFallbackAndSuccessfulLoserCleanup(t *testing.T) {
	server, err := New(Config{Adapters: []Adapter{{Name: "v6", SourceIPv6: "::1", DNSServers: []string{"::1"}}}})
	if err != nil {
		t.Fatal(err)
	}
	// Native AAAA addresses are unreachable; A records become reachable only
	// through the advertised translator. The fixture returns ipv4only answers
	// as AAAA too, letting us verify the final IPv4 mapping independently.
	dualStackTestResolver(t, server, []string{"192.0.2.33"}, []string{"64:ff9b::c000:aa", "64:ff9b::c000:ab"}, 0)
	loserReleased := make(chan struct{})
	loserPeer := make(chan net.Conn, 1)
	server.dialTCP = func(ctx context.Context, d *net.Dialer, target string) (net.Conn, error) {
		if d.LocalAddr.(*net.TCPAddr).IP.To4() != nil {
			t.Error("NAT64 escaped onto IPv4")
		}
		if target == "[64:ff9b::c000:221]:443" {
			client, peer := net.Pipe()
			peer.Close()
			return client, nil
		}
		<-ctx.Done()
		// Simulate a successful dial completing just as another socket wins.
		client, peer := net.Pipe()
		loserPeer <- peer
		close(loserReleased)
		return client, nil
	}
	connection, _, err := server.dialUpstream(context.Background(), "ipv4-only.example:443", server.scheduler, false, false)
	if err != nil {
		t.Fatal(err)
	}
	connection.Close()
	select {
	case <-loserReleased:
	case <-time.After(time.Second):
		t.Fatal("native loser survived")
	}
	peer := <-loserPeer
	defer peer.Close()
	peer.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := peer.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("successful loser leaked: %v", err)
	}
}

func TestIPv6OnlyNAT64UDPUsesOriginalReplyTarget(t *testing.T) {
	server, err := New(Config{Adapters: []Adapter{{Name: "v6", SourceIPv6: "::1", DNSServers: []string{"::1"}}}, Channels: []Channel{
		{Name: ChannelEthernet, AdapterNames: []string{"v6"}}, {Name: ChannelWiFi, AdapterNames: []string{"v6"}}, {Name: ChannelAggregation, AdapterNames: []string{"v6"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	server.dialUDP = func(_ context.Context, d *net.Dialer, target string) (net.Conn, error) {
		if target != "[64:ff9b::c000:221]:443" || !d.LocalAddr.(*net.UDPAddr).IP.Equal(net.IPv6loopback) {
			t.Errorf("wrong UDP mapping: %s %s", target, d.LocalAddr)
		}
		client, peer := net.Pipe()
		go func() {
			defer peer.Close()
			payload := make([]byte, 1200)
			n, err := peer.Read(payload)
			if err == nil {
				peer.Write(payload[:n])
			}
		}()
		return client, nil
	}
	endpoints, err := server.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer stopServer(t, server)
	dualStackTestResolver(t, server, nil, []string{"64:ff9b::c000:aa"}, 0)
	control, relay := startUDPAssociation(t, endpoints.Channels[ChannelAggregation], 0)
	defer control.Close()
	client := listenUDPClient(t)
	defer client.Close()
	sendSOCKSUDP(t, client, relay, "192.0.2.33:443", []byte("translated"))
	client.SetReadDeadline(time.Now().Add(time.Second))
	buffer := make([]byte, 1500)
	n, _, err := client.ReadFromUDP(buffer)
	if err != nil {
		t.Fatal(err)
	}
	packet, ok := parseSOCKSUDPPacket(buffer[:n])
	if !ok || packet.addr.String() != "192.0.2.33:443" || string(packet.payload) != "translated" {
		t.Fatalf("original UDP identity lost: %+v valid=%t", packet, ok)
	}
}

func TestIPv6OnlyProxyRealSOCKSTCPAndUDP(t *testing.T) {
	requireIPv6Loopback(t, "tcp6")
	tcp, stopTCP := startEchoServerIPv6(t)
	defer stopTCP()
	udp, packets, stopUDP := startUDPEchoServerIPv6(t)
	defer stopUDP()
	server, err := New(Config{Adapters: []Adapter{{Name: "v6-only", SourceIPv6: "::1"}}, Channels: []Channel{
		{Name: ChannelEthernet, AdapterNames: []string{"v6-only"}},
		{Name: ChannelWiFi, AdapterNames: []string{"v6-only"}},
		{Name: ChannelAggregation, AdapterNames: []string{"v6-only"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	endpoints, err := server.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer stopServer(t, server)
	endpoint := endpoints.Channels[ChannelAggregation]
	connection := dialSOCKSIPv6(t, endpoint, tcp)
	assertEcho(t, connection, []byte("ipv6-only-tcp"))
	assertEcho(t, connection, bytes.Repeat([]byte{0xa5}, 256*1024))
	connection.Close()
	control, relay := startUDPAssociation(t, endpoint, 0)
	defer control.Close()
	client := listenUDPClient(t)
	defer client.Close()
	for _, payload := range [][]byte{[]byte("ipv6-only-udp"), bytes.Repeat([]byte{0xa5}, 1200), bytes.Repeat([]byte{0x3c}, 8192)} {
		sendSOCKSUDP(t, client, relay, udp, payload)
		if !bytes.Equal(readSOCKSUDP(t, client), payload) {
			t.Fatal("IPv6-only UDP payload changed")
		}
	}
	if packets.Load() != 3 {
		t.Fatal("IPv6-only UDP flow did not relay all packets")
	}
}

func TestDualStackConnectionFailureFallsBackAndRetriesAddresses(t *testing.T) {
	for _, brokenFamily := range []string{"4", "6"} {
		t.Run(brokenFamily, func(t *testing.T) {
			server, err := New(Config{Adapters: []Adapter{{Name: "dual", SourceIP: "127.0.0.1", SourceIPv6: "::1"}}})
			if err != nil {
				t.Fatal(err)
			}
			dualStackTestResolver(t, server, []string{"192.0.2.1", "192.0.2.2"}, []string{"2001:db8::1", "2001:db8::2"}, 0)
			var calls4, calls6 atomic.Int64
			server.dialTCP = func(ctx context.Context, dialer *net.Dialer, target string) (net.Conn, error) {
				ip := dialer.LocalAddr.(*net.TCPAddr).IP
				family := "6"
				var count int64
				if ip.To4() != nil {
					family = "4"
					count = calls4.Add(1)
				} else {
					count = calls6.Add(1)
				}
				if family == brokenFamily || count == 1 {
					return nil, errors.New("destination connection refused")
				}
				client, peer := net.Pipe()
				peer.Close()
				return client, nil
			}
			connection, adapter, err := server.dialUpstream(context.Background(), "dual.example:443", server.scheduler, false, false)
			if err != nil || adapter.Name != "dual" {
				t.Fatalf("adapter=%s error=%v", adapter.Name, err)
			}
			connection.Close()
			if brokenFamily == "4" && calls6.Load() < 2 || brokenFamily == "6" && calls4.Load() < 2 {
				t.Fatal("did not retry alternate addresses")
			}
		})
	}
}

func TestDualStackSlowDNSDoesNotBlockOtherFamily(t *testing.T) {
	server, err := New(Config{Adapters: []Adapter{{Name: "dual", SourceIP: "127.0.0.1", SourceIPv6: "::1"}}})
	if err != nil {
		t.Fatal(err)
	}
	dualStackTestResolver(t, server, []string{"192.0.2.1"}, []string{"2001:db8::1"}, 800*time.Millisecond)
	server.dialTCP = func(_ context.Context, d *net.Dialer, _ string) (net.Conn, error) {
		if d.LocalAddr.(*net.TCPAddr).IP.To4() != nil {
			t.Error("slow A lookup won")
		}
		client, peer := net.Pipe()
		peer.Close()
		return client, nil
	}
	start := time.Now()
	connection, _, err := server.dialUpstream(context.Background(), "slow.example:443", server.scheduler, false, false)
	if err != nil {
		t.Fatal(err)
	}
	connection.Close()
	if time.Since(start) >= 500*time.Millisecond {
		t.Fatal("AAAA waited for the slow A lookup")
	}
}

func TestDualStackBlackholeReservesSocketForDelayedFamily(t *testing.T) {
	server, err := New(Config{Adapters: []Adapter{{Name: "dual", SourceIP: "127.0.0.1", SourceIPv6: "::1"}}})
	if err != nil {
		t.Fatal(err)
	}
	dualStackTestResolver(t, server, []string{"192.0.2.1"}, []string{"2001:db8::1", "2001:db8::2"}, 300*time.Millisecond)
	var calls6 atomic.Int64
	server.dialTCP = func(ctx context.Context, d *net.Dialer, _ string) (net.Conn, error) {
		if d.LocalAddr.(*net.TCPAddr).IP.To4() == nil {
			calls6.Add(1)
			<-ctx.Done()
			return nil, ctx.Err()
		}
		client, peer := net.Pipe()
		peer.Close()
		return client, nil
	}
	start := time.Now()
	connection, _, err := server.dialUpstream(context.Background(), "blackhole.example:443", server.scheduler, false, false)
	if err != nil {
		t.Fatal(err)
	}
	connection.Close()
	if calls6.Load() != 1 || time.Since(start) > 1500*time.Millisecond {
		t.Fatal("same-family blackholes consumed both sockets before delayed A became available")
	}
}

func TestSOCKSConnectReservesDNSBudgetBeforeSocketDeadline(t *testing.T) {
	echo, stop := startEchoServer(t)
	defer stop()
	s, err := New(Config{ConnectTimeout: 100 * time.Millisecond, DNS: dns.Config{QueryTimeout: time.Second}, Adapters: []Adapter{{Name: "v4", SourceIP: "127.0.0.1"}}})
	if err != nil {
		t.Fatal(err)
	}
	endpoints, err := s.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer stopServer(t, s)
	dualStackTestResolver(t, s, []string{"127.0.0.1"}, nil, 150*time.Millisecond)
	client, err := net.Dial("tcp4", endpoints.SOCKS)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	client.SetDeadline(time.Now().Add(2 * time.Second))
	client.Write([]byte{5, 1, 0})
	if _, err := io.ReadFull(client, make([]byte, 2)); err != nil {
		t.Fatal(err)
	}
	host := []byte("slow.example")
	request := append([]byte{5, 1, 0, 3, byte(len(host))}, host...)
	_, tcpPort, err := net.SplitHostPort(echo)
	if err != nil {
		t.Fatal(err)
	}
	address, err := net.ResolveTCPAddr("tcp4", net.JoinHostPort("127.0.0.1", tcpPort))
	if err != nil {
		t.Fatal(err)
	}
	request = binary.BigEndian.AppendUint16(request, uint16(address.Port))
	client.Write(request)
	reply := make([]byte, 10)
	if _, err := io.ReadFull(client, reply); err != nil || reply[1] != 0 {
		t.Fatalf("DNS consumed the socket deadline: %v %v", reply, err)
	}
	assertEcho(t, client, []byte("budget-preserved"))
}

func TestDualStackLoserIsCancelledWithoutLeakingSocket(t *testing.T) {
	server, err := New(Config{Adapters: []Adapter{{Name: "dual", SourceIP: "127.0.0.1", SourceIPv6: "::1"}}})
	if err != nil {
		t.Fatal(err)
	}
	dualStackTestResolver(t, server, []string{"192.0.2.1"}, []string{"2001:db8::1"}, 0)
	loserDone := make(chan struct{})
	var attempts atomic.Int64
	server.dialTCP = func(ctx context.Context, _ *net.Dialer, _ string) (net.Conn, error) {
		if attempts.Add(1) == 1 {
			<-ctx.Done()
			close(loserDone)
			return nil, ctx.Err()
		}
		client, peer := net.Pipe()
		peer.Close()
		return client, nil
	}
	connection, _, err := server.dialUpstream(context.Background(), "race.example:443", server.scheduler, false, false)
	if err != nil {
		t.Fatal(err)
	}
	connection.Close()
	select {
	case <-loserDone:
	case <-time.After(time.Second):
		t.Fatal("losing dial survived the winner")
	}
}

func TestIPv6OnlyNAT64LiteralKeepsSelectedSource(t *testing.T) {
	server, err := New(Config{Adapters: []Adapter{{Name: "v6", SourceIPv6: "::1", DNSServers: []string{"::1"}}}})
	if err != nil {
		t.Fatal(err)
	}
	dualStackTestResolver(t, server, nil, []string{"64:ff9b::c000:aa", "64:ff9b::c000:ab"}, 0)
	server.dialTCP = func(_ context.Context, dialer *net.Dialer, target string) (net.Conn, error) {
		if !dialer.LocalAddr.(*net.TCPAddr).IP.Equal(net.IPv6loopback) || target != "[64:ff9b::c000:221]:443" {
			t.Errorf("binding or synthesis lost: %s %s", dialer.LocalAddr, target)
		}
		client, peer := net.Pipe()
		peer.Close()
		return client, nil
	}
	connection, _, err := server.dialUpstream(context.Background(), "192.0.2.33:443", server.scheduler, true, false)
	if err != nil {
		t.Fatal(err)
	}
	connection.Close()
}

func TestIPv6OnlyCannotSilentlyDialUnboundIPv4(t *testing.T) {
	server, err := New(Config{Adapters: []Adapter{{Name: "v6", SourceIPv6: "::1"}}})
	if err != nil {
		t.Fatal(err)
	}
	dualStackTestResolver(t, server, nil, []string{"2001:db8::1"}, 0)
	var calls atomic.Int64
	server.dialTCP = func(context.Context, *net.Dialer, string) (net.Conn, error) { calls.Add(1); return nil, io.EOF }
	_, _, err = server.dialUpstream(context.Background(), "192.0.2.33:443", server.scheduler, true, false)
	if err == nil || calls.Load() != 0 || !strings.Contains(err.Error(), "NAT64") {
		t.Fatalf("unbound fallback error=%v dials=%d", err, calls.Load())
	}
}

func TestIPv6OnlyDefaultDNSPrefersAAAAAndKeepsIPv4OnlyDomains(t *testing.T) {
	s, err := New(Config{Adapters: []Adapter{{Name: "v6", SourceIPv6: "::1"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Start(); err != nil {
		t.Fatal(err)
	}
	defer stopServer(t, s)
	dualStackTestResolver(t, s, []string{"192.0.2.44"}, []string{"2001:db8::44"}, 0)
	result, err := s.ResolveDNS(context.Background(), "dual.example", "v6", "")
	if err != nil || result.RecordType != dns.RecordAAAA || result.Address != "2001:db8::44" {
		t.Fatalf("IPv6 connectivity bootstrap selected IPv4: %+v %v", result, err)
	}
	dualStackTestResolver(t, s, []string{"192.0.2.44"}, nil, 0)
	result, err = s.ResolveDNS(context.Background(), "ipv4-only.example", "v6", "")
	if err != nil || result.RecordType != dns.RecordA || result.Address != "192.0.2.44" {
		t.Fatalf("NAT64 domain bootstrap unavailable: %+v %v", result, err)
	}
}
