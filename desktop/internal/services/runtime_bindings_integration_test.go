package services

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestRuntimeBindingRealCoreIPv6Loopback(t *testing.T) {
	if os.Getenv("HYPOMUX_ENGINE_PATH") == "" {
		root, err := filepath.Abs(filepath.Join("..", "..", "..", "engine"))
		if err != nil {
			t.Fatal(err)
		}
		core := filepath.Join(t.TempDir(), "hypomux-engine.exe")
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if output, err := exec.CommandContext(ctx, "go", "-C", root, "build", "-o", core, "./cmd/hypomux-engine").CombinedOutput(); err != nil {
			t.Fatalf("build the actual Core: %v\n%s", err, output)
		}
		t.Setenv("HYPOMUX_ENGINE_PATH", core)
	}
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	listener, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("recovered")) }))
	server.Listener = listener
	server.StartTLS()
	defer server.Close()
	cert := server.Certificate()
	roots := x509.NewCertPool()
	roots.AddCert(cert)
	current := AdapterView{ID: "loopback", Name: "loopback", SourceIPv6: "::1", Selected: true, Operational: true, Weight: 1}
	stale := current
	stale.SourceIPv6 = "::2"
	settings := NewSettingsService()
	runRuntimeBindingCoreRecovery(t, settings, NewAdapterService(settings), stale, current,
		listener.Addr().String(), &tls.Config{ServerName: cert.DNSNames[0], RootCAs: roots, MinVersion: tls.VersionTLS12}, false)
}

// The old metadata is deliberately stale; the replacement comes from the
// actual OS. This proves the automatic Desktop -> Core recovery transaction
// and public IPv6 data path, not a physical suspend or NIC reindex event.
func TestRuntimeBindingRealCoreRecoversStaleIPv6Source(t *testing.T) {
	if os.Getenv("HYPOMUX_RUN_RUNTIME_BINDING_TEST") != "1" {
		t.Skip("set HYPOMUX_RUN_RUNTIME_BINDING_TEST=1 with a real test Core and physical IPv6 adapter")
	}
	if os.Getenv("HYPOMUX_ENGINE_PATH") == "" {
		t.Fatal("select an independently built development Core")
	}
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	settings := NewSettingsService()
	adapters := NewAdapterService(settings)
	available, err := adapters.List()
	if err != nil {
		t.Fatal(err)
	}
	var current AdapterView
	for _, adapter := range available {
		if adapter.Name == os.Getenv("HYPOMUX_NETWORK_TEST_ADAPTER") && adapter.SourceIPv6 != "" && !adapter.IsVirtual {
			current = adapter
			break
		}
	}
	if current.ID == "" {
		t.Fatal("selected physical adapter has no preferred IPv6 source")
	}
	current.Selected, current.Address, current.IfIndex = true, "", 0
	current.Weight = 1
	stale := current
	stale.SourceIPv6, stale.IPv6IfIndex = "2001:db8::dead", current.IPv6IfIndex+1000
	stale.DNSServers = []string{"2400:3200::1"}
	runRuntimeBindingCoreRecovery(t, settings, adapters, stale, current, "dns.alidns.com:443",
		&tls.Config{ServerName: "dns.alidns.com", MinVersion: tls.VersionTLS12}, true)
}

