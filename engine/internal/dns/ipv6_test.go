package dns

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"
)

func TestIPv6OnlyBindingAndScopedDNS(t *testing.T) {
	b, err := NormalizeBinding(Binding{Name: "v6", SourceIPv6: "2001:db8::1", IPv6IfIndex: 7, DNSServers: []string{"fe80::1", "2001:db8::53"}})
	if err != nil || b.DNSServers[0] != "fe80::1%7" {
		t.Fatalf("binding=%+v error=%v", b, err)
	}
	if _, err = NormalizeBinding(Binding{Name: "v6", SourceIPv6: "2001:db8::1", IPv6IfIndex: 7, DNSServers: []string{"fe80::1%8"}}); err == nil {
		t.Fatal("accepted DNS on another interface")
	}
	if _, err = NormalizeBinding(Binding{Name: "empty"}); err == nil {
		t.Fatal("accepted an unbound DNS adapter")
	}
	for _, server := range LegacyServers(DefaultConfig(), b) {
		if endpointNetwork("udp", server) != "udp6" {
			t.Fatalf("IPv6-only selected %s", server)
		}
	}
	if supportsEndpoint(b, "fe80::1%8") || supportsEndpoint(b, "fe80::1") {
		t.Fatal("global fallback bypassed selected IPv6 scope")
	}
}

func TestNAT64RFC6052And7050(t *testing.T) {
	for _, vector := range []struct{ prefix, expected string }{
		{"2001:db8::/32", "2001:db8:c000:221::"},
		{"2001:db8:1200::/40", "2001:db8:12c0:2:21::"},
		{"2001:db8:1234::/48", "2001:db8:1234:c000:2:2100::"},
		{"2001:db8:1234:5600::/56", "2001:db8:1234:56c0:0:221::"},
		{"2001:db8:1234:5678::/64", "2001:db8:1234:5678:c0:2:2100:0"},
		{"2001:db8:1234:5678::/96", "2001:db8:1234:5678::c000:221"},
	} {
		got, err := SynthesizeNAT64(netip.MustParsePrefix(vector.prefix), netip.MustParseAddr("192.0.2.33"))
		if err != nil || got != netip.MustParseAddr(vector.expected) {
			t.Fatalf("%s produced %s, want %s: %v", vector.prefix, got, vector.expected, err)
		}
	}
	for _, bits := range []int{32, 40, 48, 56, 64, 96} {
		t.Run(fmt.Sprint(bits), func(t *testing.T) {
			prefix := netip.PrefixFrom(netip.MustParseAddr("2001:db8:1234:5678::"), bits).Masked()
			answer, err := SynthesizeNAT64(prefix, netip.MustParseAddr("192.0.0.170"))
			if err != nil {
				t.Fatal(err)
			}
			got, err := DiscoverNAT64Prefix([]string{answer.String()})
			if err != nil || got != prefix {
				t.Fatalf("got %s, want %s: %v", got, prefix, err)
			}
			translated, err := SynthesizeNAT64(prefix, netip.MustParseAddr("192.0.2.33"))
			if err != nil || !prefix.Contains(translated) {
				t.Fatalf("translation %s: %v", translated, err)
			}
		})
	}
	got, err := SynthesizeNAT64(netip.MustParsePrefix("64:ff9b::/96"), netip.MustParseAddr("192.0.2.33"))
	if err != nil || got.String() != "64:ff9b::c000:221" {
		t.Fatalf("RFC example=%s: %v", got, err)
	}
	if _, err := DiscoverNAT64Prefix([]string{"2001:db8::1", "::ffff:192.0.0.170"}); err == nil {
		t.Fatal("guessed a prefix without a DNS64 answer")
	}
}

