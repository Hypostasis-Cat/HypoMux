package proxy

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Hypostasis-Cat/HypoMux/engine/internal/dns"
)

const testSteamChunk = "/depot/123/chunk/0123456789012345678901234567890123456789"

const testSteamHost = "cache1.steamcontent.com"

func seedCDN(c *steamCDN, adapter, port, ip string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[cdnKey{adapter, testSteamHost, port, ip}] = &SteamCDNEntry{
		Adapter: adapter, Domain: testSteamHost, Port: port, IP: ip, Validated: true, ExpiresAt: time.Now().Add(time.Minute),
	}
	// Exercise the next scheduled exploratory connection.
	c.decisions[adapter+"/"+testSteamHost+":"+port] = 7
	c.discovery[net.JoinHostPort(testSteamHost, port)] = time.Now().Add(time.Minute)
}

func TestSteamCDNOffResetExpiryAndNICIsolation(t *testing.T) {
	c := newSteamCDN(context.Background(), false)
	defer c.cancel()
	if c.active() {
		t.Fatal("must default off")
	}
	c.configure(true, false)
	seedCDN(c, "a", "443", "1.2.3.4")
	seedCDN(c, "b", "443", "5.6.7.8")
	ip, generation := c.choose("a", testSteamHost, "443")
	if ip != "1.2.3.4" {
		t.Fatal(ip)
	}
	if ip, _ := c.choose("a", testSteamHost, "80"); ip != "" {
		t.Fatal("port leaked")
	}
	c.observe(cdnKey{"a", testSteamHost, "443", "1.2.3.4"}, generation, 1024*1024, time.Second)
	if c.snapshot().Entries[0].DownloadBPS != 1024*1024 {
		t.Fatal("missing rate")
	}
	c.outcome(cdnKey{"a", testSteamHost, "443", "1.2.3.4"}, generation, false)
	if ip, _ := c.choose("a", testSteamHost, "443"); ip != "" {
		t.Fatal("cooldown ignored")
	}
	oldContext := c.ctx
	c.configure(false, false)
	if oldContext.Err() == nil || len(c.snapshot().Entries) != 0 {
		t.Fatal("disable did not cancel/clear")
	}
	c.configure(true, false)
	seedCDN(c, "a", "443", "1.2.3.4")
	c.observe(cdnKey{"a", testSteamHost, "443", "1.2.3.4"}, generation, 1024*1024, time.Second)
	if c.snapshot().Entries[0].Samples != 0 {
		t.Fatal("old session repopulated samples")
	}
	c.now = func() time.Time { return time.Now().Add(2 * time.Minute) }
	if ip, _ := c.choose("a", testSteamHost, "443"); ip != "" {
		t.Fatal("expired candidate selected")
	}
}

func TestSteamCDNScope(t *testing.T) {
	for _, host := range []string{"steamcontent.com.evil.test", "store.steampowered.com", "example.com", "x..steamcontent.com", "*.steamcontent.com"} {
		if steamDownloadHost(host) {
			t.Fatalf("matched %s", host)
		}
	}
	for _, ip := range []string{"127.0.0.1", "10.0.0.1", "198.18.1.1", "100.64.1.1", "::1", "fc00::1", "bad"} {
		if publicCDNIP(ip) {
			t.Fatalf("accepted %s", ip)
		}
	}
}

func TestSteamCDNPrefersObservedThroughputWithExploration(t *testing.T) {
	c := newSteamCDN(context.Background(), true)
	defer c.cancel()
	seedCDN(c, "a", "443", "1.2.3.4")
	seedCDN(c, "a", "443", "5.6.7.8")
	c.observe(cdnKey{"a", testSteamHost, "443", "1.2.3.4"}, c.generation, 1024*1024, time.Second)
	c.observe(cdnKey{"a", testSteamHost, "443", "5.6.7.8"}, c.generation, 10*1024*1024, time.Second)
	fast := 0
	for range 32 {
		ip, _ := c.choose("a", testSteamHost, "443")
		if ip == "5.6.7.8" {
			fast++
		}
	}
	if fast < 24 || fast == 32 {
		t.Fatalf("fast selections=%d; expected preference with exploration", fast)
	}
}

