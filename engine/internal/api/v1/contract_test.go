package v1

import (
	"encoding/json"
	"os"
	"path/filepath"
	goruntime "runtime"
	"slices"
	"testing"

	"github.com/Hypostasis-Cat/HypoMux/engine/internal/diagnostic"
	"github.com/Hypostasis-Cat/HypoMux/engine/internal/protocol"
	"github.com/Hypostasis-Cat/HypoMux/engine/internal/proxy"
	engineRuntime "github.com/Hypostasis-Cat/HypoMux/engine/internal/runtime"
)

type contractManifest struct {
	Protocol        int      `json:"protocol"`
	Transport       string   `json:"transport"`
	MaxMessageBytes int      `json:"max_message_bytes"`
	States          []string `json:"states"`
	Methods         []struct {
		Name         string `json:"name"`
		Idempotency  string `json:"idempotency"`
		Privilege    string `json:"privilege"`
		Cancellation string `json:"cancellation"`
	} `json:"methods"`
	Events []struct {
		Name        string `json:"name"`
		Delivery    string `json:"delivery"`
		Coalescible bool   `json:"coalescible"`
	} `json:"events"`
	ErrorCodes []string `json:"error_codes"`
}

type contractFixture struct {
	Name    string          `json:"name"`
	Kind    string          `json:"kind"`
	Method  string          `json:"method,omitempty"`
	Message json.RawMessage `json:"message"`
}

func TestManifestMatchesCompiledProtocol(t *testing.T) {
	var manifest contractManifest
	readContractJSON(t, "manifest.json", &manifest)

	if manifest.Protocol != protocol.Version {
		t.Fatalf("manifest protocol = %d, compiled = %d", manifest.Protocol, protocol.Version)
	}
	if manifest.Transport != protocol.Transport {
		t.Fatalf("manifest transport = %q, compiled = %q", manifest.Transport, protocol.Transport)
	}
	if manifest.MaxMessageBytes != protocol.MaxMessageBytes {
		t.Fatalf(
			"manifest max_message_bytes = %d, compiled = %d",
			manifest.MaxMessageBytes,
			protocol.MaxMessageBytes,
		)
	}

	methods := make([]string, 0, len(manifest.Methods))
	for _, method := range manifest.Methods {
		methods = append(methods, method.Name)
		if method.Idempotency == "" || method.Privilege == "" || method.Cancellation == "" {
			t.Fatalf("method %q is missing operational semantics", method.Name)
		}
	}
	if !slices.Equal(methods, Capabilities()) {
		t.Fatalf("manifest methods = %#v, capabilities = %#v", methods, Capabilities())
	}

	wantStates := []string{
		string(engineRuntime.StateStopped),
		string(engineRuntime.StateStarting),
		string(engineRuntime.StateRunning),
		string(engineRuntime.StateDegraded),
		string(engineRuntime.StateStopping),
		string(engineRuntime.StateFailed),
	}
	if !slices.Equal(manifest.States, wantStates) {
		t.Fatalf("manifest states = %#v, compiled = %#v", manifest.States, wantStates)
	}

	wantEvents := []string{EventEngineStateChanged, EventHostExiting}
	events := make([]string, 0, len(manifest.Events))
	for _, event := range manifest.Events {
		events = append(events, event.Name)
		if event.Delivery == "" {
			t.Fatalf("event %q is missing delivery semantics", event.Name)
		}
	}
	if !slices.Equal(events, wantEvents) {
		t.Fatalf("manifest events = %#v, compiled = %#v", events, wantEvents)
	}
	if len(manifest.ErrorCodes) == 0 {
		t.Fatal("manifest must document structured error codes")
	}
}

