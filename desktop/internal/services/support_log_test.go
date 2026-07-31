package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSupportLogKeepsThreeSessionsAndRedactsSecrets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "app.log")
	store := newSupportLogStore(path)
	for index := 0; index < 4; index++ {
		if !store.Start("proxy", []string{"Ethernet"}, map[string]any{
			"token": "secret-value",
			"path":  `C:\Users\Alice\HypoMux`,
		}) {
			t.Fatal("expected a new log session")
		}
		store.RecordEvent("test", "complete", map[string]any{"index": index})
		store.Finish("test")
	}
	snapshot := store.Snapshot()
	if len(snapshot.Sessions) != 3 {
		t.Fatalf("expected 3 sessions, got %d", len(snapshot.Sessions))
	}
	data, err := store.Raw()
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, "secret-value") || strings.Contains(text, `C:\Users\Alice`) {
		t.Fatalf("sensitive value was not redacted: %s", text)
	}
	if !strings.Contains(text, "[REDACTED]") || !strings.Contains(text, `%USERPROFILE%`) {
		t.Fatalf("redaction markers missing: %s", text)
	}
}

func TestSupportLogEnforcesFiveMiBLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	content := supportLogMarker + " id=large | started=2026-07-31T09:00:00+08:00 | mode=test ===\n" +
		"selected_adapters=Ethernet\n" + strings.Repeat("x", supportLogMaxBytes+4096)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	store := newSupportLogStore(path)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > supportLogMaxBytes {
		t.Fatalf("log size %d exceeds %d", info.Size(), supportLogMaxBytes)
	}
	data, _ := store.Raw()
	if !strings.Contains(string(data), supportLogTruncated) {
		t.Fatal("expected truncation marker")
	}
}

func TestSupportLogAggregatesRepeatedEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	store := newSupportLogStore(path)
	now := time.Date(2026, 7, 31, 10, 0, 0, 0, time.Local)
	store.now = func() time.Time { return now }
	if !store.Start("proxy", []string{"Ethernet"}, nil) {
		t.Fatal("expected a new log session")
	}
	for index := 0; index < 3; index++ {
		store.Record("event [socket-bind] ready", false)
	}
	store.Finish("test")

	data, err := store.Raw()
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Count(text, "[socket-bind] ready") != 1 {
		t.Fatalf("expected one retained event, got: %s", text)
	}
	if !strings.Contains(text, "socket-bind ready") {
		t.Fatalf("expected an aggregation summary, got: %s", text)
	}
}