func TestSteamCDNDisableDuringCandidateConnectKeepsOriginal(t *testing.T) {
	s, _ := New(Config{SteamCDNEnabled: true, Adapters: []Adapter{{Name: "a", SourceIP: "127.0.0.1"}}})
	s.ctx = context.Background()
	s.cdn = newSteamCDN(s.ctx, true)
	defer s.cdn.cancel()
	seedCDN(s.cdn, "a", "80", "5.6.7.8")
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	original := cdnRemoteConn{a, "1.2.3.4:80"}
	client, peer := net.Pipe()
	defer client.Close()
	defer peer.Close()
	session := s.registry.Begin("socks5", ChannelAggregation, client)
	defer s.registry.Finish(session)
	s.registry.Attach(session, original, "1.2.3.4:80", s.config.Adapters[0])
	started, release := make(chan struct{}), make(chan struct{})
	replacement, remote := net.Pipe()
	defer remote.Close()
	s.dialTCP = func(context.Context, *net.Dialer, string) (net.Conn, error) {
		close(started)
		<-release
		return cdnRemoteConn{replacement, "5.6.7.8:80"}, nil
	}
	result := make(chan net.Conn, 1)
	go func() {
		result <- s.prepareSteamCDN(session, original, s.config.Adapters[0], testSteamHost, "80", testSteamChunk)
	}()
	<-started
	s.cdn.configure(false, false)
	close(release)
	if got := <-result; got != original {
		t.Fatal("disabled pool replaced original")
	}
	if _, err := remote.Write([]byte("x")); err == nil {
		t.Fatal("late replacement was not closed")
	}
	if len(s.cdn.snapshot().Entries) != 0 {
		t.Fatal("late candidate repopulated pool")
	}
}