func TestCanonicalFixturesDecodeIntoTransportDTOs(t *testing.T) {
	var fixtures []contractFixture
	readContractJSON(t, filepath.Join("fixtures", "messages.json"), &fixtures)

	requestMethods := make(map[string]bool)
	responseMethods := make(map[string]bool)
	events := make(map[string]bool)
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			var envelope struct {
				Protocol int             `json:"protocol"`
				Result   json.RawMessage `json:"result"`
				Event    string          `json:"event"`
				Data     json.RawMessage `json:"data"`
			}
			if err := json.Unmarshal(fixture.Message, &envelope); err != nil {
				t.Fatalf("decode envelope: %v", err)
			}
			if envelope.Protocol != protocol.Version {
				t.Fatalf("fixture protocol = %d", envelope.Protocol)
			}

			switch fixture.Kind {
			case "request":
				var request protocol.Request
				if err := json.Unmarshal(fixture.Message, &request); err != nil {
					t.Fatalf("decode request: %v", err)
				}
				if request.Method != fixture.Method {
					t.Fatalf("request method = %q, metadata = %q", request.Method, fixture.Method)
				}
				decodeRequestParams(t, request)
				requestMethods[request.Method] = true
			case "response":
				var response protocol.Response
				if err := json.Unmarshal(fixture.Message, &response); err != nil {
					t.Fatalf("decode response: %v", err)
				}
				if response.Error != nil || response.Result == nil {
					t.Fatalf("fixture is not a success response: %#v", response)
				}
				decodeResult(t, fixture.Method, envelope.Result)
				responseMethods[fixture.Method] = true
			case "error":
				var response protocol.Response
				if err := json.Unmarshal(fixture.Message, &response); err != nil {
					t.Fatalf("decode error response: %v", err)
				}
				if response.Error == nil || response.Error.Code == "" || response.Error.Message == "" {
					t.Fatalf("invalid structured error: %#v", response.Error)
				}
			case "event":
				var event protocol.Event
				if err := json.Unmarshal(fixture.Message, &event); err != nil {
					t.Fatalf("decode event: %v", err)
				}
				if event.Sequence == 0 || event.Event == "" {
					t.Fatalf("invalid event envelope: %#v", event)
				}
				decodeEventData(t, event.Event, envelope.Data)
				events[event.Event] = true
			default:
				t.Fatalf("unknown fixture kind %q", fixture.Kind)
			}
		})
	}

	for _, method := range Capabilities() {
		if !requestMethods[method] {
			t.Errorf("missing canonical request fixture for %q", method)
		}
		if !responseMethods[method] {
			t.Errorf("missing canonical response fixture for %q", method)
		}
	}
	for _, event := range []string{EventEngineStateChanged, EventHostExiting} {
		if !events[event] {
			t.Errorf("missing canonical event fixture for %q", event)
		}
	}
}

func decodeRequestParams(t *testing.T, request protocol.Request) {
	t.Helper()
	var target any
	switch request.Method {
	case MethodEngineStart:
		target = &EngineStartParams{}
	case MethodEngineTelemetry:
		target = &EngineTelemetryParams{}
	case MethodDiagnosticRun:
		target = &DiagnosticRunParams{}
	case MethodEngineHello, MethodEngineStatus, MethodEngineStop, MethodHealthCheck, MethodHostShutdown:
		if len(request.Params) != 0 {
			t.Fatalf("%s fixture must not have params", request.Method)
		}
		return
	default:
		t.Fatalf("fixture uses unadvertised method %q", request.Method)
	}
	if err := json.Unmarshal(request.Params, target); err != nil {
		t.Fatalf("decode %s params: %v", request.Method, err)
	}
}

func decodeResult(t *testing.T, method string, payload json.RawMessage) {
	t.Helper()
	var target any
	switch method {
	case MethodEngineHello:
		target = &HelloResult{}
	case MethodEngineStatus:
		target = &StatusResult{}
	case MethodEngineStart:
		target = &EngineStartResult{}
	case MethodEngineStop:
		target = &EngineStopResult{}
	case MethodEngineTelemetry:
		target = &proxy.TelemetrySnapshot{}
	case MethodHealthCheck:
		target = &HealthResult{}
	case MethodDiagnosticRun:
		target = &diagnostic.Result{}
	case MethodHostShutdown:
		target = &ShutdownResult{}
	default:
		t.Fatalf("fixture uses unadvertised response method %q", method)
	}
	if err := json.Unmarshal(payload, target); err != nil {
		t.Fatalf("decode %s result: %v", method, err)
	}
}

func decodeEventData(t *testing.T, event string, payload json.RawMessage) {
	t.Helper()
	var target any
	switch event {
	case EventEngineStateChanged:
		target = &engineRuntime.Snapshot{}
	case EventHostExiting:
		target = &HostExitingData{}
	default:
		t.Fatalf("fixture uses undocumented event %q", event)
	}
	if err := json.Unmarshal(payload, target); err != nil {
		t.Fatalf("decode %s data: %v", event, err)
	}
}

func readContractJSON(t *testing.T, relative string, target any) {
	t.Helper()
	_, source, _, ok := goruntime.Caller(0)
	if !ok {
		t.Fatal("locate contract test source")
	}
	path := filepath.Join(filepath.Dir(source), "..", "..", "..", "..", "protocol", "v1", relative)
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read contract %s: %v", path, err)
	}
	if err := json.Unmarshal(payload, target); err != nil {
		t.Fatalf("decode contract %s: %v", path, err)
	}
}
