package services

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestAIEndpointForms(t *testing.T) {
	for _, tc := range []struct{ base, chat, models string }{
		{"https://relay.example", "/v1/chat/completions", "/v1/models"},
		{"https://relay.example/v1/", "/v1/chat/completions", "/v1/models"},
		{"https://relay.example/api/v2", "/api/v2/chat/completions", "/api/v2/models"},
		{"https://relay.example/tenant%2Fone/v1", "/tenant%2Fone/v1/chat/completions", "/tenant%2Fone/v1/models"},
		{"https://relay.example/api/v2/chat/completions", "/api/v2/chat/completions", "/api/v2/models"},
		{"https://relay.example/v1/messages", "/v1/chat/completions", "/v1/models"},
		{"https://relay.example/responses", "/chat/completions", "/models"},
	} {
		c := AIConfig{BaseURL: tc.base}
		if got := aiEndpoint(c, "chat/completions"); got != "https://relay.example"+tc.chat {
			t.Errorf("%s -> %s", tc.base, got)
		}
		if got := aiEndpoint(c, "models"); got != "https://relay.example"+tc.models {
			t.Errorf("%s -> %s", tc.base, got)
		}
	}
}

func TestAIRelayAuthForDiscoveryAndCompletion(t *testing.T) {
	for _, mode := range []string{"", "bearer", "x-api-key"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("anthropic-version") == "" {
					t.Error("missing version")
				}
				if mode == "bearer" {
					if r.Header.Get("Authorization") != "Bearer secret" || r.Header.Get("x-api-key") != "" {
						t.Error("wrong relay auth")
					}
				} else if r.Header.Get("x-api-key") != "secret" || r.Header.Get("Authorization") != "" {
					t.Error("wrong API key auth")
				}
				switch r.URL.Path {
				case "/v1/models":
					io.WriteString(w, `{"data":[{"id":"test"}]}`)
				case "/v1/messages":
					io.WriteString(w, `{"content":[{"type":"text","text":"ok"}]}`)
				default:
					t.Errorf("unexpected path %s", r.URL.Path)
				}
			}))
			defer server.Close()
			c := AIConfig{Protocol: "anthropic", AuthMode: mode, BaseURL: server.URL + "/v1/messages", Model: "test"}
			s := testAIService(t)
			if _, err := s.ListModels(c, "secret", false); err != nil {
				t.Fatal(err)
			}
			if _, err := aiComplete(context.Background(), aiStoredConfig{Config: c, Key: "secret"}, []aiMessage{{Role: "user", Content: "hi"}}, aiTools(), false); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAIReasoningSurvivesToolRoundTrip(t *testing.T) {
	for _, protocol := range []string{"openai", "anthropic", "responses"} {
		t.Run(protocol, func(t *testing.T) {
			hits := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits++
				data, _ := io.ReadAll(r.Body)
				if hits == 2 && !strings.Contains(string(data), "opaque-reasoning") {
					t.Error("reasoning lost on tool continuation")
				}
				if protocol == "responses" {
					var body map[string]any
					if json.Unmarshal(data, &body) != nil || body["store"] != false || body["messages"] != nil || r.URL.Path != "/v1/responses" {
						t.Error("bad Responses request")
					}
					if hits == 2 && !strings.Contains(string(data), `"call_id":"call-1"`) {
						t.Error("tool output not correlated")
					}
					if hits == 1 {
						io.WriteString(w, `{"status":"completed","output":[{"id":"rs1","type":"reasoning","summary":[],"encrypted_content":"opaque-reasoning"},{"id":"fc1","type":"function_call","call_id":"call-1","name":"get_status","arguments":"{}"}]}`)
					} else {
						io.WriteString(w, `{"status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}]}`)
					}
				} else if protocol == "anthropic" {
					if hits == 1 {
						io.WriteString(w, `{"content":[{"type":"thinking","thinking":"opaque-reasoning","signature":"signature"},{"type":"tool_use","id":"call-1","name":"get_status","input":{}}]}`)
					} else {
						io.WriteString(w, `{"content":[{"type":"text","text":"ok"}]}`)
					}
				} else {
					if hits == 1 {
						io.WriteString(w, `{"choices":[{"message":{"content":null,"reasoning_content":"opaque-reasoning","tool_calls":[{"type":"function","id":"call-1","function":{"name":"get_status","arguments":"{}"}}]}}]}`)
					} else {
						io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}]}`)
					}
				}
			}))
			defer server.Close()
			c := aiStoredConfig{Config: AIConfig{Protocol: protocol, BaseURL: server.URL, Model: "reasoner"}}
			messages := []aiMessage{{Role: "user", Content: "status"}}
			first, err := aiComplete(context.Background(), c, messages, aiTools(), false)
			if err != nil || len(first.Calls) != 1 {
				t.Fatalf("first=%+v err=%v", first, err)
			}
			messages = append(messages, first, aiMessage{Role: "tool", ToolID: first.Calls[0].ID, Content: "{}"})
			last, err := aiComplete(context.Background(), c, messages, aiTools(), false)
			if err != nil || last.Content != "ok" {
				t.Fatalf("last=%+v err=%v", last, err)
			}
		})
	}
}

func TestAISwitchIsolatesHistoryAcrossReload(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows DPAPI")
	}
	s := testAIService(t)
	c := AIConfig{Protocol: "openai", BaseURL: "https://old.example/v1", Model: "old"}
	if _, err := s.SaveConfig(c, "old-key", false); err != nil {
		t.Fatal(err)
	}
	s.addEntry(AIEntry{Role: "user", Text: "old-private-context"})
	oldID := s.config.ContextID
	// Equivalent URL retains both the key and context.
	c.BaseURL += "/chat/completions"
	if _, err := s.SaveConfig(c, "", false); err != nil {
		t.Fatal(err)
	}
	if s.config.ContextID != oldID || s.config.Key != "old-key" {
		t.Fatal("equivalent URL reset context/key")
	}
	c.Model = "new"
	if _, err := s.SaveConfig(c, "", false); err != nil {
		t.Fatal(err)
	}
	if s.config.ContextID == oldID || len(s.Snapshot().Entries) != 1 {
		t.Fatal("switch failed to isolate or deleted history")
	}
	loaded := testAIService(t)
	loaded.directory = s.directory
	loaded.loadConfig()
	loaded.state.Entries = s.Snapshot().Entries
	if loaded.config.ContextID != s.config.ContextID {
		t.Fatal("context ID did not persist")
	}
	received := make(chan []aiMessage, 1)
	loaded.complete = func(_ context.Context, _ aiStoredConfig, m []aiMessage, _ []aiTool, _ bool) (aiMessage, error) {
		received <- m
		return aiMessage{Role: "assistant", Content: "ok"}, nil
	}
	if err := loaded.Send("new question"); err != nil {
		t.Fatal(err)
	}
	select {
	case messages := <-received:
		if len(messages) != 1 || messages[0].Content != "new question" {
			t.Fatalf("old history replayed: %+v", messages)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("send timeout")
	}
	for deadline := time.Now().Add(2 * time.Second); loaded.Snapshot().Running; {
		if time.Now().After(deadline) {
			t.Fatal("worker timeout")
		}
		time.Sleep(time.Millisecond)
	}
	for _, change := range []func(){func() { c.BaseURL = "https://new.example/v1" }, func() { c.Protocol = "responses" }, func() { c.AuthMode = "x-api-key" }} {
		before := s.config.ContextID
		change()
		if _, err := s.SaveConfig(c, "new-key", false); err != nil {
			t.Fatal(err)
		}
		if before == s.config.ContextID {
			t.Fatal("provider identity change did not reset context")
		}
	}
	before := s.config.ContextID
	if _, err := s.SaveConfig(c, "rotated-key", false); err != nil {
		t.Fatal(err)
	}
	if before == s.config.ContextID {
		t.Fatal("credential change did not reset context")
	}
}

func TestAILegacyAndCorruptConfigRecovery(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows DPAPI")
	}
	s := testAIService(t)
	legacy := aiStoredConfig{Config: AIConfig{Protocol: "openai", BaseURL: "https://relay.example", Model: "test"}, Key: "secret"}
	data, _ := json.Marshal(legacy)
	encrypted, err := protectAIData(data, true)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(s.directory, "provider.bin"), encrypted, 0600); err != nil {
		t.Fatal(err)
	}
	s.loadConfig()
	id := s.config.ContextID
	if id == "" || s.config.Key != "secret" {
		t.Fatal("legacy migration failed")
	}
	s.loadConfig()
	if s.config.ContextID != id {
		t.Fatal("migration was not persisted")
	}
	if err = os.WriteFile(filepath.Join(s.directory, "provider.bin"), []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	s.config = aiStoredConfig{}
	s.loadConfig()
	if s.state.Error == "" {
		t.Fatal("corrupt config not reported")
	}
	if _, err = s.SaveConfig(legacy.Config, "new-secret", false); err != nil {
		t.Fatal(err)
	}
	if s.state.Error != "" {
		t.Fatal("stale error survived successful save")
	}
}

func TestAIIncompleteResponsesDoNotExecuteTools(t *testing.T) {
	_, err := aiParseResponses([]byte(`{"status":"incomplete","output":[{"type":"function_call","call_id":"a","name":"start","arguments":"{}"}]}`))
	if err == nil {
		t.Fatal("incomplete tool call accepted")
	}
}

func TestAIConnectionTestUsesAutomaticToolsAndValidatesNonce(t *testing.T) {
	for _, valid := range []bool{true, false} {
		s := testAIService(t)
		s.config.Config = AIConfig{Protocol: "openai", BaseURL: "https://relay.example", Model: "reasoner"}
		s.complete = func(_ context.Context, _ aiStoredConfig, messages []aiMessage, tools []aiTool, force bool) (aiMessage, error) {
			if force {
				t.Error("test forces a mode not used in real conversations")
			}
			nonce := strings.TrimPrefix(messages[0].Content, "Call connection_test with nonce ")
			if !valid {
				nonce = "wrong"
			}
			call := aiToolCall{ID: "test", Type: "function"}
			call.Function.Name = tools[0].Name
			args, _ := json.Marshal(map[string]string{"nonce": nonce})
			call.Function.Arguments = string(args)
			return aiMessage{Role: "assistant", Calls: []aiToolCall{call}}, nil
		}
		_, err := s.TestConnection()
		if (err == nil) != valid {
			t.Fatalf("valid=%v err=%v", valid, err)
		}
	}
}

func TestAIFailedSavePreservesContextAndCredentials(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows DPAPI")
	}
	s := testAIService(t)
	c := AIConfig{Protocol: "openai", BaseURL: "https://relay.example", Model: "one"}
	if _, err := s.SaveConfig(c, "original", false); err != nil {
		t.Fatal(err)
	}
	before := s.config
	// Make the target directory a regular file to force a persistence failure.
	blocked := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocked, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	s.directory = blocked
	c.Model = "two"
	if _, err := s.SaveConfig(c, "changed", false); err == nil {
		t.Fatal("save unexpectedly succeeded")
	}
	if s.config != before {
		t.Fatal("failed save changed live configuration")
	}
}
