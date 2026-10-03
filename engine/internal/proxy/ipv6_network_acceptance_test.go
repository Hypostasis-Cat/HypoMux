package proxy

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"net"
	"os"
	"testing"
	"time"

	"github.com/Hypostasis-Cat/HypoMux/engine/internal/dns"
)

// Opt-in physical-network checks. The runner treats missing prerequisites as
// blocked and never turns these skips into successful qualification evidence.
type ipv6AcceptanceConfig struct {
	Adapter        Adapter `json:"adapter"`
	NAT64TCP       string  `json:"nat64_tcp"`
	NAT64UDP       string  `json:"nat64_udp"`
	IPv4OnlyDomain string  `json:"ipv4_only_domain"`
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

func acceptanceServer(t *testing.T, cfg ipv6AcceptanceConfig) *Server {
	t.Helper()
	s, err := New(Config{Adapters: []Adapter{cfg.Adapter}, DNS: dns.Config{Policy: dns.PolicyGoogle}, Channels: []Channel{
		{Name: ChannelEthernet, AdapterNames: []string{cfg.Adapter.Name}}, {Name: ChannelWiFi, AdapterNames: []string{cfg.Adapter.Name}}, {Name: ChannelAggregation, AdapterNames: []string{cfg.Adapter.Name}},
	}})
	if err != nil {
		t.Fatal(err)
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
	answer, err := s.resolver.Resolve(ctx, dns.Query{Domain: "dns.google", RecordType: dns.RecordAAAA, Binding: adapterDNSBinding(cfg.Adapter)})
	if err != nil || net.ParseIP(answer.Address) == nil || net.ParseIP(answer.Address).To4() != nil || answer.Transport != "doh" {
		t.Fatalf("IPv6 DoH=%+v error=%v", answer, err)
	}
	acceptanceTLS(t, ctx, s, net.JoinHostPort(answer.Address, "443"), "dns.google", cfg)
}

func TestPublicNAT64NetworkAcceptance(t *testing.T) {
	cfg := physicalIPv6Config(t)
	if cfg.NAT64TCP == "" || cfg.NAT64UDP == "" || cfg.IPv4OnlyDomain == "" {
		t.Fatal("NAT64 acceptance needs IPv4 TCP/UDP targets and a controlled IPv4-only TLS domain")
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
	acceptanceTLS(t, ctx, s, cfg.NAT64TCP, "dns.google", cfg)
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
	// RFC 1035 query for dns.google AAAA, carried through an IPv4 SOCKS target.
	query := []byte{0x64, 0x06, 1, 0, 0, 1, 0, 0, 0, 0, 0, 0, 3, 'd', 'n', 's', 6, 'g', 'o', 'o', 'g', 'l', 'e', 0, 0, 28, 0, 1}
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
