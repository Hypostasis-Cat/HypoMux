package services

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"errors"
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

func ensureRuntimeBindingTestCore(t *testing.T) {
	t.Helper()
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
}

func TestRuntimeBindingRealCoreIPv6Loopback(t *testing.T) {
	ensureRuntimeBindingTestCore(t)
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

// Real Core lifecycle coverage uses IPv4 loopback so IPv6 availability is not
// a prerequisite for testing recovery intent and persisted NIC selection.
func TestRuntimeBindingRealCoreRetryAndRejoin(t *testing.T) {
	ensureRuntimeBindingTestCore(t)
	for _, scenario := range []string{"start_failure", "stop_failure", "original_address", "explicit_stop", "rejoin"} {
		t.Run(scenario, func(t *testing.T) {
			a := AdapterView{ID: "a", Name: "a", Address: "127.0.0.1", Selected: true, Operational: true, Weight: 1}
			b := AdapterView{ID: "b", Name: "b", Address: "127.0.0.2", Selected: true, Operational: true, Weight: 1}
			available := []AdapterView{a}
			if scenario == "rejoin" {
				available = append(available, b)
			}
			s, ctx, poll := newRuntimeBindingLoopbackService(t, &available)
			a.Address = "127.0.0.3"
			available = []AdapterView{a}
			actions := s.runtimeBindingActions()
			if scenario == "stop_failure" {
				stop := actions.stop
				actions.stop = func(ctx context.Context) error {
					if err := stop(ctx); err != nil {
						return err
					}
					return errors.New("transient cleanup failure")
				}
			} else if scenario != "rejoin" {
				start := actions.start
				actions.start = func(ctx context.Context, mode string) error {
					s.runtimeAdapters = func() ([]AdapterView, error) { return nil, errors.New("transient adapter enumeration failure") }
					return start(ctx, mode)
				}
			}
			err := s.refreshRuntimeBindings(ctx, actions)
			if (err != nil) != (scenario != "rejoin") {
				t.Fatalf("unexpected first recovery result: %v", err)
			}
			if !s.runtimeBindingEnabled || len(s.runtimeBindings) == 0 {
				t.Fatal("failed recovery discarded its monitoring state")
			}
			if scenario == "original_address" {
				available[0].Address = "127.0.0.1"
			}
			if scenario == "rejoin" {
				available = append(available, b)
			}
			s.runtimeAdapters = func() ([]AdapterView, error) { return cloneRuntimeBindings(available), nil }
			if scenario == "explicit_stop" {
				s.cancelRuntimeBindingRefresh()
				if _, err := s.stopLocked(ctx); err != nil {
					t.Fatal(err)
				}
			}
			// Exercise the backend scheduler, without a Snapshot/UI call. It must
			// retry even though the first transaction left the Core stopped.
			poll()
			var status engineStatusResult
			if err := s.client.Request(ctx, "engine.status", nil, &status); err != nil {
				t.Fatal(err)
			}
			if scenario == "explicit_stop" {
				if status.Engine.State != "stopped" || s.runtimeBindingEnabled || s.runtimeBindingRecovery != nil {
					t.Fatal("explicit Stop was undone by automatic recovery")
				}
				return
			}
			var telemetry struct {
				Adapters []struct {
					Name   string `json:"name"`
					Source string `json:"source_ip"`
				} `json:"adapters"`
			}
			if err := s.client.Request(ctx, "engine.telemetry", nil, &telemetry); err != nil {
				t.Fatal(err)
			}
			if status.Engine.State != "running" || len(telemetry.Adapters) != len(available) || s.runtimeBindingRecovery != nil {
				t.Fatalf("Core did not recover: state=%s adapters=%+v pending=%v", status.Engine.State, telemetry.Adapters, s.runtimeBindingRecovery != nil)
			}
			for _, expected := range available {
				found := false
				for _, actual := range telemetry.Adapters {
					found = found || actual.Name == expected.Name && actual.Source == expected.Address
				}
				if !found {
					t.Fatalf("Core lost selected binding %+v", expected)
				}
			}
		})
	}
}

// The caller holds the lifecycle gate while replacing the OS inventory fixture.
func newRuntimeBindingLoopbackService(t *testing.T, available *[]AdapterView) (*EngineService, context.Context, func()) {
	t.Helper()
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	settings := NewSettingsService()
	next := settings.Get()
	next.Mode, next.SystemProxyTakeover = "proxy", false
	var reservations []net.Listener
	for range 2 {
		listener, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = listener.Close() })
		reservations = append(reservations, listener)
	}
	next.SOCKSPort = reservations[0].Addr().(*net.TCPAddr).Port
	next.HTTPPort = reservations[1].Addr().(*net.TCPAddr).Port
	for _, adapter := range *available {
		next.SelectedAdapterIDs = append(next.SelectedAdapterIDs, adapter.ID)
	}
	if _, err := settings.Update(next); err != nil {
		t.Fatal(err)
	}
	s := NewEngineService(settings, NewAdapterService(settings))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	if err := s.acquireLifecycle(ctx); err != nil {
		s.Shutdown()
		cancel()
		t.Fatal(err)
	}
	held := true
	t.Cleanup(func() {
		s.cancelRuntimeBindingRefresh()
		if held {
			s.releaseLifecycle()
		}
		s.Shutdown()
		cancel()
	})
	s.runtimeAdapters = func() ([]AdapterView, error) { return cloneRuntimeBindings(*available), nil }
	for _, listener := range reservations {
		_ = listener.Close()
	}
	if _, err := s.startLocked(ctx, "proxy"); err != nil {
		t.Fatal(err)
	}
	poll := func() {
		t.Helper()
		held = false
		s.releaseLifecycle()
		s.scheduleRuntimeBindingRefresh()
		if err := s.acquireLifecycle(ctx); err != nil {
			t.Fatal(err)
		}
		held = true
	}
	return s, ctx, poll
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
	changedScan := make(chan struct{}, 1)
	service.runtimeAdapters = func() ([]AdapterView, error) {
		mu.RLock()
		defer mu.RUnlock()
		if observed.SourceIPv6 == current.SourceIPv6 {
			select {
			case changedScan <- struct{}{}:
			default:
			}
		}
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
	// No Snapshot or other frontend call: the backend's real timer must
	// detect the change even when no window is polling engine state.
	select {
	case <-changedScan:
	case <-ctx.Done():
		t.Fatal("backend did not detect the new binding without UI polling", ctx.Err())
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
	t.Log("without UI polling: actual Core binding replaced; old client closed; verified IPv6 SOCKS TLS passed")
}
