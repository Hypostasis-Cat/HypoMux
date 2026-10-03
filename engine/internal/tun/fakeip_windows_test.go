//go:build windows

package tun

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// The production interrupt launches the engine executable as a small helper.
// Dispatch that same helper in the test binary, without a shell or extra tool.
func TestMain(m *testing.M) {
	if len(os.Args) == 3 && os.Args[1] == "signal-tun" {
		pid, err := strconv.ParseUint(os.Args[2], 10, 32)
		if err == nil {
			err = InterruptConsoleProcess(uint32(pid))
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestSupervisorPreservesBundledFakeIPAcrossImmediateAndCachedRestarts(t *testing.T) {
	executable, err := filepath.Abs("../../../bin/sing-box.exe")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(executable); err != nil {
		t.Fatal(err)
	}
	listener, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	endpoint := listener.LocalAddr().String()
	port := listener.LocalAddr().(*net.UDPAddr).Port
	listener.Close()
	directory := t.TempDir()
	config := map[string]any{
		"log": map[string]any{"level": "warn"},
		"dns": map[string]any{
			"servers": []any{map[string]any{"type": "hosts", "tag": "local"}, map[string]any{"type": "fakeip", "tag": "fake", "inet4_range": "198.18.0.0/15", "inet6_range": "fc00::/18"}},
			"final":   "local", "rules": []any{map[string]any{"query_type": []string{"A", "AAAA"}, "server": "fake"}},
		},
		"inbounds":     []any{map[string]any{"type": "direct", "listen": "127.0.0.1", "listen_port": port, "network": "udp"}},
		"outbounds":    []any{map[string]any{"type": "direct", "tag": "direct"}},
		"route":        map[string]any{"rules": []any{map[string]any{"action": "hijack-dns"}}, "final": "direct", "default_domain_resolver": "local"},
		"experimental": map[string]any{"cache_file": map[string]any{"enabled": true, "store_fakeip": true, "path": filepath.Join(directory, "cache.db")}},
	}
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "dns.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	resolver := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "udp4", endpoint)
	}}
	lookup := func(domain string, timeout time.Duration) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		ips, err := resolver.LookupIP(ctx, "ip4", domain+".supervisor-test.example")
		if err != nil {
			return "", err
		}
		if len(ips) != 1 {
			return "", fmt.Errorf("unexpected IPs: %v", ips)
		}
		return ips[0].String(), nil
	}
	supervisor := NewSupervisor()
	// Only the network-facing config/readiness and network cleanup are replaced.
	// Process creation, private console, job containment, interrupt and wait are
	// the production implementation. No host TUN/DNS/routes are modified.
	supervisor.cleanup = func(context.Context) error { return nil }
	supervisor.stageConfig = func(c Config) (string, func(), error) { return c.ConfigPath, func() {}, nil }
	supervisor.command = func(ctx context.Context, exe string, args ...string) *exec.Cmd {
		for i := 0; i < len(args)-1; i++ {
			if args[i] == "-c" {
				args[i+1] = path
			}
		}
		return exec.CommandContext(ctx, exe, args...)
	}
	supervisor.startupReady = func(string) bool { _, err := lookup("ready", 200*time.Millisecond); return err == nil }
	supervisor.readyStableFor = defaultReadyStableFor
	c := testConfig(t)
	c.Executable = executable
	c.StartupTimeout = 5 * time.Second
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		supervisor.Stop(ctx)
	})
	query := func(name string) string {
		t.Helper()
		// The assertion also exercises the on-disk FakeIP cache. Give a cold or
		// busy Windows CI host a bounded query budget without delaying Stop or
		// allowing a periodic cache checkpoint to replace the shutdown flush.
		value, err := lookup(name, 2*time.Second)
		if err != nil {
			t.Fatalf("lookup %s: %v; stderr=%s", name, err, supervisor.run.stderrSnapshot())
		}
		return value
	}
	start := func() {
		t.Helper()
		if _, err := supervisor.Activate(context.Background(), c); err != nil {
			t.Fatal(err)
		}
	}
	stop := func() {
		t.Helper()
		run := supervisor.run
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := supervisor.Stop(ctx); err != nil {
			t.Fatal(err)
		}
		if code := run.command.ProcessState.ExitCode(); code != 0 {
			t.Fatalf("core was force-killed instead of closing cleanly: exit=%d, stderr=%s", code, run.stderrSnapshot())
		}
	}
	start()
	first, second := query("first"), query("second")
	if first == second {
		t.Fatal("distinct names share a FakeIP")
	}
	// No checkpoint sleep: normal stop must flush immediately, including when
	// the following run only reuses cached names and never allocates a new IP.
	stop()
	for restart := 1; restart <= 2; restart++ {
		start()
		if got := query("second"); got != second {
			t.Fatalf("restart %d: second=%s, want %s", restart, got, second)
		}
		if got := query("first"); got != first {
			t.Fatalf("restart %d: first=%s, want %s", restart, got, first)
		}
		stop()
	}
}
