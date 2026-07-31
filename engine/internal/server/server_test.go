package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Hypostasis-Cat/HypoMux/engine/internal/protocol"
	engineRuntime "github.com/Hypostasis-Cat/HypoMux/engine/internal/runtime"
	"github.com/Hypostasis-Cat/HypoMux/engine/internal/tun"
)

func TestServerHandshakeStatusAndShutdown(t *testing.T) {
	input := strings.Join([]string{
		`{"protocol":1,"id":"hello-1","method":"engine.hello"}`,
		`{"protocol":1,"id":"health-1","method":"health.check"}`,
		`{"protocol":1,"id":"status-1","method":"engine.status"}`,
		`{"protocol":1,"id":"diagnostic-1","method":"diagnostic.run","params":{"src_ip":"invalid"}}`,
		`{"protocol":1,"id":"missing-1","method":"engine.missing"}`,
		`{"protocol":1,"id":"shutdown-1","method":"host.shutdown"}`,
	}, "\n")

	var output bytes.Buffer
	engineServer := New(strings.NewReader(input), &output, Metadata{
		Name:    "hypomux-engine",
		Version: "test",
		Commit:  "abc123",
	})

	if err := engineServer.Run(context.Background()); err != nil {
		t.Fatalf("Run() failed: %v", err)
	}

	messages := decodeMessages(t, output.String())
	if len(messages) != 7 {
		t.Fatalf("message count = %d, want 7\n%s", len(messages), output.String())
	}

	helloResult := resultObject(t, messages[0])
	if helloResult["protocol_version"] != float64(1) {
		t.Fatalf("hello protocol_version = %#v", helloResult["protocol_version"])
	}
	if helloResult["transport"] != "stdio-jsonl" {
		t.Fatalf("hello transport = %#v", helloResult["transport"])
	}
	if helloResult["engine_version"] != "test" {
		t.Fatalf("hello engine_version = %#v", helloResult["engine_version"])
	}
	modes, ok := helloResult["modes"].([]any)
	if !ok || len(modes) != 2 || modes[1] != "tun_tcp_pool" {
		t.Fatalf("hello modes = %#v", helloResult["modes"])
	}
	features, ok := helloResult["mode_features"].(map[string]any)
	if !ok {
		t.Fatalf("hello mode_features = %#v", helloResult["mode_features"])
	}
	tunFeatures, ok := features["tun_tcp_pool"].([]any)
	if !ok || len(tunFeatures) != 6 ||
		tunFeatures[2] != "ipv6_egress" ||
		tunFeatures[3] != "adaptive_health" ||
		tunFeatures[4] != "managed_tun_lifecycle" ||
		tunFeatures[5] != "dynamic_nic_channels" {
		t.Fatalf("TUN mode features = %#v", features["tun_tcp_pool"])
	}

	healthResult := resultObject(t, messages[1])
	if healthResult["ok"] != true {
		t.Fatalf("health ok = %#v", healthResult["ok"])
	}
	if healthResult["state"] != "stopped" {
		t.Fatalf("health state = %#v", healthResult["state"])
	}

	statusResult := resultObject(t, messages[2])
	engineStatus, ok := statusResult["engine"].(map[string]any)
	if !ok {
		t.Fatalf("status engine = %#v", statusResult["engine"])
	}
	if engineStatus["state"] != "stopped" {
		t.Fatalf("status state = %#v", engineStatus["state"])
	}

	diagnosticResult := resultObject(t, messages[3])
	if diagnosticResult["status"] != "unavailable" || diagnosticResult["note"] != "invalid --src-ip" {
		t.Fatalf("diagnostic result = %#v", diagnosticResult)
	}

	errorObject, ok := messages[4]["error"].(map[string]any)
	if !ok || errorObject["code"] != "method_not_found" {
		t.Fatalf("unknown method error = %#v", messages[4]["error"])
	}

	shutdownResult := resultObject(t, messages[5])
	if shutdownResult["accepted"] != true {
		t.Fatalf("shutdown accepted = %#v", shutdownResult["accepted"])
	}
	if messages[6]["event"] != "host.exiting" {
		t.Fatalf("shutdown event = %#v", messages[6]["event"])
	}
	if messages[6]["sequence"] != float64(1) {
		t.Fatalf("shutdown event sequence = %#v", messages[6]["sequence"])
	}
}

