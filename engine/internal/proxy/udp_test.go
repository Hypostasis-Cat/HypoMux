package proxy

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

func TestSOCKSUDPAssociationReusesFlowLocksClientAndReportsTelemetry(t *testing.T) {
	echoAddress, echoPackets, stopEcho := startUDPEchoServer(t)
	defer stopEcho()
	server := newTUNPoolTestServer(t)
	var udpDials atomic.Int64
	server.dialUDP = func(
		ctx context.Context,
		dialer *net.Dialer,
		target string,
	) (net.Conn, error) {
		udpDials.Add(1)
		return dialer.DialContext(ctx, "udp4", target)
	}
	endpoints, err := server.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer stopServer(t, server)

	control, relay := startUDPAssociation(
		t,
		endpoints.Channels[ChannelEthernet],
		0,
	)
	defer control.Close()
	client := listenUDPClient(t)
	defer client.Close()

	for _, payload := range [][]byte{[]byte("one"), []byte("two")} {
		sendSOCKSUDP(t, client, relay, echoAddress, payload)
		if reply := readSOCKSUDP(t, client); string(reply) != string(payload) {
			t.Fatalf("UDP reply = %q, want %q", reply, payload)
		}
	}
	if udpDials.Load() != 1 {
		t.Fatalf("physical UDP dials = %d, want 1", udpDials.Load())
	}
	if echoPackets.Load() != 2 {
		t.Fatalf("echo packets = %d, want 2", echoPackets.Load())
	}

	flow := waitForUDPFlowTelemetry(
		t,
		server,
		uint64(len("one")+len("two")),
		uint64(len("one")+len("two")),
		time.Second,
	)
	if flow.Channel != ChannelEthernet || flow.Adapter != "wired" {
		t.Fatalf("UDP flow escaped channel subset: %#v", flow)
	}

	spoof := listenUDPClient(t)
	defer spoof.Close()
	sendSOCKSUDP(t, spoof, relay, echoAddress, []byte("spoofed"))
	_ = spoof.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
	if _, _, err := spoof.ReadFromUDP(make([]byte, 128)); err == nil {
		t.Fatal("different local UDP endpoint received a relay reply")
	}
	if echoPackets.Load() != 2 {
		t.Fatalf("spoofed packet reached upstream, count = %d", echoPackets.Load())
	}

	_ = control.Close()
	waitForUDPFlows(t, server, 0, time.Second)
}