func TestIPv6DNSCacheIsolatesAddressInterfaceServersAndNAT64(t *testing.T) {
	var calls atomic.Int64
	r, err := New(context.Background(), Config{Policy: PolicyOff}, answeringDialer(t, &calls, "192.0.2.44", 60, 0))
	if err != nil {
		t.Fatal(err)
	}
	b := Binding{Name: "v6", SourceIPv6: "2001:db8::1", IPv6IfIndex: 7, DNSServers: []string{"2001:db8::53"}}
	query := Query{Domain: "cache.example", RecordType: RecordA, Binding: b}
	if _, err := r.Resolve(context.Background(), query); err != nil {
		t.Fatal(err)
	}
	if result, err := r.Resolve(context.Background(), query); err != nil || !result.Cached {
		t.Fatal("same binding was not cached", err)
	}
	for _, variant := range []Query{
		{Domain: query.Domain, Binding: Binding{Name: b.Name, SourceIPv6: "2001:db8::2", IPv6IfIndex: 7, DNSServers: b.DNSServers}},
		{Domain: query.Domain, Binding: Binding{Name: b.Name, SourceIPv6: b.SourceIPv6, IPv6IfIndex: 8, DNSServers: b.DNSServers}},
		{Domain: query.Domain, Binding: Binding{Name: b.Name, SourceIPv6: b.SourceIPv6, IPv6IfIndex: 7, DNSServers: []string{"2001:db8::54"}}},
		{Domain: query.Domain, Binding: b, NetworkDNS: true},
	} {
		if result, err := r.Resolve(context.Background(), variant); err != nil || result.Cached {
			t.Fatalf("stale binding cache=%+v err=%v", result, err)
		}
	}
	if calls.Load() != 5 {
		t.Fatalf("dials=%d, want 5", calls.Load())
	}
	if _, err := r.Resolve(context.Background(), Query{Domain: "ipv4only.arpa", RecordType: RecordAAAA, Binding: Binding{Name: "v6", SourceIPv6: b.SourceIPv6}, NetworkDNS: true}); err == nil {
		t.Fatal("NAT64 used public fallback without network DNS")
	}
}

