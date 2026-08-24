package services

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFetchClashConnectionDetails(t *testing.T) {
	started := time.Now().UTC().Truncate(time.Millisecond)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/connections" || request.Header.Get("Authorization") != "Bearer test-secret" {
			http.Error(writer, "unauthorized", http.StatusUnauthorized)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(writer, `{"connections":[{"metadata":{"host":"example.com","destinationIP":"203.0.113.9","destinationPort":443,"processPath":"C:\\Apps\\browser.exe"},"start":%q}]}`, started.Format(time.RFC3339Nano))
	}))
	defer server.Close()

	got := fetchClashConnectionDetails(
		context.Background(),
		clashAPIConfig{Endpoint: strings.TrimPrefix(server.URL, "http://"), Secret: "test-secret"},
		[]connectionTelemetry{{ID: 10, Target: "203.0.113.9:443", StartedAt: started}},
	)
	if got[10].Process != "browser.exe" || got[10].Domain != "example.com" {
		t.Fatalf("connection details = %#v", got)
	}
}

func TestMatchClashConnectionsUsesTargetAndStartTime(t *testing.T) {
	started := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	core := []connectionTelemetry{
		{ID: 7, Target: "example.com:443", StartedAt: started.Add(30 * time.Millisecond)},
		{ID: 8, Target: "203.0.113.9:443", StartedAt: started.Add(2 * time.Second)},
	}
	clash := []clashConnection{
		{Metadata: clashConnectionMetadata{Host: "example.com", DestinationPort: "443", ProcessPath: `C:\Program Files\Browser\browser.exe`}, Start: started},
		{Metadata: clashConnectionMetadata{DestinationIP: "203.0.113.9", DestinationPort: "443", Process: "game.exe"}, Start: started.Add(2 * time.Second)},
	}
	got := matchClashConnections(core, clash)
	if got[7].Process != "browser.exe" || got[7].Domain != "example.com" || got[8].Process != "game.exe" {
		t.Fatalf("matched connection details = %#v", got)
	}
}

func TestMatchClashConnectionsDoesNotReuseOneFlow(t *testing.T) {
	started := time.Now().UTC()
	core := []connectionTelemetry{
		{ID: 1, Target: "example.com:443", StartedAt: started},
		{ID: 2, Target: "example.com:443", StartedAt: started.Add(time.Second)},
	}
	clash := []clashConnection{{
		Metadata: clashConnectionMetadata{Host: "example.com", DestinationPort: "443", Process: "browser.exe"},
		Start:    started,
	}}
	got := matchClashConnections(core, clash)
	if got[1].Process != "browser.exe" {
		t.Fatalf("first connection details = %#v", got)
	}
	if _, exists := got[2]; exists {
		t.Fatalf("one Clash flow was reused: %#v", got)
	}
}

func TestMatchClashConnectionsKeepsDomainWithoutProcess(t *testing.T) {
	started := time.Now().UTC()
	got := matchClashConnections(
		[]connectionTelemetry{{ID: 4, Target: "203.0.113.9:443", StartedAt: started}},
		[]clashConnection{{
			Metadata: clashConnectionMetadata{
				Host: "WWW.Example.COM.", DestinationIP: "203.0.113.9", DestinationPort: "443",
			},
			Start: started,
		}},
	)
	if got[4].Process != "" || got[4].Domain != "www.example.com" {
		t.Fatalf("domain-only connection details = %#v", got)
	}
}
