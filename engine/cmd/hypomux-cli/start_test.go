package main

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestResolveRequestedAdaptersMatchesNamesCaseInsensitively(t *testing.T) {
	available := []localAdapter{
		{Name: "Ethernet", IfIndex: 7, SourceIP: "192.0.2.10", SourceIPv6: "2001:db8:1::10"},
		{Name: "Wi-Fi", IfIndex: 12, SourceIP: "192.0.2.20"},
	}
	resolved, err := resolveRequestedAdapters([]string{"wi-fi", "Ethernet:3"}, available)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved) != 2 {
		t.Fatalf("resolved = %#v", resolved)
	}
	if resolved[0].Name != "Wi-Fi" || resolved[0].Weight != 1 ||
		resolved[0].SourceIP != "192.0.2.20" || resolved[0].IfIndex != 12 {
		t.Fatalf("first adapter = %#v", resolved[0])
	}
	if resolved[1].Weight != 3 || resolved[1].SourceIPv6 != "2001:db8:1::10" {
		t.Fatalf("second adapter = %#v", resolved[1])
	}
}

func TestResolveRequestedAdaptersRejectsUnknownDuplicateAndBadWeight(t *testing.T) {
	available := []localAdapter{{Name: "Ethernet", IfIndex: 7, SourceIP: "192.0.2.10"}}
	if _, err := resolveRequestedAdapters([]string{"Missing"}, available); err == nil {
		t.Fatal("unknown adapter accepted")
	}
	if _, err := resolveRequestedAdapters([]string{"Ethernet", "ethernet"}, available); err == nil {
		t.Fatal("duplicate adapter accepted")
	}
	// An explicit `:0` means "unspecified" and normalizes to weight 1, exactly
	// like the engine treats a missing weight.
	resolved, err := resolveRequestedAdapters([]string{"Ethernet:0"}, available)
	if err != nil || len(resolved) != 1 || resolved[0].Weight != 1 {
		t.Fatalf("zero weight must normalize to 1: %#v, %v", resolved, err)
	}
	if _, err := resolveRequestedAdapters([]string{"Ethernet:101"}, available); err == nil {
		t.Fatal("overweight adapter accepted")
	}
}

func TestSplitAdapterWeight(t *testing.T) {
	cases := []struct {
		input  string
		name   string
		weight int
	}{
		{"Ethernet", "Ethernet", 0},
		{"Ethernet:3", "Ethernet", 3},
		{"Wi-Fi", "Wi-Fi", 0},
		{"以太网 2", "以太网 2", 0},
		{"Ethernet:abc", "Ethernet:abc", 0},
		{":5", ":5", 0},
		{"Ethernet:", "Ethernet:", 0},
	}
	for _, item := range cases {
		name, weight := splitAdapterWeight(item.input)
		if name != item.name || weight != item.weight {
			t.Fatalf("%q -> (%q, %d), want (%q, %d)", item.input, name, weight, item.name, item.weight)
		}
	}
}

func TestApplyWeightOverrides(t *testing.T) {
	adapters := []startAdapter{{Name: "Ethernet", Weight: 1}, {Name: "Wi-Fi", Weight: 1}}
	if err := applyWeightOverrides(adapters, []string{"Wi-Fi=4", "ethernet=2"}); err != nil {
		t.Fatal(err)
	}
	if adapters[0].Weight != 2 || adapters[1].Weight != 4 {
		t.Fatalf("weights = %#v", adapters)
	}
	if err := applyWeightOverrides(adapters, []string{"Missing=2"}); err == nil {
		t.Fatal("override for unselected adapter accepted")
	}
	if err := applyWeightOverrides(adapters, []string{"Wi-Fi"}); err == nil {
		t.Fatal("malformed override accepted")
	}
}

func TestParseStartFlagsDefaultsAndValidation(t *testing.T) {
	invocation := &commandContext{timeout: defaultCallTimeout}
	flags := invocation.newFlagSet("start")
	options, err := parseStartFlags(flags, []string{"--adapter", "Ethernet", "--adapter", "Wi-Fi:3"})
	if err != nil {
		t.Fatal(err)
	}
	if options.listenHost != "127.0.0.1" || options.connectTimeoutMS != defaultConnectTimeoutMS {
		t.Fatalf("options = %#v", options)
	}
	if len(options.adapters) != 2 || options.adapters[1] != "Wi-Fi:3" {
		t.Fatalf("adapters = %#v", options.adapters)
	}
	if invocation.jsonOut {
		t.Fatal("--json must default to off")
	}

	for _, invalid := range [][]string{
		{"--all", "--adapter", "Ethernet"},
		{"--socks-port", "10800"},
		{"--http-port", "10801"},
		{"--socks-port", "1", "--http-port", "1"},
		{"--strategy", "bogus"},
		{"--adapter", "Ethernet", "extra"},
	} {
		invocation := &commandContext{timeout: defaultCallTimeout}
		flags := invocation.newFlagSet("start")
		if _, err := parseStartFlags(flags, invalid); err == nil {
			t.Fatalf("invalid args accepted: %v", invalid)
		}
	}
}

func TestParseStartFlagsSharedFlags(t *testing.T) {
	invocation := &commandContext{timeout: defaultCallTimeout}
	flags := invocation.newFlagSet("start")
	if _, err := parseStartFlags(flags, []string{"--all", "--json", "--timeout", "45s"}); err != nil {
		t.Fatal(err)
	}
	if !invocation.jsonOut || invocation.timeout.String() != "45s" {
		t.Fatalf("shared flags not parsed: json=%v timeout=%v", invocation.jsonOut, invocation.timeout)
	}
}

