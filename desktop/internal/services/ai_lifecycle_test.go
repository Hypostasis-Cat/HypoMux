package services

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func lifecycleAI(t *testing.T) *AIService {
	t.Helper()
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	s := NewAIService(NewSettingsService(), nil, nil, nil, nil, nil, nil)
	t.Cleanup(s.Shutdown)
	return s
}

func setLifecycleAI(t *testing.T, s *AIService, enabled bool) {
	t.Helper()
	if _, err := s.settings.UpdateFields(AppSettings{AIEnabled: enabled}, []string{"ai_enabled"}); err != nil {
		t.Fatal(err)
	}
}

func awaitAIResult[T any](t *testing.T, result <-chan T) T {
	t.Helper()
	select {
	case value := <-result:
		return value
	case <-time.After(3 * time.Second):
		t.Fatal("AI activity did not stop")
		var zero T
		return zero
	}
}

func TestAIEnabledPersistenceAndLegacyDefault(t *testing.T) {
	s := lifecycleAI(t)
	if !s.settings.Get().AIEnabled {
		t.Fatal("fresh settings must retain existing AI behaviour")
	}
	stale := s.settings.Get()
	setLifecycleAI(t, s, false)
	if _, err := s.settings.UpdateFields(stale, []string{"language"}); err != nil {
		t.Fatal(err)
	}
	if s.settings.Get().AIEnabled || NewSettingsService().Get().AIEnabled {
		t.Fatal("unrelated save or restart re-enabled AI")
	}
	restarted := NewAIService(NewSettingsService(), nil, nil, nil, nil, nil, nil)
	defer restarted.Shutdown()
	if err := restarted.Send("hello"); !errors.Is(err, errAIDisabled) {
		t.Fatalf("disabled startup accepted chat: %v", err)
	}
	if err := os.WriteFile(s.settings.path, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	if !NewSettingsService().Get().AIEnabled {
		t.Fatal("older settings without ai_enabled must default to enabled")
	}
}

func TestAIDisabledRejectsAllWorkAndPreservesConfiguration(t *testing.T) {
	s := lifecycleAI(t)
	c := AIConfig{Protocol: "openai", BaseURL: "https://example.com/v1", Model: "test"}
	if _, err := s.SaveConfig(c, "saved-key", false); err != nil {
		t.Fatal(err)
	}
	s.addEntry(AIEntry{Role: "user", Text: "keep history"})
	before := s.config
	history := s.Snapshot().Entries
	setLifecycleAI(t, s, false)
	checks := []func() error{
		func() error { return s.Send("hello") },
		func() error { return s.SendWithContext("hello", `{}`) },
		func() error { _, err := s.TestConnection(); return err },
		func() error { _, err := s.ListModels(c, "", false); return err },
		func() error { _, err := s.EnableMCP(17863, true); return err },
		func() error {
			_, err := s.invoke(context.Background(), "assistant", "get_capabilities", json.RawMessage(`{}`))
			return err
		},
		func() error {
			_, err := s.invoke(context.Background(), "mcp", "set_rule", json.RawMessage(`{"match_type":"process","value":"game.exe","outbound":"direct"}`))
			return err
		},
		func() error { return s.Decide("expired", true) },
		func() error { _, err := s.SaveConfig(c, "new-key", false); return err },
		func() error { return s.ClearHistory() },
	}
	for i, check := range checks {
		if err := check(); !errors.Is(err, errAIDisabled) {
			t.Errorf("entry point %d: expected disabled, got %v", i, err)
		}
	}
	if !reflect.DeepEqual(s.config, before) || !reflect.DeepEqual(s.Snapshot().Entries, history) {
		t.Fatal("turning AI off changed provider credentials or history")
	}
	setLifecycleAI(t, s, true)
	if _, err := s.invoke(context.Background(), "assistant", "get_capabilities", json.RawMessage(`{}`)); err != nil {
		t.Fatalf("re-enabled AI unusable: %v", err)
	}
	if s.config.Key != "saved-key" || s.MCPStatus().Enabled {
		t.Fatal("re-enable lost credentials or automatically opened external access")
	}
}

func TestAIDisableCancelsChatWithoutExecutingLateTools(t *testing.T) {
	s := lifecycleAI(t)
	s.config.Config = AIConfig{Protocol: "openai", BaseURL: "https://example.com/v1", Model: "test"}
	started, stopped := make(chan struct{}), make(chan struct{})
	s.complete = func(ctx context.Context, _ aiStoredConfig, _ []aiMessage, _ []aiTool, _ bool) (aiMessage, error) {
		close(started)
		<-ctx.Done()
		close(stopped)
		call := aiToolCall{ID: "late", Type: "function"}
		call.Function.Name, call.Function.Arguments = "get_capabilities", `{}`
		return aiMessage{Role: "assistant", Content: "late reply", Calls: []aiToolCall{call}}, nil
	}
	if err := s.Send("hello"); err != nil {
		t.Fatal(err)
	}
	awaitAIResult(t, started)
	setLifecycleAI(t, s, false)
	awaitAIResult(t, stopped)
	for deadline := time.Now().Add(3 * time.Second); s.Snapshot().Running; {
		if time.Now().After(deadline) {
			t.Fatal("chat remained running")
		}
		time.Sleep(time.Millisecond)
	}
	for _, e := range s.Snapshot().Entries {
		if e.Role == "tool" || e.Text == "late reply" {
			t.Fatal("cancelled chat continued after disable")
		}
	}
}

func TestAIDisableCancelsConnectionTestAndModelDiscovery(t *testing.T) {
	for _, operation := range []string{"test", "models"} {
		t.Run(operation, func(t *testing.T) {
			s := lifecycleAI(t)
			started, stopped, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				io.Copy(io.Discard, r.Body)
				close(started)
				<-r.Context().Done()
				close(stopped)
			}))
			defer server.Close()
			s.config.Config = AIConfig{Protocol: "openai", BaseURL: server.URL, Model: "test"}
			go func() {
				var err error
				if operation == "test" {
					_, err = s.TestConnection()
				} else {
					_, err = s.ListModels(s.config.Config, "", false)
				}
				done <- err
			}()
			awaitAIResult(t, started)
			setLifecycleAI(t, s, false)
			if awaitAIResult(t, done) == nil {
				t.Fatal("cancelled request reported success")
			}
			awaitAIResult(t, stopped)
		})
	}
}