type fakeTunController struct {
	mu           sync.Mutex
	status       tun.Status
	activateErr  error
	stopErr      error
	stopCalls    int
	onStop       func()
	onLog        func(string)
	onUnexpected func(tun.Status)
}

func (f *fakeTunController) Activate(
	context.Context,
	tun.Config,
) (tun.Status, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.activateErr != nil {
		f.status = tun.Status{
			State:     tun.StateFailed,
			LastError: f.activateErr.Error(),
		}
		return f.status, f.activateErr
	}
	now := time.Now().UTC()
	f.status = tun.Status{
		State:     tun.StateRunning,
		PID:       1234,
		StartedAt: &now,
	}
	return f.status, nil
}

func (f *fakeTunController) Stop(context.Context) (tun.Status, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopCalls++
	if f.onStop != nil {
		f.onStop()
	}
	f.status = tun.Status{State: tun.StateStopped}
	return f.status, f.stopErr
}

func (f *fakeTunController) Status() tun.Status {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.status.State == "" {
		return tun.Status{State: tun.StateStopped}
	}
	return f.status
}

func (f *fakeTunController) SetHandlers(
	onLog func(string),
	onUnexpected func(tun.Status),
) {
	f.mu.Lock()
	f.onLog = onLog
	f.onUnexpected = onUnexpected
	f.mu.Unlock()
}

func TestManagedTunLifecycleRequiresPreparedPoolAndStopsInOrder(t *testing.T) {
	var output bytes.Buffer
	engineServer := New(
		strings.NewReader(""),
		&output,
		Metadata{Name: "test"},
	)
	controller := &fakeTunController{
		status: tun.Status{State: tun.StateStopped},
	}
	engineServer.tun = controller
	startTUNPoolForLifecycleTest(t, engineServer)

	controller.onStop = func() {
		if engineServer.proxy == nil || !engineServer.proxy.Running() {
			t.Error("TUN sidecar was not stopped before the outbound pool")
		}
	}
	response, _ := engineServer.handle(
		context.Background(),
		[]byte(`{
			"protocol":1,
			"id":"tun-activate",
			"method":"tun.activate",
			"params":{
				"executable":"C:\\HypoMux\\bin\\sing-box.exe",
				"config_path":"C:\\Users\\Example\\.hypomux\\singbox-config.json",
				"startup_timeout_ms":1500
			}
		}`),
	)
	if response.Error != nil {
		t.Fatalf("tun.activate failed: %#v", response.Error)
	}
	if controller.Status().State != tun.StateRunning {
		t.Fatalf("TUN status = %#v", controller.Status())
	}

	stopResponse := engineServer.stopProxy("engine-stop")
	if stopResponse.Error != nil {
		t.Fatalf("engine.stop failed: %#v", stopResponse.Error)
	}
	if controller.stopCalls != 1 ||
		engineServer.runtime.Snapshot().State != engineRuntime.StateStopped ||
		engineServer.proxy != nil {
		t.Fatalf(
			"stop result: calls=%d state=%s proxy=%v",
			controller.stopCalls,
			engineServer.runtime.Snapshot().State,
			engineServer.proxy,
		)
	}
}