func runRuntimeBindingCoreRecovery(t *testing.T, settings *SettingsService, adapters *AdapterService,
	stale, current AdapterView, target string, tlsConfig *tls.Config, publicDNS bool) {
	t.Helper()
	var reservations []net.Listener
	for range 2 {
		listener, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		reservations = append(reservations, listener)
		defer listener.Close()
	}
	next := settings.Get()
	next.Mode, next.DNSPolicy, next.SystemProxyTakeover = "proxy", "alidns", false
	next.SelectedAdapterIDs = []string{current.ID}
	next.SOCKSPort = reservations[0].Addr().(*net.TCPAddr).Port
	next.HTTPPort = reservations[1].Addr().(*net.TCPAddr).Port
	if _, err := settings.Update(next); err != nil {
		t.Fatal(err)
	}
	service := NewEngineService(settings, adapters)
	defer service.Shutdown()
	var mu sync.RWMutex
	observed := stale
	service.runtimeAdapters = func() ([]AdapterView, error) {
		mu.RLock()
		defer mu.RUnlock()
		return cloneRuntimeBindings([]AdapterView{observed}), nil
	}
	for _, listener := range reservations {
		_ = listener.Close()
	}
	if _, err := service.Start("proxy"); err != nil {
		t.Fatal(err)
	}
	defer service.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	assertBinding := func(expected AdapterView) {
		t.Helper()
		var telemetry struct {
			Adapters []struct {
				Name   string `json:"name"`
				Source string `json:"source_ipv6"`
				Index  int    `json:"ipv6_if_index"`
			} `json:"adapters"`
		}
		if err := service.client.Request(ctx, "engine.telemetry", nil, &telemetry); err != nil {
			t.Fatal(err)
		}
		if len(telemetry.Adapters) != 1 || telemetry.Adapters[0].Name != expected.Name || telemetry.Adapters[0].Source != expected.SourceIPv6 || telemetry.Adapters[0].Index != expected.IPv6IfIndex {
			t.Fatal("actual Core did not install the expected source/interface binding")
		}
	}
	assertBinding(stale)
	proxy := net.JoinHostPort("127.0.0.1", strconv.Itoa(next.SOCKSPort))
	oldClient, err := net.DialTimeout("tcp4", proxy, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer oldClient.Close()
	// Complete only method negotiation; hold a real accepted client across the
	// restart to prove the old listener's connections are retired.
	if _, err := oldClient.Write([]byte{5, 1, 0}); err != nil {
		t.Fatal(err)
	}
	var method [2]byte
	if _, err := io.ReadFull(oldClient, method[:]); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	observed = current
	mu.Unlock()
	service.mu.Lock()
	service.lastBindingCheck = time.Now().Add(-6 * time.Second)
	service.mu.Unlock()
	if _, err := service.Snapshot(); err != nil {
		t.Fatal(err)
	}
	if err := service.acquireLifecycle(ctx); err != nil {
		t.Fatal(err)
	}
	service.releaseLifecycle()
	assertBinding(current)
	_ = oldClient.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := oldClient.Read(method[:]); err == nil {
		t.Fatal("old client survived binding recovery")
	} else if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		t.Fatal("old client was not closed")
	}
	if publicDNS {
		var answer dnsResolveResult
		if err := service.client.Request(ctx, "dns.resolve", map[string]any{"domain": "dns.alidns.com", "record_type": "AAAA", "adapter": current.Name}, &answer); err != nil {
			t.Fatal(err)
		}
		if ip := net.ParseIP(answer.Address); ip == nil || ip.To4() != nil {
			t.Fatal("fresh Core resolver did not return IPv6")
		}
	}
	client, err := net.DialTimeout("tcp4", proxy, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	_ = client.SetDeadline(time.Now().Add(10 * time.Second))
	if _, err := client.Write([]byte{5, 1, 0}); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(client, method[:]); err != nil || method != [2]byte{5, 0} {
		t.Fatal("SOCKS authentication failed", err)
	}
	host, portText, err := net.SplitHostPort(target)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	var request []byte
	if ip := net.ParseIP(host); ip != nil && ip.To4() == nil {
		request = append([]byte{5, 1, 0, 4}, ip.To16()...)
	} else {
		request = append([]byte{5, 1, 0, 3, byte(len(host))}, []byte(host)...)
	}
	request = binary.BigEndian.AppendUint16(request, uint16(port))
	if _, err := client.Write(request); err != nil {
		t.Fatal(err)
	}
	var reply [4]byte
	if _, err := io.ReadFull(client, reply[:]); err != nil || reply[1] != 0 {
		t.Fatal("fresh IPv6 SOCKS connection failed", err, reply)
	}
	length := 0
	switch reply[3] {
	case 1:
		length = 4
	case 4:
		length = 16
	case 3:
		if _, err := io.ReadFull(client, method[:1]); err != nil {
			t.Fatal(err)
		}
		length = int(method[0])
	default:
		t.Fatal("invalid SOCKS reply")
	}
	if _, err := io.CopyN(io.Discard, client, int64(length+2)); err != nil {
		t.Fatal(err)
	}
	secure := tls.Client(client, tlsConfig)
	defer secure.Close()
	if err := secure.HandshakeContext(ctx); err != nil {
		t.Fatal("verified IPv6 TLS after automatic recovery", err)
	}
	if !publicDNS {
		if _, err := fmt.Fprintf(secure, "GET / HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", tlsConfig.ServerName); err != nil {
			t.Fatal(err)
		}
		response, err := http.ReadResponse(bufio.NewReader(secure), nil)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(io.LimitReader(response.Body, 32))
		_ = response.Body.Close()
		if err != nil || response.StatusCode != 200 || string(body) != "recovered" {
			t.Fatal("IPv6 payload after recovery failed", response.StatusCode, err)
		}
	}
	service.mu.Lock()
	baseline, notice, enabled := cloneRuntimeBindings(service.runtimeBindings), service.runtimeBindingNotice, service.runtimeBindingEnabled
	service.mu.Unlock()
	if len(baseline) != 1 || baseline[0].SourceIPv6 != current.SourceIPv6 || notice != "" || !enabled {
		t.Fatal("successful recovery did not commit its new baseline")
	}
	t.Log("actual Core binding replaced; old client closed; verified IPv6 SOCKS TLS passed")
}
