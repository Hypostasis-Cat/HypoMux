package proxy

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"github.com/Hypostasis-Cat/HypoMux/engine/internal/dns"
)

// Opt-in physical-network checks. The runner treats missing prerequisites as
// blocked and never turns these skips into successful qualification evidence.
type ipv6AcceptanceConfig struct {
	Adapter         Adapter `json:"adapter"`
	DNSPolicy       string  `json:"dns_policy"`
	IPv6UDP         string  `json:"ipv6_udp"`
	IPv6UDPProtocol string  `json:"ipv6_udp_protocol"`
	NAT64TCP        string  `json:"nat64_tcp"`
	NAT64UDP        string  `json:"nat64_udp"`
	NAT64ServerName string  `json:"nat64_server_name"`
	IPv4OnlyDomain  string  `json:"ipv4_only_domain"`
}

func physicalIPv6Config(t *testing.T) ipv6AcceptanceConfig {
	t.Helper()
	raw := os.Getenv("HYPOMUX_IPV6_ACCEPTANCE_CONFIG")
	if raw == "" {
		t.Skip("run tools/ipv6-acceptance.ps1 on the explicit IPv6 test network")
	}
	var cfg ipv6AcceptanceConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.DNSPolicy == "" {
		cfg.DNSPolicy = dns.PolicyAliDNS
	}
	switch cfg.DNSPolicy {
	case dns.PolicyAliDNS, dns.PolicyDNSPod, dns.PolicyGoogle:
	default:
		t.Fatalf("unsupported acceptance DNS policy: %s", cfg.DNSPolicy)
	}
	if cfg.Adapter.SourceIP != "" || cfg.Adapter.SourceIPv6 == "" || cfg.Adapter.IPv6IfIndex <= 0 {
		t.Fatal("acceptance requires an explicit IPv6-only source binding")
	}
	iface, err := net.InterfaceByName(cfg.Adapter.Name)
	if err != nil {
		t.Fatal(err)
	}
	addresses, err := iface.Addrs()
	if err != nil {
		t.Fatal(err)
	}
	owned := false
	source := net.ParseIP(cfg.Adapter.SourceIPv6)
	for _, address := range addresses {
		ip, _, err := net.ParseCIDR(address.String())
		if err == nil && ip.Equal(source) {
			owned = true
		}
	}
	if !owned || source == nil || source.To4() != nil || source.IsLoopback() || source.IsLinkLocalUnicast() {
		t.Fatal("selected interface does not own the usable IPv6 source")
	}
	return cfg
}