func TestManagedTunActivationFailureRollsBackPreparedPool(t *testing.T) {
	engineServer := New(
		strings.NewReader(""),
		&bytes.Buffer{},
		Metadata{Name: "test"},
	)
	controller := &fakeTunController{
		status:      tun.Status{State: tun.StateStopped},
		activateErr: errors.New("synthetic activation failure"),
	}
	engineServer.tun = controller
	startTUNPoolForLifecycleTest(t, engineServer)

	response, _ := engineServer.handle(
		context.Background(),
		[]byte(`{
			"protocol":1,
			"id":"tun-activate",
			"method":"tun.activate",
			"params":{"executable":"x","config_path":"y"}
		}`),
	)
	if response.Error == nil || response.Error.Code != "tun_failed" {
		t.Fatalf("activation response = %#v", response)
	}
	if engineServer.proxy != nil ||
		engineServer.runtime.Snapshot().State != engineRuntime.StateFailed ||
		controller.stopCalls != 1 {
		t.Fatalf(
			"rollback: proxy=%v state=%s stopCalls=%d",
			engineServer.proxy,
			engineServer.runtime.Snapshot().State,
			controller.stopCalls,
		)
	}
	_ = engineServer.stopProxy("clear-failed")
}

func TestUnexpectedManagedTunExitStopsPoolAndFailsEngine(t *testing.T) {
	engineServer := New(
		strings.NewReader(""),
		&bytes.Buffer{},
		Metadata{Name: "test"},
	)
	controller := &fakeTunController{
		status: tun.Status{State: tun.StateRunning, PID: 1234},
	}
	engineServer.tun = controller
	startTUNPoolForLifecycleTest(t, engineServer)

	engineServer.handleTunUnexpectedExit(tun.Status{
		State:     tun.StateFailed,
		LastError: "synthetic crash",
	})
	if engineServer.proxy != nil ||
		engineServer.runtime.Snapshot().State != engineRuntime.StateFailed {
		t.Fatalf(
			"unexpected-exit rollback: proxy=%v state=%s",
			engineServer.proxy,
			engineServer.runtime.Snapshot().State,
		)
	}
	_ = engineServer.stopProxy("clear-failed")
}

func TestTunActivateRejectsUnpreparedAndOrdinaryProxyModes(t *testing.T) {
	engineServer := New(
		strings.NewReader(""),
		&bytes.Buffer{},
		Metadata{Name: "test"},
	)
	response, _ := engineServer.handle(
		context.Background(),
		[]byte(`{
			"protocol":1,
			"id":"tun-activate",
			"method":"tun.activate",
			"params":{"executable":"x","config_path":"y"}
		}`),
	)
	if response.Error == nil || response.Error.Code != "invalid_state" {
		t.Fatalf("unprepared activation = %#v", response)
	}
}

func startTUNPoolForLifecycleTest(t *testing.T, server *Server) {
	t.Helper()
	request := protocol.Request{
		Protocol: protocol.Version,
		ID:       "start-tun-pool",
		Method:   "engine.start",
		Params: json.RawMessage(`{
			"mode":"tun_tcp_pool",
			"listen_host":"127.0.0.1",
			"adapters":[
				{"name":"loopback","source_ip":"127.0.0.1","weight":1}
			],
			"channels":[
				{"name":"nic_ethernet","adapter_names":["loopback"]},
				{"name":"nic_wifi","adapter_names":["loopback"]},
				{"name":"aggregation","adapter_names":["loopback"]}
			]
		}`),
	}
	response := server.startProxy(request)
	if response.Error != nil {
		t.Fatalf("prepare TUN pool: %#v", response.Error)
	}
}

func TestServerRejectsUnsupportedProtocol(t *testing.T) {
	input := `{"protocol":99,"id":"bad-version","method":"engine.hello"}` + "\n"
	var output bytes.Buffer

	if err := New(strings.NewReader(input), &output, Metadata{}).Run(context.Background()); err != nil {
		t.Fatalf("Run() failed: %v", err)
	}

	messages := decodeMessages(t, output.String())
	errorObject, ok := messages[0]["error"].(map[string]any)
	if !ok || errorObject["code"] != "unsupported_protocol" {
		t.Fatalf("protocol error = %#v", messages[0]["error"])
	}
}

