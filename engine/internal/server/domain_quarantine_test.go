package server

import (
	"bytes"
	"context"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Hypostasis-Cat/HypoMux/engine/internal/proxy"
)

func TestDomainQuarantineUpdateWithoutEngineIsNoOp(t *testing.T) {
	s := New(bytes.NewReader(nil), io.Discard, Metadata{Name: "test"})
	response, _ := s.handle(context.Background(), RoleDesktop, []byte(`{"protocol":1,"id":"dq","method":"domain_quarantine.update","params":{"expiry":false,"clear":true}}`))
	if response.Error != nil {
		t.Fatal(response.Error)
	}
	result, ok := response.Result.(proxy.DomainQuarantineUpdateResult)
	if !ok || result.Applied {
		t.Fatalf("result = %#v", response.Result)
	}
}

func TestDomainQuarantineUpdateChangesRunningEngine(t *testing.T) {
	input := strings.Join([]string{
		`{"protocol":1,"id":"start","method":"engine.start","params":{"mode":"proxy","socks_port":0,"http_port":0,"domain_isolation":true,"domain_isolation_expiry":false,"adapters":[{"name":"loopback","source_ip":"127.0.0.1"}],"domain_quarantines":[{"adapter":"loopback","domain":"a.example.com","expires_at":"2126-01-01T00:00:00Z"},{"adapter":"loopback","domain":"b.example.com","expires_at":"2126-01-01T00:00:00Z"}]}}`,
		`{"protocol":1,"id":"seeded","method":"engine.telemetry"}`,
		`{"protocol":1,"id":"remove","method":"domain_quarantine.update","params":{"remove":[{"adapter":"loopback","domain":"a.example.com"}]}}`,
		`{"protocol":1,"id":"removed","method":"engine.telemetry"}`,
		`{"protocol":1,"id":"expiry","method":"domain_quarantine.update","params":{"expiry":true}}`,
		`{"protocol":1,"id":"limited","method":"engine.telemetry"}`,
		`{"protocol":1,"id":"clear","method":"domain_quarantine.update","params":{"clear":true}}`,
		`{"protocol":1,"id":"cleared","method":"engine.telemetry"}`,
		`{"protocol":1,"id":"shutdown","method":"host.shutdown"}`,
	}, "\n")
	var output bytes.Buffer
	if err := New(strings.NewReader(input), &output, Metadata{}).Run(context.Background()); err != nil {
		t.Fatalf("Run() failed: %v", err)
	}
	results := make(map[string]map[string]any)
	for _, message := range decodeMessages(t, output.String()) {
		if id, ok := message["id"].(string); ok {
			results[id] = resultObject(t, message)
		}
	}
	domains := func(id string) []string {
		t.Helper()
		items, _ := results[id]["domain_quarantines"].([]any)
		names := make([]string, 0, len(items))
		for _, item := range items {
			names = append(names, item.(map[string]any)["domain"].(string))
		}
		return names
	}
	if got := domains("seeded"); !slices.Equal(got, []string{"a.example.com", "b.example.com"}) {
		t.Fatalf("seeded = %v", got)
	}
	if results["seeded"]["domain_isolation_expiry"] != false {
		t.Fatalf("seeded policy = %#v", results["seeded"]["domain_isolation_expiry"])
	}
	if results["remove"]["applied"] != true {
		t.Fatalf("remove result = %#v", results["remove"])
	}
	if got := domains("removed"); !slices.Equal(got, []string{"b.example.com"}) {
		t.Fatalf("removed = %v", got)
	}
	if results["limited"]["domain_isolation_expiry"] != true {
		t.Fatalf("limited policy = %#v", results["limited"]["domain_isolation_expiry"])
	}
	limited, _ := results["limited"]["domain_quarantines"].([]any)
	if len(limited) != 1 {
		t.Fatalf("limited = %#v", limited)
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, limited[0].(map[string]any)["expires_at"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if remaining := time.Until(expiresAt); remaining <= 0 || remaining > 31*time.Minute {
		t.Fatalf("remaining quarantine lifetime = %v", remaining)
	}
	if got := domains("cleared"); len(got) != 0 {
		t.Fatalf("cleared = %v", got)
	}
}

func TestDomainQuarantineUpdateRejectsInvalidParams(t *testing.T) {
	s := New(bytes.NewReader(nil), io.Discard, Metadata{Name: "test"})
	for _, params := range []string{
		`"not an object"`,
		`{"remove":[{"adapter":" ","domain":"example.com"}]}`,
		`{"remove":[{"adapter":"a","domain":" "}]}`,
	} {
		response, _ := s.handle(context.Background(), RoleDesktop, []byte(`{"protocol":1,"id":"dq","method":"domain_quarantine.update","params":`+params+`}`))
		if response.Error == nil || response.Error.Code != "invalid_params" {
			t.Fatalf("params %s: response = %#v", params, response)
		}
	}
}
