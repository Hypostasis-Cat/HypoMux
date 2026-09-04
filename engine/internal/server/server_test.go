package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	api "github.com/Hypostasis-Cat/HypoMux/engine/internal/api/v1"
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

func TestIPv6AddressSetupErrorClassifierDoesNotTreatTimeoutAsWFP(t *testing.T) {
	for _, message := range []string{
		"set ipv6 address: Element not found",
		"sing-box: ipv6 interface address element not found",
	} {
		if !isIPv6AddressSetupError(errors.New(message)) {
			t.Fatalf("expected IPv6 setup error: %q", message)
		}
	}
	for _, message := range []string{
		"ordinary upstream timeout",
		"set ipv4 address: Element not found",
		"WFP strict route filter rejected",
	} {
		if isIPv6AddressSetupError(errors.New(message)) {
			t.Fatalf("unexpected IPv6 setup classification: %q", message)
		}
	}
}

type fakeTunController struct {
	mu              sync.Mutex
	status          tun.Status
	activateErr     error
	activateErrs    []error
	activateConfigs []tun.Config
	stopErr         error
	stopCalls       int
	onStop          func()
	onLog           func(string)
	onUnexpected    func(tun.Status)
	nextGeneration  uint64
}

const testTunExecutable = `C:\HypoMux\bin\sing-box.exe`

func testTunMetadata() Metadata {
	return Metadata{Name: "test", TunExecutable: testTunExecutable}
}

func testPinnedTunMetadata() Metadata {
	return Metadata{
		Name:                "test",
		TunExecutable:       testTunExecutable,
		TunExecutableSHA256: strings.Repeat("a", 64),
	}
}