func TestAIDisableRevokesApprovalsAndClosesMCP(t *testing.T) {
	s := lifecycleAI(t)
	// Select a free local port without modifying any system network settings.
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	connection, err := s.EnableMCP(port, false)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get(connection.Status.URL)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	done := make(chan error, 1)
	go func() { _, err := s.invoke(context.Background(), "mcp", "stop", json.RawMessage(`{}`)); done <- err }()
	id := waitAIApproval(t, s)
	setLifecycleAI(t, s, false)
	if awaitAIResult(t, done) == nil {
		t.Fatal("pending tool ran without approval")
	}
	if s.Snapshot().Pending != 0 || s.MCPStatus().Enabled {
		t.Fatal("MCP or approvals remained active")
	}
	if s.Decide(id, true) == nil {
		t.Fatal("old approval remained valid")
	}
	client := &http.Client{Timeout: time.Second}
	if resp, err := client.Get(connection.Status.URL); err == nil {
		resp.Body.Close()
		t.Fatal("MCP listener remained open")
	}
	setLifecycleAI(t, s, true)
	if s.Decide(id, true) == nil || s.MCPStatus().Enabled {
		t.Fatal("re-enable resurrected old access")
	}
	reconnected, err := s.EnableMCP(port, true)
	if err != nil || reconnected.Token == connection.Token {
		t.Fatalf("manual reconnection did not rotate credentials: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, reconnected.Status.URL, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+connection.Token)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatal("previous MCP token remained valid")
	}
}

func TestAIFailedSettingWriteDoesNotDisableRuntime(t *testing.T) {
	s := lifecycleAI(t)
	s.settings.path = filepath.Join(t.TempDir(), "directory")
	if err := os.Mkdir(s.settings.path, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := s.settings.UpdateFields(AppSettings{AIEnabled: false}, []string{"ai_enabled"}); err == nil {
		t.Fatal("expected persistence failure")
	}
	if !s.settings.Get().AIEnabled || s.disabled {
		t.Fatal("failed save changed runtime or settings")
	}
}

func TestAIDisableWaitsForNativeToolAndBlocksFurtherExecution(t *testing.T) {
	s := lifecycleAI(t)
	entered, release := make(chan struct{}), make(chan struct{})
	s.diagnostics = &DiagnosticsService{listAdapters: func() ([]AdapterView, error) {
		close(entered)
		<-release
		return nil, nil
	}}
	toolDone := make(chan error, 1)
	go func() {
		_, err := s.invoke(context.Background(), "assistant", "run_diagnostics", json.RawMessage(`{"adapter_ids":["missing"]}`))
		toolDone <- err
	}()
	awaitAIResult(t, entered)
	disabled := make(chan error, 1)
	go func() {
		_, err := s.settings.UpdateFields(AppSettings{AIEnabled: false}, []string{"ai_enabled"})
		disabled <- err
	}()
	select {
	case err := <-disabled:
		close(release)
		awaitAIResult(t, toolDone)
		t.Fatalf("disabled before native tool completed: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	close(release)
	awaitAIResult(t, toolDone)
	if err := awaitAIResult(t, disabled); err != nil {
		t.Fatal(err)
	}
	if _, err := s.invoke(context.Background(), "assistant", "get_capabilities", json.RawMessage(`{}`)); !errors.Is(err, errAIDisabled) {
		t.Fatalf("tool ran after disable commit: %v", err)
	}
}

func TestAINetworkMigrationDoesNotReenableAI(t *testing.T) {
	s := lifecycleAI(t)
	profile := t.TempDir()
	t.Setenv("USERPROFILE", profile)
	legacy := filepath.Join(profile, ".hypomux")
	if err := os.MkdirAll(legacy, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "config.json"), []byte(`{"run_mode":"tun"}`), 0600); err != nil {
		t.Fatal(err)
	}
	setLifecycleAI(t, s, false)
	if migrated, err := s.settings.MigrateLegacy(); err != nil || migrated.AIEnabled || !s.disabled {
		t.Fatalf("migration changed opt-out: %+v %v", migrated, err)
	}
	setLifecycleAI(t, s, true)
	if _, err := s.settings.MigrateLegacy(); err != nil {
		t.Fatal(err)
	}
	setLifecycleAI(t, s, false)
	if restored, err := s.settings.RollbackLegacyMigration(); err != nil || restored.AIEnabled || !s.disabled {
		t.Fatalf("rollback changed opt-out: %+v %v", restored, err)
	}
}
