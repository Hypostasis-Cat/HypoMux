package server

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
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