func (f *fakeTunController) Activate(
	_ context.Context,
	config tun.Config,
) (tun.Status, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.activateConfigs = append(f.activateConfigs, config)
	activateErr := f.activateErr
	if len(f.activateErrs) > 0 {
		activateErr = f.activateErrs[0]
		f.activateErrs = f.activateErrs[1:]
	}
	if activateErr != nil {
		f.nextGeneration++
		f.status = tun.Status{
			State:      tun.StateFailed,
			Generation: f.nextGeneration,
			LastError:  activateErr.Error(),
		}
		return f.status, activateErr
	}
	now := time.Now().UTC()
	f.nextGeneration++
	f.status = tun.Status{
		State:      tun.StateRunning,
		Generation: f.nextGeneration,
		PID:        1234,
		StartedAt:  &now,
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
		testTunMetadata(),
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
	if len(controller.activateConfigs) != 1 ||
		controller.activateConfigs[0].Executable != testTunExecutable {
		t.Fatalf("Core did not pin the TUN executable: %#v", controller.activateConfigs)
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
		testTunMetadata(),
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
			"params":{"executable":"C:\\HypoMux\\bin\\sing-box.exe","config_path":"y"}
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

func TestTunActivateRejectsExecutableOutsideCorePolicy(t *testing.T) {
	engineServer := New(
		strings.NewReader(""),
		&bytes.Buffer{},
		testTunMetadata(),
	)
	controller := &fakeTunController{status: tun.Status{State: tun.StateStopped}}
	engineServer.tun = controller
	startTUNPoolForLifecycleTest(t, engineServer)

	response, _ := engineServer.handle(
		context.Background(),
		[]byte(`{
			"protocol":1,
			"id":"tun-policy-reject",
			"method":"tun.activate",
			"params":{
				"executable":"C:\\Users\\Example\\payload.exe",
				"config_path":"C:\\Users\\Example\\payload.json"
			}
		}`),
	)
	if response.Error == nil || response.Error.Code != "security_policy_rejected" {
		t.Fatalf("untrusted executable response = %#v", response)
	}
	if len(controller.activateConfigs) != 0 {
		t.Fatalf("untrusted executable reached the privileged supervisor: %#v", controller.activateConfigs)
	}
	if engineServer.proxy == nil || !engineServer.proxy.Running() ||
		engineServer.runtime.Snapshot().State != engineRuntime.StateRunning {
		t.Fatalf("policy rejection mutated the prepared engine state: %#v", engineServer.runtime.Snapshot())
	}
	_ = engineServer.stopProxy("clear-policy-rejection")
}

func TestTunActivateRequiresConfigDigestForInstalledServicePolicy(t *testing.T) {
	engineServer := New(
		strings.NewReader(""),
		&bytes.Buffer{},
		testPinnedTunMetadata(),
	)
	controller := &fakeTunController{status: tun.Status{State: tun.StateStopped}}
	engineServer.tun = controller
	startTUNPoolForLifecycleTest(t, engineServer)
	response, _ := engineServer.handle(
		context.Background(),
		[]byte(`{
			"protocol":1,
			"id":"tun-config-pin",
			"method":"tun.activate",
			"params":{
				"executable":"C:\\HypoMux\\bin\\sing-box.exe",
				"config_path":"C:\\Users\\Example\\sing-box.json"
			}
		}`),
	)
	if response.Error == nil || response.Error.Code != "security_policy_rejected" {
		t.Fatalf("unpinned configuration response = %#v", response)
	}
	if len(controller.activateConfigs) != 0 {
		t.Fatalf("unpinned configuration reached the privileged supervisor: %#v", controller.activateConfigs)
	}
	_ = engineServer.stopProxy("clear-config-pin-rejection")
}

func TestTunActivatePinsConfigDigestForInstalledServicePolicy(t *testing.T) {
	engineServer := New(
		strings.NewReader(""),
		&bytes.Buffer{},
		testPinnedTunMetadata(),
	)
	controller := &fakeTunController{status: tun.Status{State: tun.StateStopped}}
	engineServer.tun = controller
	startTUNPoolForLifecycleTest(t, engineServer)
	digest := strings.Repeat("b", 64)
	request := fmt.Sprintf(`{
		"protocol":1,
		"id":"tun-config-pinned",
		"method":"tun.activate",
		"params":{
			"executable":"C:\\HypoMux\\bin\\sing-box.exe",
			"config_path":"C:\\Users\\Example\\sing-box.json",
			"config_sha256":"%s"
		}
	}`, digest)
	response, _ := engineServer.handle(context.Background(), []byte(request))
	if response.Error != nil {
		t.Fatalf("pinned configuration was rejected: %#v", response.Error)
	}
	if len(controller.activateConfigs) != 1 ||
		controller.activateConfigs[0].ConfigSHA256 != digest ||
		controller.activateConfigs[0].ExecutableSHA256 != engineServer.metadata.TunExecutableSHA256 ||
		!controller.activateConfigs[0].RequireProtectedConfig {
		t.Fatalf("security pins were not passed to the supervisor: %#v", controller.activateConfigs)
	}
	_ = engineServer.stopProxy("clear-config-pin")
}

func TestTunActivateUsesProtectedPinnedExecutableWhenDesktopAssertsAppCopy(t *testing.T) {
	metadata := testPinnedTunMetadata()
	metadata.TunExecutable = `C:\ProgramData\HypoMux\Core\bin\sing-box.exe`
	engineServer := New(strings.NewReader(""), &bytes.Buffer{}, metadata)
	controller := &fakeTunController{status: tun.Status{State: tun.StateStopped}}
	engineServer.tun = controller
	startTUNPoolForLifecycleTest(t, engineServer)
	digest := strings.Repeat("b", 64)
	request := fmt.Sprintf(`{
		"protocol":1,
		"id":"tun-protected-copy",
		"method":"tun.activate",
		"params":{
			"executable":"D:\\Apps\\HypoMux\\bin\\sing-box.exe",
			"config_path":"C:\\Users\\Example\\sing-box.json",
			"config_sha256":"%s"
		}
	}`, digest)
	response, _ := engineServer.handle(context.Background(), []byte(request))
	if response.Error != nil {
		t.Fatalf("protected executable override was rejected: %#v", response.Error)
	}
	if len(controller.activateConfigs) != 1 ||
		controller.activateConfigs[0].Executable != metadata.TunExecutable ||
		controller.activateConfigs[0].ExecutableSHA256 != metadata.TunExecutableSHA256 {
		t.Fatalf("protected executable policy was not enforced: %#v", controller.activateConfigs)
	}
	_ = engineServer.stopProxy("clear-protected-copy")
}

func TestTunActivateRetriesWithIPv4OnlyConfigAfterIPv6SetupFailure(t *testing.T) {
	engineServer := New(
		strings.NewReader(""),
		&bytes.Buffer{},
		testTunMetadata(),
	)
	controller := &fakeTunController{
		status:       tun.Status{State: tun.StateStopped},
		activateErrs: []error{errors.New("set ipv6 address: Element not found"), nil},
	}
	engineServer.tun = controller
	startTUNPoolForLifecycleTest(t, engineServer)

	response, _ := engineServer.handle(
		context.Background(),
		[]byte(`{
			"protocol":1,
			"id":"tun-activate",
			"method":"tun.activate",
			"params":{
				"executable":"C:\\HypoMux\\bin\\sing-box.exe",
				"config_path":"C:\\Users\\Example\\singbox-dual-stack.json",
				"ipv4_fallback_config_path":"C:\\Users\\Example\\singbox-ipv4.json",
				"startup_timeout_ms":1500
			}
		}`),
	)
	if response.Error != nil {
		t.Fatalf("IPv4-only fallback failed: %#v", response.Error)
	}
	result, ok := response.Result.(api.TunLifecycleResult)
	if !ok || !result.IPv4OnlyFallback || result.Tun.State != tun.StateRunning {
		t.Fatalf("fallback result = %#v", response.Result)
	}
	if len(controller.activateConfigs) != 2 ||
		controller.activateConfigs[0].ConfigPath != `C:\Users\Example\singbox-dual-stack.json` ||
		controller.activateConfigs[1].ConfigPath != `C:\Users\Example\singbox-ipv4.json` {
		t.Fatalf("activation configs = %#v", controller.activateConfigs)
	}
	if controller.stopCalls != 1 {
		t.Fatalf("IPv4-only retry did not clean the failed attempt: stopCalls=%d", controller.stopCalls)
	}
	_ = engineServer.stopProxy("clear-fallback")
}

func TestTunActivateRecoversStaleWintunAdapterOnce(t *testing.T) {
	engineServer := New(
		strings.NewReader(""),
		&bytes.Buffer{},
		testTunMetadata(),
	)
	controller := &fakeTunController{
		status: tun.Status{State: tun.StateStopped},
		activateErrs: []error{
			errors.New("create adapter: Cannot create a file when that file already exists. | open existing adapter: Element not found."),
			nil,
		},
	}
	engineServer.tun = controller
	startTUNPoolForLifecycleTest(t, engineServer)

	response, _ := engineServer.handle(
		context.Background(),
		[]byte(`{
			"protocol":1,
			"id":"tun-activate",
			"method":"tun.activate",
			"params":{
				"executable":"C:\\HypoMux\\bin\\sing-box.exe",
				"config_path":"C:\\Users\\Example\\singbox.json",
				"startup_timeout_ms":20000
			}
		}`),
	)
	if response.Error != nil {
		t.Fatalf("stale Wintun recovery failed: %#v", response.Error)
	}
	result, ok := response.Result.(api.TunLifecycleResult)
	if !ok || !result.RecoveredStaleAdapter || result.Tun.State != tun.StateRunning {
		t.Fatalf("stale Wintun recovery result = %#v", response.Result)
	}
	if len(controller.activateConfigs) != 2 || controller.stopCalls != 1 {
		t.Fatalf(
			"stale Wintun retry count: activations=%d stops=%d",
			len(controller.activateConfigs), controller.stopCalls,
		)
	}
	_ = engineServer.stopProxy("clear-stale-wintun")
}

func TestStaleTunAdapterErrorClassificationIsNarrow(t *testing.T) {
	for _, message := range []string{
		"create adapter: Cannot create a file when that file already exists.",
		"create adapter failed | open existing adapter: Element not found.",
		"stale HypoMux-Tun device still exists: SWD\\WINTUN\\123",
	} {
		if !isStaleTunAdapterError(errors.New(message)) {
			t.Fatalf("stale adapter error was not recognized: %q", message)
		}
	}
	for _, message := range []string{
		"ordinary upstream timeout",
		"set ipv6 address: Element not found",
		"WFP strict route filter rejected",
	} {
		if isStaleTunAdapterError(errors.New(message)) {
			t.Fatalf("unrelated error was classified as stale adapter: %q", message)
		}
	}
}

func TestUnexpectedManagedTunExitStopsPoolAndFailsEngine(t *testing.T) {
	engineServer := New(
		strings.NewReader(""),
		&bytes.Buffer{},
		Metadata{Name: "test"},
	)
	controller := &fakeTunController{
		status: tun.Status{State: tun.StateRunning, Generation: 7, PID: 1234},
	}
	engineServer.tun = controller
	startTUNPoolForLifecycleTest(t, engineServer)
	engineServer.activeTunGeneration = 7

	engineServer.handleTunUnexpectedExit(tun.Status{
		State:      tun.StateFailed,
		Generation: 7,
		LastError:  "synthetic crash",
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

func TestStaleTunExitCannotStopNewProxyGeneration(t *testing.T) {
	engineServer := New(
		strings.NewReader(""),
		&bytes.Buffer{},
		Metadata{Name: "test"},
	)
	controller := &fakeTunController{}
	engineServer.tun = controller
	startTUNPoolForLifecycleTest(t, engineServer)
	engineServer.activeTunGeneration = 12

	engineServer.handleTunUnexpectedExit(tun.Status{
		State:      tun.StateFailed,
		Generation: 11,
		LastError:  "stale generation crash",
	})
	if engineServer.proxy == nil || !engineServer.proxy.Running() ||
		engineServer.runtime.Snapshot().State != engineRuntime.StateRunning {
		t.Fatalf(
			"stale TUN callback stopped the new pool: proxy=%v state=%s",
			engineServer.proxy, engineServer.runtime.Snapshot().State,
		)
	}
	_ = engineServer.stopProxy("clear-stale-generation")
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

type blockingReadCloser struct {
	readStarted  chan struct{}
	readReturned chan struct{}
	closed       chan struct{}
	closeOnce    sync.Once
}

func (r *blockingReadCloser) Read([]byte) (int, error) {
	select {
	case r.readStarted <- struct{}{}:
	default:
	}
	<-r.closed
	select {
	case r.readReturned <- struct{}{}:
	default:
	}
	return 0, io.EOF
}

func (r *blockingReadCloser) Close() error {
	r.closeOnce.Do(func() { close(r.closed) })
	return nil
}

func TestRunCancellationInterruptsBlockedInputRead(t *testing.T) {
	input := &blockingReadCloser{
		readStarted:  make(chan struct{}, 1),
		readReturned: make(chan struct{}, 1),
		closed:       make(chan struct{}),
	}
	engineServer := New(input, io.Discard, Metadata{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- engineServer.Run(ctx) }()

	select {
	case <-input.readStarted:
	case <-time.After(time.Second):
		t.Fatal("Run did not reach the input read")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run() = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not return after cancellation")
	}
	select {
	case <-input.readReturned:
	case <-time.After(time.Second):
		t.Fatal("the blocked scanner read was not interrupted after cancellation")
	}
}

func TestRunExitReleasesQueuedRequestReaders(t *testing.T) {
	for _, test := range []struct {
		name    string
		method  string
		output  io.Writer
		wantErr bool
	}{
		{name: "shutdown", method: "host.shutdown", output: io.Discard},
		{name: "write failure", method: "engine.status", output: failingSessionWriter{}, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Keep the service context alive across sessions, as the Windows
			// service does. Closing a transport cannot unblock a channel send.
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			before := countSessionReaders()
			for i := 0; i < 8; i++ {
				input := io.NopCloser(strings.NewReader(fmt.Sprintf(
					"{\"protocol\":1,\"id\":\"first\",\"method\":%q}\n"+
						"{\"protocol\":1,\"id\":\"queued\",\"method\":\"engine.status\"}\n", test.method,
				)))
				err := New(input, test.output, Metadata{}).Run(ctx)
				_ = input.Close()
				if (err != nil) != test.wantErr {
					t.Fatalf("Run() = %v, wantErr=%v", err, test.wantErr)
				}
			}
			deadline := time.Now().Add(2 * time.Second)
			for countSessionReaders() > before && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
			if remaining := countSessionReaders() - before; remaining > 0 {
				t.Fatalf("%d scanner goroutines remain after 8 completed sessions", remaining)
			}
			if ctx.Err() != nil {
				t.Fatal("ending a session must not cancel the service context")
			}
		})
	}
}

func countSessionReaders() int {
	buffer := make([]byte, 1024*1024)
	n := runtime.Stack(buffer, true)
	// Match Run's closures without depending on compiler-assigned numbering.
	return strings.Count(string(buffer[:n]), "server.(*Server).Run.func")
}

type failingSessionWriter struct{}

func (failingSessionWriter) Write([]byte) (int, error) {
	return 0, io.ErrClosedPipe
}

func TestRunShutdownClosesInputWithBlockedRead(t *testing.T) {
	blocked := &blockingReadCloser{
		readStarted: make(chan struct{}, 1), readReturned: make(chan struct{}, 1), closed: make(chan struct{}),
	}
	defer blocked.Close()
	input := &sessionReadCloser{
		Reader: io.MultiReader(strings.NewReader("{\"protocol\":1,\"id\":\"shutdown\",\"method\":\"host.shutdown\"}\n"), blocked),
		Closer: blocked,
	}
	// Hold the response until the scanner is blocked waiting for more input.
	output := &waitForSessionReadWriter{started: blocked.readStarted}
	if err := New(input, output, Metadata{}).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-blocked.readReturned:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not close and release the blocked input read")
	}
}

type sessionReadCloser struct {
	io.Reader
	io.Closer
}

type waitForSessionReadWriter struct {
	started <-chan struct{}
	ready   bool
}

func (w *waitForSessionReadWriter) Write(data []byte) (int, error) {
	if !w.ready {
		select {
		case <-w.started:
			w.ready = true
		case <-time.After(time.Second):
			return 0, errors.New("scanner did not reach the blocked read")
		}
	}
	return len(data), nil
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