func TestStartCommandSendsProtocolStartParams(t *testing.T) {
	originalListAdapters := listAdapters
	listAdapters = func() ([]localAdapter, error) {
		return []localAdapter{{Name: "Ethernet", IfIndex: 7, SourceIP: "192.0.2.10"}}, nil
	}
	t.Cleanup(func() { listAdapters = originalListAdapters })

	var captured json.RawMessage
	engine, s := startFakeEngine(t)
	engine.handlers["engine.hello"] = func(params json.RawMessage) (any, error) {
		return map[string]any{
			"engine": "hypomux-engine", "protocol_version": 1,
			"capabilities":          []string{"engine.hello", "engine.start"},
			"scheduling_strategies": []string{"round-robin", "weighted", "adaptive-throughput", "latency-first"},
		}, nil
	}
	engine.handlers["engine.start"] = func(params json.RawMessage) (any, error) {
		captured = append(json.RawMessage(nil), params...)
		return map[string]any{
			"state": map[string]any{"state": "running"},
			"mode":  "proxy",
			"endpoints": map[string]any{
				"socks": "127.0.0.1:10800", "http": "127.0.0.1:10801",
			},
		}, nil
	}

	var stdout, stderr strings.Builder
	code := run(
		[]string{
			"start",
			"--adapter", "Ethernet:2",
			"--socks-port", "10800", "--http-port", "10801",
			"--strategy", "adaptive-throughput",
			"--dns-server", "223.5.5.5",
			"--dns-cache-ttl-ms", "120000",
		},
		&stdout, &stderr,
		func() transport { return staticTransport{s} },
	)
	if code != 0 {
		t.Fatalf("start exit = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "127.0.0.1:10800") {
		t.Fatalf("start output = %q", stdout.String())
	}

	var params struct {
		Mode             string         `json:"mode"`
		ListenHost       string         `json:"listen_host"`
		SOCKSPort        int            `json:"socks_port"`
		HTTPPort         int            `json:"http_port"`
		Strategy         string         `json:"strategy"`
		Weighted         bool           `json:"weighted"`
		ConnectTimeoutMS int            `json:"connect_timeout_ms"`
		Adapters         []startAdapter `json:"adapters"`
		DNS              struct {
			Policy        string   `json:"policy,omitempty"`
			LegacyServers []string `json:"legacy_servers,omitempty"`
			CacheTTLMS    int      `json:"cache_ttl_ms,omitempty"`
		} `json:"dns"`
	}
	if err := json.Unmarshal(captured, &params); err != nil {
		t.Fatalf("params = %s", string(captured))
	}
	if params.Mode != "proxy" || params.ListenHost != "127.0.0.1" ||
		params.SOCKSPort != 10800 || params.HTTPPort != 10801 ||
		params.Strategy != "adaptive-throughput" || params.Weighted ||
		params.ConnectTimeoutMS != defaultConnectTimeoutMS {
		t.Fatalf("params = %#v", params)
	}
	if len(params.Adapters) != 1 || params.Adapters[0].Name != "Ethernet" ||
		params.Adapters[0].SourceIP != "192.0.2.10" || params.Adapters[0].IfIndex != 7 ||
		params.Adapters[0].Weight != 2 {
		t.Fatalf("adapters = %#v", params.Adapters)
	}
	if params.DNS.Policy != "" || len(params.DNS.LegacyServers) != 1 ||
		params.DNS.LegacyServers[0] != "223.5.5.5" || params.DNS.CacheTTLMS != 120000 {
		t.Fatalf("dns = %#v", params.DNS)
	}
}

func TestStartCommandRejectsUnadvertisedStrategy(t *testing.T) {
	originalListAdapters := listAdapters
	listAdapters = func() ([]localAdapter, error) {
		return []localAdapter{{Name: "Ethernet", IfIndex: 7, SourceIP: "192.0.2.10"}}, nil
	}
	t.Cleanup(func() { listAdapters = originalListAdapters })

	engine, s := startFakeEngine(t)
	engine.handlers["engine.hello"] = func(params json.RawMessage) (any, error) {
		return map[string]any{
			"engine": "hypomux-engine", "protocol_version": 1,
			"capabilities":          []string{"engine.hello", "engine.start"},
			"scheduling_strategies": []string{"round-robin", "weighted"},
		}, nil
	}
	started := false
	engine.handlers["engine.start"] = func(params json.RawMessage) (any, error) {
		started = true
		return map[string]any{}, nil
	}

	var stdout, stderr strings.Builder
	code := run(
		[]string{"start", "--adapter", "Ethernet", "--strategy", "adaptive-throughput"},
		&stdout, &stderr,
		func() transport { return staticTransport{s} },
	)
	if code != 1 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr.String())
	}
	if started {
		t.Fatal("engine.start was issued with an unadvertised strategy")
	}
	if !strings.Contains(stderr.String(), "adaptive-throughput") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestStartCommandWithoutSelectionListsAvailable(t *testing.T) {
	var stdout, stderr strings.Builder
	code := run([]string{"start"}, &stdout, &stderr, unreachableFactory(t))
	if code != 2 {
		t.Fatalf("exit = %d", code)
	}
	// Without any local adapters the hint still explains how to select.
	if !strings.Contains(stderr.String(), "--adapter") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

// staticTransport reuses one established session for all Connect calls; the
// fake engine behind it keeps serving.
type staticTransport struct {
	session *session
}

func (t staticTransport) Connect(context.Context) (io.ReadWriteCloser, error) {
	return t.session.conn, nil
}

func (t staticTransport) Describe() string { return "static" }