func TestSteamCandidatesUseEachNICDNSAndExcludeLocalAddresses(t *testing.T) {
	s, _ := New(Config{Adapters: []Adapter{{Name: "a", SourceIP: "127.0.0.1"}, {Name: "b", SourceIP: "127.0.0.2"}, {Name: "local", SourceIP: "127.0.0.3"}}, DNS: dns.Config{Policy: dns.PolicyOff}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var mu sync.Mutex
	seen := map[string]string{}
	resolver, err := dns.New(ctx, s.config.DNS, func(_ context.Context, network, _ string, binding dns.Binding) (net.Conn, error) {
		mu.Lock()
		seen[binding.Name] = binding.SourceIP
		mu.Unlock()
		ip := "1.2.3.4"
		if binding.Name == "b" {
			ip = "5.6.7.8"
		}
		if binding.Name == "local" {
			ip = "10.0.0.1"
		}
		a, b := net.Pipe()
		go answerDNSAOnce(b, network, net.ParseIP(ip).To4())
		return a, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	candidates := s.steamCandidates(ctx, testSteamHost, resolver)
	if len(candidates) != 2 || candidates[0].ip != "1.2.3.4" || candidates[1].ip != "5.6.7.8" {
		t.Fatal(candidates)
	}
	mu.Lock()
	defer mu.Unlock()
	if seen["a"] != "127.0.0.1" || seen["b"] != "127.0.0.2" {
		t.Fatal("DNS bindings lost", seen)
	}
	for _, candidate := range candidates {
		if !candidate.expires.After(time.Now()) || candidate.expires.After(time.Now().Add(cdnLifetime)) {
			t.Fatal("invalid expiry", candidate)
		}
	}
}

func TestSteamSniffPreservesHTTPAndMalformedBytes(t *testing.T) {
	for _, test := range []struct{ payload, host string }{
		{"GET " + testSteamChunk + " HTTP/1.1\r\nHost: " + testSteamHost + "\r\n\r\nbody", testSteamHost},
		{"GET / HTTP/1.1\r\nHost: " + testSteamHost + ":80\r\n\r\n", testSteamHost},
		{"GET / HTTP/1.1\r\nHost: " + testSteamHost + ":81\r\n\r\n", ""},
		{"GET / HTTP/1.1\r\nHost: " + testSteamHost + "\r\nHost: evil.test\r\n\r\n", ""},
		{"POST / HTTP/1.1\r\nHost: " + testSteamHost + "\r\n\r\n", testSteamHost},
		{"GET / HTTP/1.1\r\nHost: example.com\r\n\r\n", ""},
		{"garbage", ""},
	} {
		client, peer := net.Pipe()
		go func() { _, _ = io.WriteString(peer, test.payload); _ = peer.Close() }()
		reader := bufio.NewReaderSize(client, 32*1024)
		if host := sniffSteamHost(reader, client, "80"); host != test.host {
			t.Errorf("host %q expected %q", host, test.host)
		}
		data, err := io.ReadAll(reader)
		if err != nil || string(data) != test.payload {
			t.Fatalf("payload changed: %q %v", data, err)
		}
		_ = client.Close()
	}
}

func TestSteamSniffTLSAndFragmentedClientHello(t *testing.T) {
	// Generate a real ClientHello without needing a certificate or network.
	client, peer := net.Pipe()
	go func() {
		defer client.Close()
		_ = tls.Client(client, &tls.Config{ServerName: testSteamHost}).Handshake()
	}()
	_ = peer.SetReadDeadline(time.Now().Add(time.Second))
	header := make([]byte, 5)
	if _, err := io.ReadFull(peer, header); err != nil {
		t.Fatal(err)
	}
	length := int(header[3])<<8 | int(header[4])
	hello := make([]byte, length)
	if _, err := io.ReadFull(peer, hello); err != nil {
		t.Fatal(err)
	}
	_ = peer.Close()
	for _, fragmented := range []bool{false, true} {
		payload := append(append([]byte{}, header...), hello...)
		if fragmented {
			payload = nil
			for pos := 0; pos < len(hello); {
				n := min(73, len(hello)-pos)
				payload = append(payload, 22, 3, 1, byte(n>>8), byte(n))
				payload = append(payload, hello[pos:pos+n]...)
				pos += n
			}
		}
		a, b := net.Pipe()
		go func() { _, _ = b.Write(payload); _ = b.Close() }()
		reader := bufio.NewReaderSize(a, 32*1024)
		if host := sniffSteamHost(reader, a, "443"); host != testSteamHost {
			t.Fatalf("SNI = %q, fragmented=%v", host, fragmented)
		}
		data, err := io.ReadAll(reader)
		if err != nil || !bytes.Equal(data, payload) {
			t.Fatal("TLS bytes changed", err)
		}
		_ = a.Close()
	}
}

func TestSteamSniffTimeoutPreservesPartialData(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	go func() { _, _ = io.WriteString(b, "GET ") }()
	reader := bufio.NewReaderSize(a, 32*1024)
	if got := sniffSteamHost(reader, a, "80"); got != "" {
		t.Fatal(got)
	}
	go func() { _, _ = io.WriteString(b, "rest"); _ = b.Close() }()
	data, err := io.ReadAll(reader)
	if err != nil || string(data) != "GET rest" {
		t.Fatal(string(data), err)
	}
}

func TestSteamCandidateRejectsInvalidCertificate(t *testing.T) {
	origin := httptest.NewTLSServer(nil)
	defer origin.Close()
	s, _ := New(Config{Adapters: []Adapter{{Name: "a", SourceIP: "127.0.0.1"}}})
	s.dialTCP = func(ctx context.Context, _ *net.Dialer, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", origin.Listener.Addr().String())
	}
	if s.verifySteamCandidate(context.Background(), s.config.Adapters[0], testSteamHost, "1.2.3.4") {
		t.Fatal("accepted untrusted/wrong-host certificate")
	}
}

type cdnRemoteConn struct {
	net.Conn
	remote string
}

func (c cdnRemoteConn) RemoteAddr() net.Addr {
	addr, _ := net.ResolveTCPAddr("tcp", c.remote)
	return addr
}

func TestSteamCDNProxyAndTUNRelayFallbackAndDirect(t *testing.T) {
	for _, mode := range []string{"http", "connect", "socks-domain", "tun", "tun-failure", "tun-direct", "disabled"} {
		t.Run(mode, func(t *testing.T) {
			config := Config{SteamCDNEnabled: mode != "disabled", Adapters: []Adapter{{Name: "a", SourceIP: "127.0.0.1"}}, DNS: dns.Config{Policy: dns.PolicyOff}}
			channel := ""
			if mode == "tun" || mode == "tun-failure" || mode == "tun-direct" {
				channel = ChannelAggregation
				if mode == "tun-direct" {
					channel = ChannelDirect
				}
				config.Channels = []Channel{{Name: ChannelAggregation, AdapterNames: []string{"a"}}, {Name: ChannelEthernet, AdapterNames: []string{"a"}}, {Name: ChannelWiFi, AdapterNames: []string{"a"}}}
				if channel == ChannelDirect {
					config.Channels = append(config.Channels, Channel{Name: ChannelDirect})
				}
			}
			s, err := New(config)
			if err != nil {
				t.Fatal(err)
			}
			var mu sync.Mutex
			var targets []string
			s.dialTCP = func(_ context.Context, dialer *net.Dialer, target string) (net.Conn, error) {
				mu.Lock()
				targets = append(targets, target)
				mu.Unlock()
				if channel != ChannelDirect && dialer.LocalAddr.String() != "127.0.0.1:0" {
					t.Error("lost NIC binding", dialer.LocalAddr)
				}
				if mode == "tun-failure" && target == "5.6.7.8:80" {
					return nil, errors.New("candidate unavailable")
				}
				a, b := net.Pipe()
				go func() { defer b.Close(); _, _ = io.Copy(b, b) }()
				return cdnRemoteConn{a, target}, nil
			}
			endpoints, err := s.Start()
			if err != nil {
				t.Fatal(err)
			}
			defer stopServer(t, s)
			resolver, err := dns.New(s.ctx, s.config.DNS, func(_ context.Context, network, _ string, _ dns.Binding) (net.Conn, error) {
				a, b := net.Pipe()
				go answerDNSAOnce(b, network, net.ParseIP("1.2.3.4").To4())
				return a, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			s.resolver = resolver
			port := "80"
			if mode == "connect" {
				port = "443"
			}
			if mode != "disabled" {
				seedCDN(s.cdn, "a", port, "5.6.7.8")
			}
			endpoint := endpoints.SOCKS
			if channel != "" {
				endpoint = endpoints.Channels[channel]
			}
			if mode == "http" || mode == "connect" {
				endpoint = endpoints.HTTP
			}
			client, err := net.Dial("tcp", endpoint)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			_ = client.SetDeadline(time.Now().Add(3 * time.Second))
			payload := "GET " + testSteamChunk + " HTTP/1.1\r\nHost: " + testSteamHost + "\r\n\r\n"
			if mode == "http" {
				_, _ = io.WriteString(client, "GET http://"+testSteamHost+testSteamChunk+" HTTP/1.1\r\nHost: "+testSteamHost+"\r\n\r\n")
			} else if mode == "connect" {
				_, _ = io.WriteString(client, "CONNECT "+testSteamHost+":443 HTTP/1.1\r\nHost: "+testSteamHost+"\r\n\r\n")
				r := bufio.NewReader(client)
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						t.Fatal(err)
					}
					if line == "\r\n" {
						break
					}
				}
				_, _ = io.WriteString(client, payload)
			} else {
				_, _ = client.Write([]byte{5, 1, 0})
				if _, err := io.ReadFull(client, make([]byte, 2)); err != nil {
					t.Fatal(err)
				}
				request := []byte{5, 1, 0, 1, 1, 2, 3, 4, 0, 80}
				if mode == "socks-domain" {
					request = append([]byte{5, 1, 0, 3, byte(len(testSteamHost))}, []byte(testSteamHost)...)
					request = append(request, 0, 80)
				}
				_, _ = client.Write(request)
				reply := make([]byte, 10)
				if _, err := io.ReadFull(client, reply); err != nil || reply[1] != 0 {
					t.Fatal(reply, err)
				}
				_, _ = io.WriteString(client, payload)
			}
			response := make([]byte, len(payload))
			if _, err := io.ReadFull(client, response); err != nil || string(response) != payload {
				t.Fatal(string(response), err)
			}
			mu.Lock()
			got := append([]string(nil), targets...)
			mu.Unlock()
			want := 2
			if mode == "disabled" || mode == "tun-direct" {
				want = 1
			}
			if len(got) != want {
				t.Fatalf("targets = %v", got)
			}
			if want == 2 && got[1] != "5.6.7.8:"+port {
				t.Fatal(got)
			}
			if mode == "tun-failure" && s.cdn.snapshot().Fallbacks != 1 {
				t.Fatal("missing fallback")
			}
			if mode != "tun-direct" && s.Snapshot(false).Adapters[0].Connections != 1 {
				t.Fatal("replacement double-counted connection")
			}
		})
	}
}

func TestSteamTUNPreservesEndToEndTLS(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), DNSNames: []string{testSteamHost}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, public, private)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(certificate)
	origin := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil || r.TLS.ServerName != testSteamHost || r.Host != testSteamHost {
			t.Error("lost TLS SNI or HTTP Host")
		}
		_, _ = io.WriteString(w, "verified end-to-end download")
	}))
	origin.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: private}}}
	origin.StartTLS()
	defer origin.Close()
	s, err := New(Config{SteamCDNEnabled: true, Adapters: []Adapter{{Name: "a", SourceIP: "127.0.0.1"}}, Channels: []Channel{
		{Name: ChannelAggregation, AdapterNames: []string{"a"}}, {Name: ChannelEthernet, AdapterNames: []string{"a"}}, {Name: ChannelWiFi, AdapterNames: []string{"a"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	s.dialTCP = func(ctx context.Context, _ *net.Dialer, target string) (net.Conn, error) {
		conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", origin.Listener.Addr().String())
		if err != nil {
			return nil, err
		}
		return cdnRemoteConn{conn, target}, nil
	}
	endpoints, err := s.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer stopServer(t, s)
	seedCDN(s.cdn, "a", "443", "5.6.7.8")
	client, err := net.Dial("tcp", endpoints.Channels[ChannelAggregation])
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	_ = client.SetDeadline(time.Now().Add(3 * time.Second))
	_, _ = client.Write([]byte{5, 1, 0})
	if _, err := io.ReadFull(client, make([]byte, 2)); err != nil {
		t.Fatal(err)
	}
	_, _ = client.Write([]byte{5, 1, 0, 1, 1, 2, 3, 4, 1, 187})
	reply := make([]byte, 10)
	if _, err := io.ReadFull(client, reply); err != nil || reply[1] != 0 {
		t.Fatal(reply, err)
	}
	secure := tls.Client(client, &tls.Config{ServerName: testSteamHost, RootCAs: roots})
	if err := secure.Handshake(); err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(secure, "GET "+testSteamChunk+" HTTP/1.1\r\nHost: "+testSteamHost+"\r\nConnection: close\r\n\r\n")
	response, err := http.ReadResponse(bufio.NewReader(secure), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil || string(data) != "verified end-to-end download" {
		t.Fatal(string(data), err)
	}
	if s.cdn.snapshot().Replacements != 1 {
		t.Fatal("TUN TLS did not use preferred IP")
	}
}

func TestSteamGlobalDualStackDiscovery(t *testing.T) {
	adapters := []Adapter{{Name: "HK", SourceIP: "192.0.2.1", SourceIPv6: "2001:db8::1"}, {Name: "Tokyo", SourceIP: "192.0.2.2", SourceIPv6: "2001:db8::2"}}
	started := make(chan dns.Query, 4)
	release := make(chan struct{})
	expiry := time.Now().Add(time.Minute)
	resolve := func(ctx context.Context, q dns.Query) (dns.Result, error) {
		started <- q
		select {
		case <-release:
		case <-ctx.Done():
			return dns.Result{}, ctx.Err()
		}
		if q.RecordType == dns.RecordAAAA {
			return dns.Result{Address: "2606:4700::1111", ExpiresAt: &expiry}, nil
		}
		addresses := []string{"1.1.1.1", "1.1.1.2", "1.1.1.3", "1.1.1.4", "1.1.1.5", "1.1.1.6", "1.1.1.7", "1.1.1.8"}
		if q.Binding.Name == "Tokyo" {
			addresses[0] = "8.8.8.8"
		}
		return dns.Result{Address: addresses[0], Addresses: addresses, ExpiresAt: &expiry}, nil
	}
	done := make(chan []cdnCandidate, 1)
	go func() { done <- collectSteamCandidates(context.Background(), testSteamHost, adapters, resolve) }()
	for range 4 {
		select {
		case <-started:
		case <-time.After(time.Second):
			close(release)
			t.Fatal("DNS families/interfaces were serialized")
		}
	}
	close(release)
	candidates := <-done
	if len(candidates) != 10 || candidates[0].ip != "1.1.1.1" || candidates[1].ip != "2606:4700::1111" || candidates[2].ip != "8.8.8.8" {
		t.Fatalf("family or NIC starved: %+v", candidates)
	}
}

func TestSteamCandidateCancellationAndTTL(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got := collectSteamCandidates(ctx, testSteamHost, []Adapter{{Name: "US", SourceIPv6: "2001:db8::1"}}, func(ctx context.Context, q dns.Query) (dns.Result, error) {
		t.Error("cancelled discovery queried DNS")
		return dns.Result{}, ctx.Err()
	})
	if len(got) != 0 {
		t.Fatal("cancelled discovery returned candidates")
	}
	soon := time.Now().Add(time.Second)
	later := soon.Add(time.Minute)
	got = mergeSteamCandidates([][]cdnCandidate{{{"1.1.1.1", later}}, {{"1.1.1.1", soon}}})
	if len(got) != 1 || !got[0].expires.Equal(soon) {
		t.Fatal("duplicate extended DNS lifetime", got)
	}
}

func TestSteamGlobalHostScope(t *testing.T) {
	// Synthetic region labels exercise the wildcard policy; they are not a
	// hardcoded list of servers and are never resolved by this test.
	for _, region := range []string{"hkg", "tyo", "lax", "fra", "syd", "gru"} {
		host := "cache1-" + region + "1.steamcontent.com"
		if !steamDownloadHost(host) {
			t.Fatal("global region rejected", host)
		}
		req, _ := http.NewRequest("GET", "http://"+host+testSteamChunk, nil)
		if uri, reason := steamRequestURI(req); uri != testSteamChunk || reason != "http_eligible" {
			t.Fatal("global chunk rejected", reason)
		}
		if steamDownloadHost(host + ".example.com") {
			t.Fatal("lookalike accepted")
		}
	}
}

func TestSteamIPv6OnlyDiscoveryFiltersInvalidAnswers(t *testing.T) {
	expiry := time.Now().Add(time.Minute)
	got := collectSteamCandidates(context.Background(), testSteamHost, []Adapter{{Name: "IPv6 only", SourceIPv6: "2001:db8::1"}}, func(ctx context.Context, q dns.Query) (dns.Result, error) {
		if q.RecordType != dns.RecordAAAA || q.Binding.Name != "IPv6 only" {
			t.Errorf("unexpected query: %+v", q)
		}
		return dns.Result{Addresses: []string{"::1", "fd00::1", "bad", "1.1.1.1", "2606:4700:0:0:0:0:0:1111", "2606:4700::1111"}, ExpiresAt: &expiry}, nil
	})
	if len(got) != 1 || got[0].ip != "2606:4700::1111" {
		t.Fatal("invalid family/private address or duplicate accepted", got)
	}
}