func acceptanceServer(t *testing.T, cfg ipv6AcceptanceConfig, prepare ...func(*Server)) *Server {
	t.Helper()
	s, err := New(Config{Adapters: []Adapter{cfg.Adapter}, DNS: dns.Config{Policy: cfg.DNSPolicy}, Channels: []Channel{
		{Name: ChannelEthernet, AdapterNames: []string{cfg.Adapter.Name}}, {Name: ChannelWiFi, AdapterNames: []string{cfg.Adapter.Name}}, {Name: ChannelAggregation, AdapterNames: []string{cfg.Adapter.Name}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, configure := range prepare {
		configure(s)
	}
	if _, err = s.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stopServer(t, s) })
	return s
}

func acceptanceTLS(t *testing.T, ctx context.Context, s *Server, target, serverName string, cfg ipv6AcceptanceConfig) {
	t.Helper()
	connection, adapter, err := s.dialUpstream(ctx, target, s.scheduler, false, false)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	local, _, err := net.SplitHostPort(connection.LocalAddr().String())
	if err != nil || !net.ParseIP(local).Equal(net.ParseIP(cfg.Adapter.SourceIPv6)) || adapter.Name != cfg.Adapter.Name {
		t.Fatalf("connection escaped source binding: %v %s", connection.LocalAddr(), adapter.Name)
	}
	client := tls.Client(connection, &tls.Config{ServerName: serverName, MinVersion: tls.VersionTLS12})
	if err := client.HandshakeContext(ctx); err != nil {
		t.Fatal(err)
	}
	t.Logf("verified TLS peer=%s source=%s adapter=%s", connection.RemoteAddr(), connection.LocalAddr(), adapter.Name)
}

func TestPublicIPv6NetworkAcceptance(t *testing.T) {
	cfg := physicalIPv6Config(t)
	s := acceptanceServer(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	serverName := dns.Endpoints(cfg.DNSPolicy)[0].Host
	answer, err := s.resolver.Resolve(ctx, dns.Query{Domain: serverName, RecordType: dns.RecordAAAA, Binding: adapterDNSBinding(cfg.Adapter)})
	if err != nil || net.ParseIP(answer.Address) == nil || net.ParseIP(answer.Address).To4() != nil || answer.Transport != "doh" {
		t.Fatalf("IPv6 DoH=%+v error=%v", answer, err)
	}
	t.Logf("verified DoH policy=%s transport=%s server=%s", cfg.DNSPolicy, answer.Transport, answer.Server)
	acceptanceTLS(t, ctx, s, net.JoinHostPort(answer.Address, "443"), serverName, cfg)
	acceptanceTLS(t, ctx, s, net.JoinHostPort(serverName, "443"), serverName, cfg)
}

func TestPublicIPv6UDPNetworkAcceptance(t *testing.T) {
	cfg := physicalIPv6Config(t)
	if cfg.IPv6UDPProtocol == "" {
		cfg.IPv6UDPProtocol = "dns"
	}
	if cfg.IPv6UDPProtocol != "dns" && cfg.IPv6UDPProtocol != "ntp" {
		t.Fatal("IPv6 UDP acceptance protocol must be dns or ntp")
	}
	host, port, err := net.SplitHostPort(cfg.IPv6UDP)
	if err != nil || (cfg.IPv6UDPProtocol == "dns" && port != "53") || (cfg.IPv6UDPProtocol == "ntp" && port != "123") {
		t.Fatal("provide a DNS endpoint on port 53 or an NTP endpoint on port 123")
	}
	s := acceptanceServer(t, cfg, func(s *Server) {
		dialUDP := s.dialUDP
		s.dialUDP = func(ctx context.Context, dialer *net.Dialer, target string) (net.Conn, error) {
			connection, err := dialUDP(ctx, dialer, target)
			if err == nil {
				local, _, splitErr := net.SplitHostPort(connection.LocalAddr().String())
				if splitErr != nil || !net.ParseIP(local).Equal(net.ParseIP(cfg.Adapter.SourceIPv6)) {
					connection.Close()
					return nil, fmt.Errorf("UDP escaped selected IPv6 source: %s", local)
				}
				t.Logf("UDP target=%s source=%s", target, connection.LocalAddr())
			}
			return connection, err
		}
	})
	if net.ParseIP(host) == nil && cfg.IPv6UDPProtocol == "ntp" {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		answer, err := s.resolver.Resolve(ctx, dns.Query{Domain: host, RecordType: dns.RecordAAAA, Binding: adapterDNSBinding(cfg.Adapter)})
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("resolved NTP IPv6 endpoint domain=%s address=%s DNS=%s", host, answer.Address, answer.Server)
		host = answer.Address
	}
	ip := net.ParseIP(host)
	if ip == nil || ip.To4() != nil {
		t.Fatal("IPv6 UDP acceptance requires a real IPv6 destination")
	}
	cfg.IPv6UDP = net.JoinHostPort(ip.String(), port)
	control, relay := startUDPAssociation(t, s.endpoints.Channels[ChannelAggregation], 0)
	defer control.Close()
	client := listenUDPClient(t)
	defer client.Close()
	query := []byte{0x64, 0x06, 1, 0, 0, 1, 0, 0, 0, 0, 0, 0, 3, 'd', 'n', 's', 6, 'a', 'l', 'i', 'd', 'n', 's', 3, 'c', 'o', 'm', 0, 0, 28, 0, 1}
	if cfg.IPv6UDPProtocol == "ntp" {
		query = make([]byte, 48)
		query[0] = 0x23 // NTP v4 client request; this test never adjusts the clock.
		now := time.Now()
		binary.BigEndian.PutUint32(query[40:], uint32(now.Unix()+2208988800))
		binary.BigEndian.PutUint32(query[44:], uint32((uint64(now.Nanosecond())<<32)/1e9))
	}
	sendSOCKSUDP(t, client, relay, cfg.IPv6UDP, query)
	client.SetReadDeadline(time.Now().Add(8 * time.Second))
	buffer := make([]byte, 4096)
	n, _, err := client.ReadFromUDP(buffer)
	if err != nil {
		t.Fatal(err)
	}
	packet, ok := parseSOCKSUDPPacket(buffer[:n])
	if !ok || packet.target != cfg.IPv6UDP {
		t.Fatalf("IPv6 UDP reply identity invalid: %+v", packet)
	}
	if cfg.IPv6UDPProtocol == "ntp" {
		p := packet.payload
		if len(p) < 48 || p[0]&7 != 4 || p[0]>>6 == 3 || p[1] == 0 || p[1] > 15 || !bytes.Equal(p[24:32], query[40:48]) || bytes.Equal(p[40:48], make([]byte, 8)) {
			t.Fatalf("IPv6 UDP NTP reply or request correlation invalid: %x", p)
		}
	} else if len(packet.payload) < 12 || binary.BigEndian.Uint16(packet.payload) != 0x6406 || packet.payload[2]&0x80 == 0 || packet.payload[3]&0xf != 0 || binary.BigEndian.Uint16(packet.payload[6:]) == 0 {
		t.Fatalf("IPv6 UDP DNS reply invalid: %+v", packet)
	}
	t.Logf("verified public IPv6 UDP %s reply bytes=%d target=%s", cfg.IPv6UDPProtocol, len(packet.payload), packet.target)
}

func TestPublicNAT64NetworkAcceptance(t *testing.T) {
	cfg := physicalIPv6Config(t)
	if cfg.NAT64TCP == "" || cfg.NAT64UDP == "" || cfg.NAT64ServerName == "" || cfg.IPv4OnlyDomain == "" {
		t.Fatal("NAT64 acceptance needs IPv4 TCP/UDP targets, their TLS hostname and a controlled IPv4-only TLS domain")
	}
	for _, target := range []string{cfg.NAT64TCP, cfg.NAT64UDP} {
		host, _, err := net.SplitHostPort(target)
		if err != nil || net.ParseIP(host).To4() == nil {
			t.Fatalf("not a literal IPv4 target: %s", target)
		}
	}
	s := acceptanceServer(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	answer, err := s.resolver.Resolve(ctx, dns.Query{Domain: "ipv4only.arpa", RecordType: dns.RecordAAAA, Binding: adapterDNSBinding(cfg.Adapter), NetworkDNS: true})
	if err != nil {
		t.Fatal(err)
	}
	prefix, err := dns.DiscoverNAT64Prefix(answer.Addresses)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("network-advertised prefix=%s DNS=%s", prefix, answer.Server)
	acceptanceTLS(t, ctx, s, cfg.NAT64TCP, cfg.NAT64ServerName, cfg)
	domainBinding := adapterDNSBinding(cfg.Adapter)
	if _, err := s.resolver.Resolve(ctx, dns.Query{Domain: cfg.IPv4OnlyDomain, RecordType: dns.RecordA, Binding: domainBinding}); err != nil {
		t.Fatal(err)
	}
	if aaaa, err := s.resolver.Resolve(ctx, dns.Query{Domain: cfg.IPv4OnlyDomain, RecordType: dns.RecordAAAA, Binding: domainBinding}); err == nil && len(aaaa.Addresses) > 0 {
		t.Fatal("IPv4-only domain unexpectedly has AAAA records")
	}
	acceptanceTLS(t, ctx, s, net.JoinHostPort(cfg.IPv4OnlyDomain, "443"), cfg.IPv4OnlyDomain, cfg)
	control, relay := startUDPAssociation(t, s.endpoints.Channels[ChannelAggregation], 0)
	defer control.Close()
	client := listenUDPClient(t)
	defer client.Close()
	// RFC 1035 query for the domestic test endpoint, through an IPv4 target.
	query := []byte{0x64, 0x06, 1, 0, 0, 1, 0, 0, 0, 0, 0, 0, 3, 'd', 'n', 's', 6, 'a', 'l', 'i', 'd', 'n', 's', 3, 'c', 'o', 'm', 0, 0, 28, 0, 1}
	sendSOCKSUDP(t, client, relay, cfg.NAT64UDP, query)
	client.SetReadDeadline(time.Now().Add(8 * time.Second))
	buffer := make([]byte, 4096)
	n, _, err := client.ReadFromUDP(buffer)
	if err != nil {
		t.Fatal(err)
	}
	packet, ok := parseSOCKSUDPPacket(buffer[:n])
	if !ok || packet.target != cfg.NAT64UDP || len(packet.payload) < 12 || binary.BigEndian.Uint16(packet.payload) != 0x6406 || packet.payload[2]&0x80 == 0 || packet.payload[3]&0xf != 0 || binary.BigEndian.Uint16(packet.payload[6:]) == 0 {
		t.Fatalf("NAT64 UDP DNS or original reply identity invalid: %+v", packet)
	}
}