func TestServerRejectsInvalidJSONAndContinues(t *testing.T) {
	input := "{not-json}\n" +
		`{"protocol":1,"id":"health-1","method":"health.check"}` + "\n"
	var output bytes.Buffer

	if err := New(strings.NewReader(input), &output, Metadata{}).Run(context.Background()); err != nil {
		t.Fatalf("Run() failed: %v", err)
	}

	messages := decodeMessages(t, output.String())
	if len(messages) != 2 {
		t.Fatalf("message count = %d, want 2", len(messages))
	}
	errorObject, ok := messages[0]["error"].(map[string]any)
	if !ok || errorObject["code"] != "invalid_json" {
		t.Fatalf("invalid JSON error = %#v", messages[0]["error"])
	}
	if resultObject(t, messages[1])["ok"] != true {
		t.Fatalf("server did not recover after invalid JSON")
	}
}

func TestServerRejectsTrailingJSON(t *testing.T) {
	input := `{"protocol":1,"id":"first","method":"health.check"} {"second":true}` + "\n"
	var output bytes.Buffer

	if err := New(strings.NewReader(input), &output, Metadata{}).Run(context.Background()); err != nil {
		t.Fatalf("Run() failed: %v", err)
	}

	messages := decodeMessages(t, output.String())
	errorObject, ok := messages[0]["error"].(map[string]any)
	if !ok || errorObject["code"] != "invalid_json" {
		t.Fatalf("trailing JSON error = %#v", messages[0]["error"])
	}
}

func TestServerStartsStopsAndReportsProxyTelemetry(t *testing.T) {
	input := strings.Join([]string{
		`{"protocol":1,"id":"start-1","method":"engine.start","params":{"mode":"proxy","socks_port":0,"http_port":0,"adapters":[{"name":"loopback","source_ip":"127.0.0.1"}]}}`,
		`{"protocol":1,"id":"telemetry-1","method":"engine.telemetry"}`,
		`{"protocol":1,"id":"stop-1","method":"engine.stop"}`,
		`{"protocol":1,"id":"shutdown-1","method":"host.shutdown"}`,
	}, "\n")
	var output bytes.Buffer

	if err := New(strings.NewReader(input), &output, Metadata{}).Run(context.Background()); err != nil {
		t.Fatalf("Run() failed: %v", err)
	}

	messages := decodeMessages(t, output.String())
	if len(messages) != 7 {
		t.Fatalf("message count = %d, want 7\n%s", len(messages), output.String())
	}
	start := resultObject(t, messages[0])
	endpoints, ok := start["endpoints"].(map[string]any)
	if !ok || endpoints["socks"] == "" || endpoints["http"] == "" {
		t.Fatalf("start endpoints = %#v", start["endpoints"])
	}
	if messages[1]["event"] != "engine.state_changed" {
		t.Fatalf("start event = %#v", messages[1])
	}
	telemetry := resultObject(t, messages[2])
	if _, ok := telemetry["adapters"].([]any); !ok {
		t.Fatalf("telemetry adapters = %#v", telemetry["adapters"])
	}
	stop := resultObject(t, messages[3])
	if stop["accepted"] != true {
		t.Fatalf("stop result = %#v", stop)
	}
	if messages[4]["event"] != "engine.state_changed" {
		t.Fatalf("stop event = %#v", messages[4])
	}
	if resultObject(t, messages[5])["accepted"] != true {
		t.Fatalf("shutdown result = %#v", messages[5])
	}
	if messages[6]["event"] != "host.exiting" {
		t.Fatalf("shutdown event = %#v", messages[6])
	}
}

