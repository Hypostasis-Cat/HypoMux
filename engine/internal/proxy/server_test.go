package proxy

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Hypostasis-Cat/HypoMux/engine/internal/dns"
)

func TestSOCKSAndHTTPConnectRelayAndTelemetry(t *testing.T) {
	echoAddress, stopEcho := startEchoServer(t)
	defer stopEcho()

	server, err := New(Config{
		SOCKSPort: 0,
		HTTPPort:  0,
		Adapters: []Adapter{{
			Name:     "loopback",
			SourceIP: "127.0.0.1",
		}},
	})
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	endpoints, err := server.Start()
	if err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer stopServer(t, server)

	host, portText, _ := net.SplitHostPort(echoAddress)
	port, _ := strconv.Atoi(portText)
	socksClient, err := net.DialTimeout("tcp", endpoints.SOCKS, time.Second)
	if err != nil {
		t.Fatalf("dial SOCKS: %v", err)
	}
	_ = socksClient.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := socksClient.Write([]byte{5, 1, 0}); err != nil {
		t.Fatal(err)
	}
	greeting := make([]byte, 2)
	if _, err := io.ReadFull(socksClient, greeting); err != nil {
		t.Fatal(err)
	}
	ip := net.ParseIP(host).To4()
	request := append([]byte{5, 1, 0, 1}, ip...)
	request = binary.BigEndian.AppendUint16(request, uint16(port))
	if _, err := socksClient.Write(request); err != nil {
		t.Fatal(err)
	}
	reply := make([]byte, 10)
	if _, err := io.ReadFull(socksClient, reply); err != nil || reply[1] != 0 {
		t.Fatalf("SOCKS reply = %v, %v", reply, err)
	}
	assertEcho(t, socksClient, []byte("socks-data"))
	_ = socksClient.Close()

	httpClient, err := net.DialTimeout("tcp", endpoints.HTTP, time.Second)
	if err != nil {
		t.Fatalf("dial HTTP: %v", err)
	}
	_ = httpClient.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := io.WriteString(httpClient, "CONNECT "+echoAddress+" HTTP/1.1\r\nHost: "+echoAddress+"\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	responseReader := bufio.NewReader(httpClient)
	response, err := responseReader.ReadString('\n')
	if err != nil || response != "HTTP/1.1 200 Connection Established\r\n" {
		t.Fatalf("HTTP CONNECT response = %q, %v", response, err)
	}
	// Consume the two remaining response header lines.
	_, _ = responseReader.ReadString('\n')
	_, _ = responseReader.ReadString('\n')
	assertEcho(t, httpClient, []byte("http-data"))
	_ = httpClient.Close()

	deadline := time.Now().Add(time.Second)
	for {
		snapshot := server.Snapshot(false)
		if snapshot.Total.BytesUp >= uint64(len("socks-data")+len("http-data")) &&
			snapshot.Total.BytesDown >= uint64(len("socks-data")+len("http-data")) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("telemetry did not update: %#v", snapshot)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestStopClosesHandshakeOnlyClients(t *testing.T) {
	server, err := New(Config{
		SOCKSPort: 0,
		HTTPPort:  0,
		Adapters: []Adapter{{
			Name:     "loopback",
			SourceIP: "127.0.0.1",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	endpoints, err := server.Start()
	if err != nil {
		t.Fatal(err)
	}
	client, err := net.DialTimeout("tcp", endpoints.SOCKS, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := server.Stop(ctx); err != nil {
		t.Fatalf("Stop() failed: %v", err)
	}
	_ = client.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := client.Read(make([]byte, 1)); err == nil {
		t.Fatal("client remained open after stop")
	}
}

func TestOrdinaryProxyStillRejectsUDPAssociate(t *testing.T) {
	server, err := New(Config{
		SOCKSPort: 0,
		HTTPPort:  0,
		Adapters: []Adapter{{
			Name:     "loopback",
			SourceIP: "127.0.0.1",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	endpoints, err := server.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer stopServer(t, server)
	client, err := net.DialTimeout("tcp", endpoints.SOCKS, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	_ = client.SetDeadline(time.Now().Add(time.Second))
	_, _ = client.Write([]byte{5, 1, 0})
	if _, err := io.ReadFull(client, make([]byte, 2)); err != nil {
		t.Fatal(err)
	}
	_, _ = client.Write([]byte{5, 3, 0, 1, 0, 0, 0, 0, 0, 0})
	reply := make([]byte, 10)
	if _, err := io.ReadFull(client, reply); err != nil {
		t.Fatal(err)
	}
	if reply[1] != 7 {
		t.Fatalf("ordinary proxy UDP reply = %d, want command unsupported", reply[1])
	}
}

func TestTUNTCPPoolRelaysWithinChannelAndReportsTelemetry(t *testing.T) {
	echoAddress, stopEcho := startEchoServer(t)
	defer stopEcho()
	server, err := New(Config{
		Adapters: []Adapter{
			{Name: "wired", SourceIP: "127.0.0.1"},
			{Name: "wireless", SourceIP: "127.0.0.2"},
		},
		Channels: []Channel{
			{Name: "nic_ethernet", AdapterNames: []string{"wired"}},
			{Name: "nic_wifi", AdapterNames: []string{"wireless"}},
			{
				Name:         "aggregation",
				AdapterNames: []string{"wired", "wireless"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	endpoints, err := server.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer stopServer(t, server)
	if len(endpoints.Channels) != 3 {
		t.Fatalf("channel endpoints = %#v", endpoints.Channels)
	}

	client := dialSOCKSIPv4(t, endpoints.Channels["nic_ethernet"], echoAddress, 1)
	defer client.Close()
	assertEcho(t, client, []byte("tun-channel"))

	snapshot := server.Snapshot(true)
	if len(snapshot.Connections) != 1 {
		t.Fatalf("connections = %#v", snapshot.Connections)
	}
	connection := snapshot.Connections[0]
	if connection.Channel != "nic_ethernet" || connection.Adapter != "wired" {
		t.Fatalf("channel selection escaped subset: %#v", connection)
	}
}

func TestTUNTCPPoolPreferredPortCollisionFallsBack(t *testing.T) {
	occupied, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	_, portText, _ := net.SplitHostPort(occupied.Addr().String())
	port, _ := strconv.Atoi(portText)

	server, err := New(Config{
		Adapters: []Adapter{{Name: "loopback", SourceIP: "127.0.0.1"}},
		Channels: []Channel{
			{Name: ChannelEthernet, AdapterNames: []string{"loopback"}},
			{Name: ChannelWiFi, AdapterNames: []string{"loopback"}},
			{
				Name:         ChannelAggregation,
				Port:         port,
				AdapterNames: []string{"loopback"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	endpoints, err := server.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer stopServer(t, server)
	if endpoints.Channels["aggregation"] == occupied.Addr().String() {
		t.Fatalf("occupied preferred endpoint was reused: %s", occupied.Addr())
	}
}

func TestTUNTCPPoolStartupRollsBackEarlierListeners(t *testing.T) {
	server, err := New(Config{
		Adapters: []Adapter{{Name: "loopback", SourceIP: "127.0.0.1"}},
		Channels: []Channel{
			{Name: ChannelEthernet, AdapterNames: []string{"loopback"}},
			{Name: ChannelWiFi, Port: 2002, AdapterNames: []string{"loopback"}},
			{Name: ChannelAggregation, Port: 2003, AdapterNames: []string{"loopback"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var firstEndpoint string
	attempts := 0
	server.listenTCP = func(network string, address string) (net.Listener, error) {
		attempts++
		if attempts > 1 {
			return nil, errors.New("injected listener failure")
		}
		listener, err := net.Listen(network, address)
		if err == nil {
			firstEndpoint = listener.Addr().String()
		}
		return listener, err
	}
	if _, err := server.Start(); err == nil {
		t.Fatal("partial channel startup unexpectedly succeeded")
	}
	if firstEndpoint == "" {
		t.Fatal("test did not create the first listener")
	}
	connection, dialErr := net.DialTimeout("tcp", firstEndpoint, 200*time.Millisecond)
	if dialErr == nil {
		_ = connection.Close()
		t.Fatalf("rolled-back listener remained reachable at %s", firstEndpoint)
	}
	if server.Running() {
		t.Fatal("server reports running after startup rollback")
	}
}

func TestTUNTCPPoolRejectsDomainAndIPv6WithoutBoundSource(t *testing.T) {
	server, err := New(Config{
		Adapters: []Adapter{{Name: "loopback", SourceIP: "127.0.0.1"}},
		Channels: []Channel{
			{Name: ChannelEthernet, AdapterNames: []string{"loopback"}},
			{Name: ChannelWiFi, AdapterNames: []string{"loopback"}},
			{Name: ChannelAggregation, AdapterNames: []string{"loopback"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	endpoints, err := server.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer stopServer(t, server)
	endpoint := endpoints.Channels["aggregation"]

	tests := []struct {
		name        string
		command     byte
		addressType byte
		address     []byte
		wantReply   byte
	}{
		{
			name:        "domain",
			command:     1,
			addressType: 3,
			address:     append([]byte{byte(len("example.com"))}, []byte("example.com")...),
			wantReply:   5,
		},
		{
			name:        "IPv6",
			command:     1,
			addressType: 4,
			address:     net.ParseIP("2001:db8::1").To16(),
			wantReply:   5,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, err := net.DialTimeout("tcp", endpoint, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			_ = client.SetDeadline(time.Now().Add(2 * time.Second))
			_, _ = client.Write([]byte{5, 1, 0})
			if _, err := io.ReadFull(client, make([]byte, 2)); err != nil {
				t.Fatal(err)
			}
			request := []byte{5, test.command, 0, test.addressType}
			request = append(request, test.address...)
			request = binary.BigEndian.AppendUint16(request, 443)
			_, _ = client.Write(request)
			reply := make([]byte, 10)
			if _, err := io.ReadFull(client, reply); err != nil {
				t.Fatal(err)
			}
			if reply[1] != test.wantReply {
				t.Fatalf("reply = %d, want %d", reply[1], test.wantReply)
			}
		})
	}
}

func TestTUNTCPPoolRelaysLiteralIPv6WithBoundSource(t *testing.T) {
	echoAddress, stopEcho := startEchoServerIPv6(t)
	defer stopEcho()
	server, err := New(Config{
		Adapters: []Adapter{
			{Name: "ipv4-only", SourceIP: "127.0.0.1"},
			{
				Name:       "dual-stack",
				SourceIP:   "127.0.0.2",
				SourceIPv6: "::1",
			},
		},
		Channels: []Channel{
			{Name: ChannelEthernet, AdapterNames: []string{"ipv4-only"}},
			{Name: ChannelWiFi, AdapterNames: []string{"dual-stack"}},
			{
				Name:         ChannelAggregation,
				AdapterNames: []string{"ipv4-only", "dual-stack"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	endpoints, err := server.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer stopServer(t, server)

	client := dialSOCKSIPv6(
		t,
		endpoints.Channels[ChannelAggregation],
		echoAddress,
	)
	defer client.Close()
	assertEcho(t, client, []byte("tun-ipv6"))
	snapshot := server.Snapshot(true)
	if len(snapshot.Connections) != 1 ||
		snapshot.Connections[0].Adapter != "dual-stack" {
		t.Fatalf("IPv6 connection telemetry = %#v", snapshot.Connections)
	}
	if snapshot.Adapters[1].SourceIPv6 != "::1" {
		t.Fatalf("IPv6 adapter telemetry = %#v", snapshot.Adapters[1])
	}
	health, _ := server.health.snapshot()
	if health["ipv4-only"].Failures != 0 {
		t.Fatal("IPv6-incompatible adapter was incorrectly marked unhealthy")
	}
}

func TestSOCKSDomainIsResolvedBeforeBoundTCPDial(t *testing.T) {
	echoAddress, stopEcho := startEchoServer(t)
	defer stopEcho()
	server, err := New(Config{
		SOCKSPort: 0,
		HTTPPort:  0,
		DNS: dns.Config{
			Policy:        dns.PolicyOff,
			LegacyServers: []string{"192.0.2.53"},
		},
		Adapters: []Adapter{{
			Name:     "loopback",
			SourceIP: "127.0.0.1",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	endpoints, err := server.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer stopServer(t, server)

	resolver, err := dns.New(server.ctx, server.config.DNS, func(
		_ context.Context,
		network string,
		_ string,
		_ dns.Binding,
	) (net.Conn, error) {
		client, dnsServer := net.Pipe()
		go answerDNSAOnce(dnsServer, network, net.ParseIP("127.0.0.1").To4())
		return client, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	server.resolver = resolver
	var dialTarget string
	server.dialTCP = func(
		ctx context.Context,
		_ *net.Dialer,
		target string,
	) (net.Conn, error) {
		dialTarget = target
		var dialer net.Dialer
		return dialer.DialContext(ctx, "tcp4", target)
	}

	_, portText, _ := net.SplitHostPort(echoAddress)
	port, _ := strconv.Atoi(portText)
	client, err := net.DialTimeout("tcp", endpoints.SOCKS, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	_ = client.SetDeadline(time.Now().Add(3 * time.Second))
	_, _ = client.Write([]byte{5, 1, 0})
	if _, err := io.ReadFull(client, make([]byte, 2)); err != nil {
		t.Fatal(err)
	}
	domain := []byte("resolved.example")
	request := append([]byte{5, 1, 0, 3, byte(len(domain))}, domain...)
	request = binary.BigEndian.AppendUint16(request, uint16(port))
	_, _ = client.Write(request)
	reply := make([]byte, 10)
	if _, err := io.ReadFull(client, reply); err != nil || reply[1] != 0 {
		t.Fatalf("SOCKS reply = %v, %v", reply, err)
	}
	if dialTarget != echoAddress {
		t.Fatalf("bound TCP target = %q, want literal %q", dialTarget, echoAddress)
	}
	assertEcho(t, client, []byte("domain-data"))
}

func TestHTTPForwardProxyRewritesAbsoluteRequestTarget(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/through-proxy" || request.URL.RawQuery != "value=1" {
			t.Errorf("origin request URL = %s", request.URL.String())
		}
		_, _ = writer.Write([]byte("origin-response"))
	}))
	defer origin.Close()

	server, err := New(Config{
		SOCKSPort: 0,
		HTTPPort:  0,
		Adapters: []Adapter{{
			Name:     "loopback",
			SourceIP: "127.0.0.1",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	endpoints, err := server.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer stopServer(t, server)

	client, err := net.DialTimeout("tcp", endpoints.HTTP, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	_ = client.SetDeadline(time.Now().Add(3 * time.Second))
	_, _ = io.WriteString(
		client,
		"GET "+origin.URL+"/through-proxy?value=1 HTTP/1.1\r\n"+
			"Host: "+strings.TrimPrefix(origin.URL, "http://")+"\r\n"+
			"Proxy-Connection: close\r\nConnection: close\r\n\r\n",
	)
	response, err := io.ReadAll(client)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(response), "origin-response") {
		t.Fatalf("origin response missing: %q", response)
	}
	if strings.Contains(strings.ToLower(string(response)), "proxy-connection") {
		t.Fatalf("proxy header leaked into response: %q", response)
	}
}

func startEchoServer(t *testing.T) (string, func()) {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer connection.Close()
				buffer := make([]byte, 128*1024)
				for {
					read, err := connection.Read(buffer)
					if read > 0 {
						_, _ = connection.Write(buffer[:read])
					}
					if err != nil {
						return
					}
					select {
					case <-ctx.Done():
						return
					default:
					}
				}
			}()
		}
	}()
	return listener.Addr().String(), func() {
		cancel()
		_ = listener.Close()
	}
}

func assertEcho(t *testing.T, connection net.Conn, payload []byte) {
	t.Helper()
	if _, err := connection.Write(payload); err != nil {
		t.Fatal(err)
	}
	reply := make([]byte, len(payload))
	if _, err := io.ReadFull(connection, reply); err != nil {
		t.Fatal(err)
	}
	if string(reply) != string(payload) {
		t.Fatalf("echo = %q, want %q", reply, payload)
	}
}

func dialSOCKSIPv4(
	t *testing.T,
	endpoint string,
	target string,
	command byte,
) net.Conn {
	t.Helper()
	host, portText, err := net.SplitHostPort(target)
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(portText)
	client, err := net.DialTimeout("tcp", endpoint, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_ = client.SetDeadline(time.Now().Add(3 * time.Second))
	_, _ = client.Write([]byte{5, 1, 0})
	if _, err := io.ReadFull(client, make([]byte, 2)); err != nil {
		_ = client.Close()
		t.Fatal(err)
	}
	request := append([]byte{5, command, 0, 1}, net.ParseIP(host).To4()...)
	request = binary.BigEndian.AppendUint16(request, uint16(port))
	_, _ = client.Write(request)
	reply := make([]byte, 10)
	if _, err := io.ReadFull(client, reply); err != nil || reply[1] != 0 {
		_ = client.Close()
		t.Fatalf("SOCKS reply = %v, %v", reply, err)
	}
	return client
}

func dialSOCKSIPv6(t *testing.T, endpoint string, target string) net.Conn {
	t.Helper()
	host, portText, err := net.SplitHostPort(target)
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(portText)
	client, err := net.DialTimeout("tcp", endpoint, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_ = client.SetDeadline(time.Now().Add(3 * time.Second))
	_, _ = client.Write([]byte{5, 1, 0})
	if _, err := io.ReadFull(client, make([]byte, 2)); err != nil {
		_ = client.Close()
		t.Fatal(err)
	}
	request := append([]byte{5, 1, 0, 4}, net.ParseIP(host).To16()...)
	request = binary.BigEndian.AppendUint16(request, uint16(port))
	_, _ = client.Write(request)
	reply := make([]byte, 10)
	if _, err := io.ReadFull(client, reply); err != nil || reply[1] != 0 {
		_ = client.Close()
		t.Fatalf("SOCKS IPv6 reply = %v, %v", reply, err)
	}
	return client
}

func startEchoServerIPv6(t *testing.T) (string, func()) {
	t.Helper()
	listener, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Skipf("IPv6 loopback unavailable: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer connection.Close()
				_, _ = io.Copy(connection, connection)
			}()
		}
	}()
	return listener.Addr().String(), func() {
		_ = listener.Close()
		<-done
	}
}

func stopServer(t *testing.T, server *Server) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := server.Stop(ctx); err != nil {
		t.Fatalf("Stop() failed: %v", err)
	}
}

func answerDNSAOnce(connection net.Conn, network string, address net.IP) {
	defer connection.Close()
	var query []byte
	if network == "tcp4" {
		var length uint16
		if binary.Read(connection, binary.BigEndian, &length) != nil {
			return
		}
		query = make([]byte, int(length))
		if _, err := io.ReadFull(connection, query); err != nil {
			return
		}
	} else {
		buffer := make([]byte, 4096)
		count, err := connection.Read(buffer)
		if err != nil {
			return
		}
		query = append([]byte(nil), buffer[:count]...)
	}
	if len(query) < 12 || address == nil {
		return
	}
	response := make([]byte, 12, len(query)+16)
	copy(response[:2], query[:2])
	binary.BigEndian.PutUint16(response[2:4], 0x8180)
	binary.BigEndian.PutUint16(response[4:6], 1)
	binary.BigEndian.PutUint16(response[6:8], 1)
	response = append(response, query[12:]...)
	response = append(response, 0xc0, 0x0c)
	response = binary.BigEndian.AppendUint16(response, 1)
	response = binary.BigEndian.AppendUint16(response, 1)
	response = binary.BigEndian.AppendUint32(response, 60)
	response = binary.BigEndian.AppendUint16(response, 4)
	response = append(response, address...)
	if network == "tcp4" {
		_ = binary.Write(connection, binary.BigEndian, uint16(len(response)))
	}
	_, _ = connection.Write(response)
}