func TestIPv6DNSLastWaiterCancellationClosesLookupSocket(t *testing.T) {
	closed := make(chan error, 1)
	r, err := New(context.Background(), Config{Policy: PolicyOff, QueryTimeout: 5 * time.Second}, func(ctx context.Context, network, address string, b Binding) (net.Conn, error) {
		client, peer := net.Pipe()
		go func() {
			defer peer.Close()
			packet := make([]byte, 4096)
			if _, err := peer.Read(packet); err != nil {
				closed <- err
				return
			}
			_, err := peer.Read(packet)
			closed <- err
		}()
		return client, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := r.Resolve(ctx, Query{Domain: "cancel.example", Binding: Binding{Name: "v6", SourceIPv6: "::1"}})
		done <- err
	}()
	deadline := time.Now().Add(time.Second)
	for r.Status().Inflight == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	select {
	case err := <-closed:
		if !errors.Is(err, io.EOF) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("DNS socket outlived its last waiter")
	}
}

func TestIPv6DNSRealUDPAndTCPFallback(t *testing.T) {
	for _, tcpFallback := range []bool{false, true} {
		t.Run(fmt.Sprintf("tcp=%t", tcpFallback), func(t *testing.T) {
			listener, err := net.Listen("tcp6", "[::1]:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			udp, err := net.ListenUDP("udp6", &net.UDPAddr{IP: net.IPv6loopback})
			if err != nil {
				t.Fatal(err)
			}
			defer udp.Close()
			go func() {
				packet := make([]byte, 4096)
				count, peer, err := udp.ReadFromUDP(packet)
				if err != nil {
					return
				}
				if !peer.IP.Equal(net.IPv6loopback) {
					t.Error("UDP lost IPv6 source")
				}
				if tcpFallback {
					packet[2] |= 2
					udp.WriteToUDP(packet[:count], peer)
					return
				}
				answer, err := answerForQueryRaw(packet[:count], dnsTypeAAAA, "2001:db8::44", 60)
				if err != nil {
					t.Error(err)
					return
				}
				udp.WriteToUDP(answer, peer)
			}()
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(2 * time.Second))
				if !conn.RemoteAddr().(*net.TCPAddr).IP.Equal(net.IPv6loopback) {
					t.Error("TCP lost IPv6 source")
				}
				var length uint16
				if binary.Read(conn, binary.BigEndian, &length) != nil {
					return
				}
				query := make([]byte, length)
				if _, err := io.ReadFull(conn, query); err != nil {
					return
				}
				answer, err := answerForQueryRaw(query, dnsTypeAAAA, "2001:db8::44", 60)
				if err != nil {
					t.Error(err)
					return
				}
				binary.Write(conn, binary.BigEndian, uint16(len(answer)))
				conn.Write(answer)
			}()
			root, cancel := context.WithCancel(context.Background())
			defer cancel()
			resolver, err := New(root, Config{Policy: PolicyOff, QueryTimeout: 2 * time.Second}, func(ctx context.Context, network, address string, binding Binding) (net.Conn, error) {
				if binding.SourceIP != "" || binding.SourceIPv6 != "::1" || (network != "tcp6" && network != "udp6") {
					return nil, fmt.Errorf("lost IPv6 binding: %s %+v", network, binding)
				}
				dialer := net.Dialer{}
				if network == "udp6" {
					dialer.LocalAddr = &net.UDPAddr{IP: net.IPv6loopback}
					address = udp.LocalAddr().String()
				} else {
					dialer.LocalAddr = &net.TCPAddr{IP: net.IPv6loopback}
					address = listener.Addr().String()
				}
				return dialer.DialContext(ctx, network, address)
			})
			if err != nil {
				t.Fatal(err)
			}
			result, err := resolver.Resolve(root, Query{Domain: "ipv6.example", RecordType: RecordAAAA, Binding: Binding{Name: "v6", SourceIPv6: "::1", DNSServers: []string{"::1"}}})
			want := "udp"
			if tcpFallback {
				want = "tcp"
			}
			if err != nil || result.Address != "2001:db8::44" || result.Transport != want {
				t.Fatalf("result=%+v error=%v", result, err)
			}
		})
	}
}

func TestIPv6DoHRealTLSAndCacheIsolation(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { serveDoHAnswer(t, w, r) }))
	server.Listener.Close()
	listener, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Fatal(err)
	}
	server.Listener = listener
	server.StartTLS()
	defer server.Close()
	root, cancel := context.WithCancel(context.Background())
	defer cancel()
	var dials atomic.Int64
	resolver, err := New(root, Config{Policy: PolicyAliDNS, QueryTimeout: time.Second}, func(ctx context.Context, network, _ string, binding Binding) (net.Conn, error) {
		if network != "tcp6" || binding.SourceIPv6 != "::1" || binding.SourceIP != "" {
			return nil, fmt.Errorf("incorrect DoH source %+v %s", binding, network)
		}
		dials.Add(1)
		return (&net.Dialer{LocalAddr: &net.TCPAddr{IP: net.IPv6loopback}}).DialContext(ctx, network, listener.Addr().String())
	})
	if err != nil {
		t.Fatal(err)
	}
	binding := Binding{Name: "v6", SourceIPv6: "::1"}
	endpoint := Endpoint{IP: "::1", Host: "example.com", Path: "/dns-query"}
	transport, err := resolver.doHTransport(binding, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	transport.TLSClientConfig.RootCAs = x509.NewCertPool()
	transport.TLSClientConfig.RootCAs.AddCert(server.Certificate())
	for range 2 {
		if _, _, err := resolver.queryDoH(root, "ipv6.example", dnsTypeA, binding, endpoint); err != nil {
			t.Fatal(err)
		}
	}
	if dials.Load() != 1 {
		t.Fatalf("DoH did not reuse IPv6 binding: %d", dials.Load())
	}
	binding.IPv6IfIndex = 8
	other, err := resolver.doHTransport(binding, endpoint)
	if err != nil || other == transport {
		t.Fatal("IPv6 interface change reused old transport")
	}
}