func TestServerReportsDNSStatusAndStructuredResolutionFailure(t *testing.T) {
	input := strings.Join([]string{
		`{"protocol":1,"id":"start-1","method":"engine.start","params":{"mode":"proxy","socks_port":0,"http_port":0,"dns":{"policy":"off","legacy_servers":["127.0.0.1"]},"adapters":[{"name":"loopback","source_ip":"127.0.0.1"}]}}`,
		`{"protocol":1,"id":"dns-status-1","method":"dns.status"}`,
		`{"protocol":1,"id":"dns-resolve-1","method":"dns.resolve","params":{"domain":"bad_domain","adapter":"loopback","record_type":"A"}}`,
		`{"protocol":1,"id":"stop-1","method":"engine.stop"}`,
		`{"protocol":1,"id":"shutdown-1","method":"host.shutdown"}`,
	}, "\n")
	var output bytes.Buffer

	if err := New(strings.NewReader(input), &output, Metadata{}).Run(context.Background()); err != nil {
		t.Fatalf("Run() failed: %v", err)
	}
	messages := decodeMessages(t, output.String())
	if len(messages) != 8 {
		t.Fatalf("message count = %d, want 8\n%s", len(messages), output.String())
	}
	status := resultObject(t, messages[2])
	if status["policy"] != "off" {
		t.Fatalf("DNS status = %#v", status)
	}
	errorObject, ok := messages[3]["error"].(map[string]any)
	if !ok || errorObject["code"] != "dns_failed" {
		t.Fatalf("DNS failure = %#v", messages[3])
	}
}

func TestServerStartsAndReportsTUNTCPPoolMode(t *testing.T) {
	input := strings.Join([]string{
		`{"protocol":1,"id":"start-1","method":"engine.start","params":{"mode":"tun_tcp_pool","adapters":[{"name":"loopback","source_ip":"127.0.0.1"}],"channels":[{"name":"nic_ethernet","port":0,"adapter_names":["loopback"]},{"name":"nic_wifi","port":0,"adapter_names":["loopback"]},{"name":"aggregation","port":0,"adapter_names":["loopback"]}]}}`,
		`{"protocol":1,"id":"status-1","method":"engine.status"}`,
		`{"protocol":1,"id":"dns-status-1","method":"dns.status"}`,
		`{"protocol":1,"id":"stop-1","method":"engine.stop"}`,
		`{"protocol":1,"id":"shutdown-1","method":"host.shutdown"}`,
	}, "\n")
	var output bytes.Buffer

	if err := New(strings.NewReader(input), &output, Metadata{}).Run(context.Background()); err != nil {
		t.Fatalf("Run() failed: %v", err)
	}
	messages := decodeMessages(t, output.String())
	start := resultObject(t, messages[0])
	if start["mode"] != "tun_tcp_pool" {
		t.Fatalf("start mode = %#v", start["mode"])
	}
	endpoints := start["endpoints"].(map[string]any)
	channels, ok := endpoints["channels"].(map[string]any)
	if !ok || len(channels) != 3 {
		t.Fatalf("channel endpoints = %#v", endpoints["channels"])
	}
	status := resultObject(t, messages[2])
	pool := status["proxy"].(map[string]any)
	if pool["mode"] != "tun_tcp_pool" {
		t.Fatalf("status mode = %#v", pool["mode"])
	}
	dnsStatus := resultObject(t, messages[3])
	if dnsStatus["policy"] != "auto" {
		t.Fatalf("TUN DNS status = %#v", dnsStatus)
	}
}

func decodeMessages(t *testing.T, output string) []map[string]any {
	t.Helper()
	var messages []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		var message map[string]any
		if err := json.Unmarshal([]byte(line), &message); err != nil {
			t.Fatalf("invalid output JSON %q: %v", line, err)
		}
		messages = append(messages, message)
	}
	return messages
}

func resultObject(t *testing.T, message map[string]any) map[string]any {
	t.Helper()
	result, ok := message["result"].(map[string]any)
	if !ok {
		t.Fatalf("result = %#v", message["result"])
	}
	return result
}