func TestSOCKSUDPRejectsInvalidPacketsBeforeLockingClient(t *testing.T) {
	echoAddress, _, stopEcho := startUDPEchoServer(t)
	defer stopEcho()
	server := newTUNPoolTestServer(t)
	var udpDials atomic.Int64
	server.dialUDP = func(
		ctx context.Context,
		dialer *net.Dialer,
		target string,
	) (net.Conn, error) {
		udpDials.Add(1)
		return dialer.DialContext(ctx, "udp4", target)
	}
	endpoints, err := server.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer stopServer(t, server)
	control, relay := startUDPAssociation(
		t,
		endpoints.Channels[ChannelAggregation],
		0,
	)
	defer control.Close()

	invalidClient := listenUDPClient(t)
	defer invalidClient.Close()
	invalid := [][]byte{
		{0, 0, 1, 1, 127, 0, 0, 1, 0, 53, 1},
		{0, 0, 0, 3, 3, 'd', 'n', 's', 0, 53, 1},
		append([]byte{0, 0, 0, 4}, make([]byte, 18)...),
		{0, 0, 0, 1, 127, 0, 0, 1, 0, 0, 1},
		{0, 0, 0, 1, 127, 0, 0, 1, 0, 53},
		{0, 0, 0},
	}
	for _, packet := range invalid {
		if _, err := invalidClient.WriteToUDP(packet, relay); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(100 * time.Millisecond)
	if udpDials.Load() != 0 {
		t.Fatalf("invalid packets created %d upstream flows", udpDials.Load())
	}

	validClient := listenUDPClient(t)
	defer validClient.Close()
	sendSOCKSUDP(t, validClient, relay, echoAddress, []byte("valid"))
	if reply := readSOCKSUDP(t, validClient); string(reply) != "valid" {
		t.Fatalf("valid reply = %q", reply)
	}
	if udpDials.Load() != 1 {
		t.Fatalf("first valid client did not create exactly one flow")
	}
}

func TestSOCKSUDPEnforcesRequestedClientPort(t *testing.T) {
	echoAddress, echoPackets, stopEcho := startUDPEchoServer(t)
	defer stopEcho()
	server := newTUNPoolTestServer(t)
	endpoints, err := server.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer stopServer(t, server)
	client := listenUDPClient(t)
	defer client.Close()
	requestedPort := client.LocalAddr().(*net.UDPAddr).Port
	control, relay := startUDPAssociation(
		t,
		endpoints.Channels[ChannelAggregation],
		requestedPort,
	)
	defer control.Close()

	spoof := listenUDPClient(t)
	defer spoof.Close()
	sendSOCKSUDP(t, spoof, relay, echoAddress, []byte("wrong-port"))
	time.Sleep(100 * time.Millisecond)
	if echoPackets.Load() != 0 {
		t.Fatal("non-requested UDP client port reached upstream")
	}

	sendSOCKSUDP(t, client, relay, echoAddress, []byte("requested-port"))
	if reply := readSOCKSUDP(t, client); string(reply) != "requested-port" {
		t.Fatalf("requested-port reply = %q", reply)
	}
	if echoPackets.Load() != 1 {
		t.Fatalf("requested UDP client port packets = %d", echoPackets.Load())
	}
}

func TestSOCKSUDPInitialSetupFailsOverWithinChannel(t *testing.T) {
	echoAddress, _, stopEcho := startUDPEchoServer(t)
	defer stopEcho()
	server := newTUNPoolTestServer(t)
	var attempts atomic.Int64
	server.dialUDP = func(
		ctx context.Context,
		dialer *net.Dialer,
		target string,
	) (net.Conn, error) {
		if attempts.Add(1) == 1 {
			return nil, context.DeadlineExceeded
		}
		return dialer.DialContext(ctx, "udp4", target)
	}
	endpoints, err := server.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer stopServer(t, server)
	control, relay := startUDPAssociation(
		t,
		endpoints.Channels[ChannelAggregation],
		0,
	)
	defer control.Close()
	client := listenUDPClient(t)
	defer client.Close()
	sendSOCKSUDP(t, client, relay, echoAddress, []byte("failover"))
	if reply := readSOCKSUDP(t, client); string(reply) != "failover" {
		t.Fatalf("failover reply = %q", reply)
	}
	if attempts.Load() != 2 {
		t.Fatalf("UDP setup attempts = %d, want 2", attempts.Load())
	}
	snapshot := server.Snapshot(true)
	for _, connection := range snapshot.Connections {
		if connection.Protocol == "socks5_udp" {
			if connection.Adapter != "wireless" {
				t.Fatalf("UDP failover adapter = %q, want wireless", connection.Adapter)
			}
			return
		}
	}
	t.Fatal("UDP failover flow missing from telemetry")
}

func TestSOCKSUDPFlowLimitAndIdleExpiry(t *testing.T) {
	firstAddress, _, stopFirst := startUDPEchoServer(t)
	defer stopFirst()
	secondAddress, _, stopSecond := startUDPEchoServer(t)
	defer stopSecond()
	server := newTUNPoolTestServer(t)
	server.udpFlowLimit = 1
	server.udpIdleTimeout = 80 * time.Millisecond
	server.udpSweepInterval = 20 * time.Millisecond
	var udpDials atomic.Int64
	server.dialUDP = func(
		ctx context.Context,
		dialer *net.Dialer,
		target string,
	) (net.Conn, error) {
		udpDials.Add(1)
		return dialer.DialContext(ctx, "udp4", target)
	}
	endpoints, err := server.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer stopServer(t, server)
	control, relay := startUDPAssociation(
		t,
		endpoints.Channels[ChannelAggregation],
		0,
	)
	defer control.Close()
	client := listenUDPClient(t)
	defer client.Close()

	sendSOCKSUDP(t, client, relay, firstAddress, []byte("first"))
	if reply := readSOCKSUDP(t, client); string(reply) != "first" {
		t.Fatalf("first reply = %q", reply)
	}
	sendSOCKSUDP(t, client, relay, secondAddress, []byte("blocked"))
	_ = client.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	if _, _, err := client.ReadFromUDP(make([]byte, 128)); err == nil {
		t.Fatal("flow beyond association limit received a reply")
	}
	if udpDials.Load() != 1 {
		t.Fatalf("flow limit allowed %d physical dials", udpDials.Load())
	}

	waitForUDPFlows(t, server, 0, time.Second)
	sendSOCKSUDP(t, client, relay, secondAddress, []byte("after-expiry"))
	if reply := readSOCKSUDP(t, client); string(reply) != "after-expiry" {
		t.Fatalf("post-expiry reply = %q", reply)
	}
	if udpDials.Load() != 2 {
		t.Fatalf("expired flow was not replaced, dials = %d", udpDials.Load())
	}
}

func TestSOCKSUDPStopClosesActiveAssociationWithinDeadline(t *testing.T) {
	echoAddress, _, stopEcho := startUDPEchoServer(t)
	defer stopEcho()
	server := newTUNPoolTestServer(t)
	endpoints, err := server.Start()
	if err != nil {
		t.Fatal(err)
	}
	control, relay := startUDPAssociation(
		t,
		endpoints.Channels[ChannelAggregation],
		0,
	)
	defer control.Close()
	client := listenUDPClient(t)
	defer client.Close()
	sendSOCKSUDP(t, client, relay, echoAddress, []byte("active"))
	if reply := readSOCKSUDP(t, client); string(reply) != "active" {
		t.Fatalf("active reply = %q", reply)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := server.Stop(ctx); err != nil {
		t.Fatalf("Stop() with active UDP association failed: %v", err)
	}
	_ = control.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	if _, err := control.Read(make([]byte, 1)); err == nil {
		t.Fatal("UDP control connection remained open after engine stop")
	}
}

func TestParseAndPackSOCKSUDPIPv4(t *testing.T) {
	payload := append(
		[]byte{0, 0, 0, 1, 192, 0, 2, 1, 1, 187},
		[]byte("payload")...,
	)
	packet, ok := parseSOCKSUDPPacket(payload)
	if !ok || packet.target != "192.0.2.1:443" || string(packet.payload) != "payload" {
		t.Fatalf("parsed packet = %#v, %v", packet, ok)
	}
	reply, ok := packSOCKSUDPReply(packet.target, packet.payload)
	if !ok || string(reply) != string(payload) {
		t.Fatalf("packed reply = %v, %v", reply, ok)
	}
}

func TestParseAndPackSOCKSUDPIPv6(t *testing.T) {
	address := net.ParseIP("2001:db8::1").To16()
	payload := append([]byte{0, 0, 0, 4}, address...)
	payload = binary.BigEndian.AppendUint16(payload, 443)
	payload = append(payload, []byte("payload")...)
	packet, ok := parseSOCKSUDPPacket(payload)
	if !ok || packet.target != "[2001:db8::1]:443" ||
		string(packet.payload) != "payload" {
		t.Fatalf("parsed IPv6 packet = %#v, %v", packet, ok)
	}
	reply, ok := packSOCKSUDPReply(packet.target, packet.payload)
	if !ok || string(reply) != string(payload) {
		t.Fatalf("packed IPv6 reply = %v, %v", reply, ok)
	}
}

func TestSOCKSUDPRelaysLiteralIPv6WithStableFlow(t *testing.T) {
	echoAddress, packets, stopEcho := startUDPEchoServerIPv6(t)
	defer stopEcho()
	server, err := New(Config{
		Adapters: []Adapter{{
			Name:       "loopback",
			SourceIP:   "127.0.0.1",
			SourceIPv6: "::1",
		}},
		Channels: []Channel{
			{Name: ChannelEthernet, AdapterNames: []string{"loopback"}},
			{Name: ChannelWiFi, AdapterNames: []string{"loopback"}},
			{Name: ChannelAggregation, AdapterNames: []string{"loopback"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var dials atomic.Int64
	server.dialUDP = func(
		ctx context.Context,
		dialer *net.Dialer,
		target string,
	) (net.Conn, error) {
		dials.Add(1)
		return dialer.DialContext(ctx, "udp6", target)
	}
	endpoints, err := server.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer stopServer(t, server)
	control, relay := startUDPAssociation(
		t,
		endpoints.Channels[ChannelAggregation],
		0,
	)
	defer control.Close()
	client := listenUDPClient(t)
	defer client.Close()

	for _, value := range []string{"ipv6-one", "ipv6-two"} {
		sendSOCKSUDP(t, client, relay, echoAddress, []byte(value))
		if reply := readSOCKSUDP(t, client); string(reply) != value {
			t.Fatalf("IPv6 UDP reply = %q, want %q", reply, value)
		}
	}
	if dials.Load() != 1 || packets.Load() != 2 {
		t.Fatalf("IPv6 UDP dials=%d packets=%d", dials.Load(), packets.Load())
	}
}

func newTUNPoolTestServer(t *testing.T) *Server {
	t.Helper()
	server, err := New(Config{
		Adapters: []Adapter{
			{Name: "wired", SourceIP: "127.0.0.1"},
			{Name: "wireless", SourceIP: "127.0.0.2"},
		},
		Channels: []Channel{
			{Name: ChannelEthernet, AdapterNames: []string{"wired"}},
			{Name: ChannelWiFi, AdapterNames: []string{"wireless"}},
			{
				Name:         ChannelAggregation,
				AdapterNames: []string{"wired", "wireless"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func startUDPEchoServer(t *testing.T) (string, *atomic.Int64, func()) {
	t.Helper()
	listener, err := net.ListenUDP("udp4", &net.UDPAddr{
		IP: net.ParseIP("127.0.0.1"),
	})
	if err != nil {
		t.Fatal(err)
	}
	var packets atomic.Int64
	done := make(chan struct{})
	go func() {
		buffer := make([]byte, 65535)
		for {
			count, client, err := listener.ReadFromUDP(buffer)
			if err != nil {
				close(done)
				return
			}
			packets.Add(1)
			_, _ = listener.WriteToUDP(buffer[:count], client)
		}
	}()
	return listener.LocalAddr().String(), &packets, func() {
		_ = listener.Close()
		<-done
	}
}

func startUDPEchoServerIPv6(t *testing.T) (string, *atomic.Int64, func()) {
	t.Helper()
	listener, err := net.ListenUDP("udp6", &net.UDPAddr{
		IP: net.ParseIP("::1"),
	})
	if err != nil {
		t.Skipf("IPv6 UDP loopback unavailable: %v", err)
	}
	var packets atomic.Int64
	done := make(chan struct{})
	go func() {
		buffer := make([]byte, 65535)
		for {
			count, client, err := listener.ReadFromUDP(buffer)
			if err != nil {
				close(done)
				return
			}
			packets.Add(1)
			_, _ = listener.WriteToUDP(buffer[:count], client)
		}
	}()
	return listener.LocalAddr().String(), &packets, func() {
		_ = listener.Close()
		<-done
	}
}

func startUDPAssociation(
	t *testing.T,
	endpoint string,
	requestedPort int,
) (net.Conn, *net.UDPAddr) {
	t.Helper()
	control, err := net.DialTimeout("tcp", endpoint, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_ = control.SetDeadline(time.Now().Add(3 * time.Second))
	_, _ = control.Write([]byte{5, 1, 0})
	greeting := make([]byte, 2)
	if _, err := io.ReadFull(control, greeting); err != nil || greeting[1] != 0 {
		_ = control.Close()
		t.Fatalf("SOCKS greeting = %v, %v", greeting, err)
	}
	request := []byte{5, 3, 0, 1, 0, 0, 0, 0}
	request = binary.BigEndian.AppendUint16(request, uint16(requestedPort))
	_, _ = control.Write(request)
	reply := make([]byte, 10)
	if _, err := io.ReadFull(control, reply); err != nil || reply[1] != 0 {
		_ = control.Close()
		t.Fatalf("UDP ASSOCIATE reply = %v, %v", reply, err)
	}
	_ = control.SetDeadline(time.Time{})
	return control, &net.UDPAddr{
		IP:   net.IP(reply[4:8]),
		Port: int(binary.BigEndian.Uint16(reply[8:10])),
	}
}

func listenUDPClient(t *testing.T) *net.UDPConn {
	t.Helper()
	connection, err := net.ListenUDP("udp4", &net.UDPAddr{
		IP: net.ParseIP("127.0.0.1"),
	})
	if err != nil {
		t.Fatal(err)
	}
	return connection
}

func sendSOCKSUDP(
	t *testing.T,
	client *net.UDPConn,
	relay *net.UDPAddr,
	target string,
	payload []byte,
) {
	t.Helper()
	packet, ok := packSOCKSUDPReply(target, payload)
	if !ok {
		t.Fatalf("could not encode SOCKS UDP target %q", target)
	}
	if _, err := client.WriteToUDP(packet, relay); err != nil {
		t.Fatal(err)
	}
}

func readSOCKSUDP(t *testing.T, client *net.UDPConn) []byte {
	t.Helper()
	_ = client.SetReadDeadline(time.Now().Add(time.Second))
	buffer := make([]byte, 65535)
	count, _, err := client.ReadFromUDP(buffer)
	if err != nil {
		t.Fatal(err)
	}
	packet, ok := parseSOCKSUDPPacket(buffer[:count])
	if !ok {
		t.Fatalf("invalid SOCKS UDP reply: %v", buffer[:count])
	}
	return packet.payload
}

func waitForUDPFlows(
	t *testing.T,
	server *Server,
	want int,
	timeout time.Duration,
) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		count := 0
		for _, connection := range server.Snapshot(true).Connections {
			if connection.Protocol == "socks5_udp" {
				count++
			}
		}
		if count == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("UDP flow count = %d, want %d", count, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func waitForUDPFlowTelemetry(
	t *testing.T,
	server *Server,
	wantUp uint64,
	wantDown uint64,
	timeout time.Duration,
) ConnectionSnapshot {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		snapshot := server.Snapshot(true)
		for _, connection := range snapshot.Connections {
			if connection.Protocol != "socks5_udp" {
				continue
			}
			if connection.BytesUp >= wantUp && connection.BytesDown >= wantDown {
				return connection
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf(
				"UDP telemetry did not reach up=%d down=%d: %#v",
				wantUp,
				wantDown,
				snapshot.Connections,
			)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