func TestDNSPodDualStackIPv6BootstrapAndIPv4WinnerCancellation(t *testing.T) {
	for _, v4Wins := range []bool{false, true} {
		t.Run(fmt.Sprintf("ipv4Wins=%t", v4Wins), func(t *testing.T) {
			key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "doh.pub"}, DNSNames: []string{"doh.pub"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
			der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				if request.Host != "doh.pub" || request.TLS.ServerName != "doh.pub" {
					t.Error("DoH lost hostname or SNI")
				}
				serveDoHAnswer(t, w, request)
			}))
			server.Listener.Close()
			network, address := "tcp6", "[::1]:0"
			if v4Wins {
				network, address = "tcp4", "127.0.0.1:0"
			}
			listener, err := net.Listen(network, address)
			if err != nil {
				t.Fatal(err)
			}
			server.Listener = listener
			server.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, MinVersion: tls.VersionTLS12}
			server.StartTLS()
			defer server.Close()
			root, cancel := context.WithCancel(context.Background())
			defer cancel()
			loserCanceled := make(chan struct{}, 1)
			resolver, err := New(root, Config{Policy: PolicyDNSPod, QueryTimeout: 2 * time.Second}, func(ctx context.Context, network, address string, binding Binding) (net.Conn, error) {
				_, port, _ := net.SplitHostPort(address)
				blocked := (v4Wins && port == "53") || (!v4Wins && network == "tcp4")
				if blocked {
					<-ctx.Done()
					select {
					case loserCanceled <- struct{}{}:
					default:
					}
					return nil, ctx.Err()
				}
				if port == "53" {
					if network != "udp6" || binding.SourceIP != "" || binding.SourceIPv6 != "::1" {
						t.Errorf("bootstrap binding=%+v network=%s", binding, network)
					}
					client, peer := net.Pipe()
					go func() {
						defer peer.Close()
						buffer := make([]byte, 4096)
						n, err := peer.Read(buffer)
						if err != nil {
							return
						}
						reply, err := answerForQueryRaw(buffer[:n], dnsTypeAAAA, "::1", 60)
						if err == nil {
							peer.Write(reply)
						}
					}()
					return client, nil
				}
				source := net.IPv6loopback
				if network == "tcp4" {
					source = net.IPv4(127, 0, 0, 1)
				}
				return (&net.Dialer{LocalAddr: &net.TCPAddr{IP: source}}).DialContext(ctx, network, listener.Addr().String())
			})
			if err != nil {
				t.Fatal(err)
			}
			binding := Binding{Name: "dual", SourceIP: "127.0.0.1", SourceIPv6: "::1"}
			for _, endpoint := range append(Endpoints(PolicyDNSPod), Endpoint{IP: "::1", Host: "doh.pub", Path: "/dns-query"}) {
				transport, err := resolver.doHTransport(binding, endpoint)
				if err != nil {
					t.Fatal(err)
				}
				if transport.TLSClientConfig.ServerName != "doh.pub" || transport.TLSClientConfig.InsecureSkipVerify {
					t.Fatal("provider TLS authentication was weakened")
				}
				transport.TLSClientConfig.RootCAs = x509.NewCertPool()
				transport.TLSClientConfig.RootCAs.AddCert(server.Certificate())
			}
			start := time.Now()
			result, err := resolver.Resolve(root, Query{Domain: "dual.example", RecordType: RecordA, Binding: binding})
			if err != nil || result.Transport != "doh" {
				t.Fatalf("DNSPod dual-stack=%+v error=%v", result, err)
			}
			if time.Since(start) > time.Second {
				t.Fatal("healthy family waited for blocked bootstrap or provider")
			}
			select {
			case <-loserCanceled:
			case <-time.After(resolver.config.QueryTimeout + time.Second):
				t.Fatal("losing DNS family was not canceled")
			}
		})
	}
}
