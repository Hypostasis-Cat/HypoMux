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

func testAIService(t *testing.T) *AIService {
	t.Helper()
	return &AIService{directory: t.TempDir(), settings: &SettingsService{settings: DefaultSettings(), autostartEnabled: func() (bool, error) { return false, nil }}, approvals: map[string]aiApproval{}, complete: aiComplete, state: AISnapshot{Entries: []AIEntry{}}}
}

func TestAIModelDiscoveryCredentialsAndPagination(t *testing.T) {
	for _, protocol := range []string{"openai", "anthropic"} {
		t.Run(protocol, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "GET" || r.URL.Path != "/models" {
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				}
				if protocol == "openai" && r.Header.Get("Authorization") != "Bearer saved-secret" {
					t.Error("missing bearer credential")
				}
				if protocol == "anthropic" && (r.Header.Get("x-api-key") != "saved-secret" || r.Header.Get("anthropic-version") == "") {
					t.Error("missing Anthropic headers")
				}
				if r.URL.Query().Get("after_id") == "" {
					io.WriteString(w, `{"data":[{"id":"b","display_name":"Model B"}],"has_more":true,"last_id":"b"}`)
				} else {
					io.WriteString(w, `{"data":[{"id":"a"},{"id":"b"}],"has_more":false}`)
				}
			}))
			defer server.Close()
			s := testAIService(t)
			s.config = aiStoredConfig{Config: AIConfig{Protocol: protocol, BaseURL: server.URL, Model: "original"}, Key: "saved-secret"}
			models, err := s.ListModels(AIConfig{Protocol: protocol, BaseURL: server.URL}, "", false)
			if err != nil || len(models) != 2 || models[0].ID != "a" || calls != 2 {
				t.Fatalf("models=%+v err=%v calls=%d", models, err, calls)
			}
			if s.config.Config.Model != "original" {
				t.Fatal("discovery changed saved configuration")
			}
		})
	}
}

func TestAIModelDiscoveryDoesNotLeakSavedKeyToNewEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("saved key leaked to different endpoint")
		}
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, "secret-provider-error")
	}))
	defer server.Close()
	s := testAIService(t)
	s.config = aiStoredConfig{Config: AIConfig{Protocol: "openai", BaseURL: "https://original.example/v1"}, Key: "private"}
	_, err := s.ListModels(AIConfig{Protocol: "openai", BaseURL: server.URL}, "", false)
	if err == nil || strings.Contains(err.Error(), "secret-provider-error") {
		t.Fatal("unsafe provider error handling")
	}
}

func TestAIPageContextIsCapturedWithoutChangingUserText(t *testing.T) {
	s := testAIService(t)
	s.config.Config = AIConfig{Protocol: "openai", BaseURL: "https://example.com/v1", Model: "test"}
	received := make(chan []aiMessage, 1)
	s.complete = func(_ context.Context, _ aiStoredConfig, messages []aiMessage, _ []aiTool, _ bool) (aiMessage, error) {
		received <- messages
		return aiMessage{Role: "assistant", Content: "Checked"}, nil
	}
	page := `{"page":"routing","selected_rules":[{"value":"cs2.exe"}]}`
	if err := s.SendWithContext("make this direct", page); err != nil {
		t.Fatal(err)
	}
	select {
	case messages := <-received:
		last := messages[len(messages)-1]
		if last.Role != "user" || !strings.Contains(last.Content, page) || !strings.Contains(last.Content, "make this direct") {
			t.Fatalf("missing user-level context: %+v", last)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("model did not receive the message")
	}
	entry := s.Snapshot().Entries[0]
	if entry.Text != "make this direct" || entry.Context != page {
		t.Fatalf("context mixed into display text: %+v", entry)
	}
	// Wait for the worker to finish before the temporary history directory is removed.
	for deadline := time.Now().Add(2 * time.Second); s.Snapshot().Running; {
		if time.Now().After(deadline) {
			t.Fatal("worker did not finish")
		}
		time.Sleep(time.Millisecond)
	}
	if err := s.SendWithContext("hello", "not json"); err == nil {
		t.Fatal("invalid context accepted")
	}
}

func TestAIConfigEndpointValidation(t *testing.T) {
	for _, url := range []string{"http://example.com/v1", "https://user:pass@example.com", "https://example.com?key=secret", "file:///tmp", "https://example.com/#fragment"} {
		if _, err := validateAIConfig(AIConfig{Protocol: "openai", BaseURL: url, Model: "test"}); err == nil {
			t.Fatalf("accepted %s", url)
		}
	}
	for _, url := range []string{"https://example.com/v1", "http://127.0.0.1:1234/v1", "http://[::1]:1234/v1"} {
		if _, err := validateAIConfig(AIConfig{Protocol: "anthropic", BaseURL: url, Model: "test"}); err != nil {
			t.Fatal(err)
		}
	}
}
func TestAISecretNeverReturnedAndEndpointChangeClearsKey(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows DPAPI")
	}
	s := testAIService(t)
	c := AIConfig{Protocol: "openai", BaseURL: "https://example.com/v1", Model: "test"}
	if _, err := s.SaveConfig(c, "private-api-key", false); err != nil {
		t.Fatal(err)
	}
	visible, _ := json.Marshal(s.Config())
	if strings.Contains(string(visible), "private-api-key") {
		t.Fatal("secret exposed in config")
	}
	encrypted, err := os.ReadFile(filepath.Join(s.directory, "provider.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encrypted), "private-api-key") {
		t.Fatal("secret persisted as plaintext")
	}
	loaded := testAIService(t)
	loaded.directory = s.directory
	loaded.loadConfig()
	if loaded.config.Key != "private-api-key" {
		t.Fatal("encrypted configuration did not round trip")
	}
	if _, err = s.SaveConfig(c, "", false); err != nil || s.config.Key != "private-api-key" {
		t.Fatal("same endpoint should preserve key", err)
	}
	c.BaseURL = "https://other.example/v1"
	if _, err = s.SaveConfig(c, "", false); err != nil || s.config.Key != "" {
		t.Fatal("secret reused across endpoints", err)
	}
	if _, err = s.SaveConfig(c, "new-key", true); err != nil || s.config.Key != "" {
		t.Fatal("clear key must take precedence", err)
	}
}
func TestAIArgumentsRejectUnexpectedInput(t *testing.T) {
	for _, tc := range []struct{ name, raw string }{{"start", `{"mode":"tun"}`}, {"stop", `null`}, {"get_status", `[]`}, {"set_rule", `{"match_type":"shell","value":"whoami","outbound":"direct"}`}, {"set_rule", `{"match_type":"process","value":"cs2.exe","outbound":"direct","command":"whoami"}`}, {"remove_rule", `{"match_type":"process","value":"cs2.exe","outbound":"direct"}`}} {
		if _, err := parseAIArguments(tc.name, json.RawMessage(tc.raw)); err == nil {
			t.Fatalf("accepted invalid %s", tc.raw)
		}
	}
}
func TestAIProviderProtocolsAndToolResults(t *testing.T) {
	for _, protocol := range []string{"openai", "anthropic"} {
		t.Run(protocol, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if json.NewDecoder(r.Body).Decode(&body) != nil {
					t.Error("bad request")
				}
				if protocol == "anthropic" {
					if r.URL.Path != "/v1/messages" || r.Header.Get("x-api-key") != "secret" {
						t.Error("incorrect Anthropic request")
					}
					messages := body["messages"].([]any)
					last := messages[len(messages)-1].(map[string]any)
					if last["role"] != "user" || len(last["content"].([]any)) != 2 {
						t.Error("parallel results not grouped")
					}
					_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"done"}]}`)
				} else {
					if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer secret" {
						t.Error("incorrect OpenAI request")
					}
					messages := body["messages"].([]any)
					if messages[len(messages)-1].(map[string]any)["tool_call_id"] != "two" {
						t.Error("missing tool result ID")
					}
					_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"done"}}]}`)
				}
			}))
			defer server.Close()
			one := aiToolCall{ID: "one", Type: "function"}
			one.Function.Name = "get_status"
			one.Function.Arguments = "{}"
			two := one
			two.ID = "two"
			messages := []aiMessage{{Role: "user", Content: "check"}, {Role: "assistant", Calls: []aiToolCall{one, two}}, {Role: "tool", ToolID: "one", Content: "{}"}, {Role: "tool", ToolID: "two", Content: "{}"}}
			result, err := aiComplete(context.Background(), aiStoredConfig{Config: AIConfig{Protocol: protocol, BaseURL: server.URL + "/v1", Model: "test"}, Key: "secret"}, messages, aiTools(), false)
			if err != nil || result.Content != "done" {
				t.Fatal(result, err)
			}
		})
	}
}
func TestAIProviderDoesNotLeakErrorsOrFollowRedirects(t *testing.T) {
	targetHits := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targetHits++ }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) }))
	defer server.Close()
	c := aiStoredConfig{Config: AIConfig{Protocol: "openai", BaseURL: server.URL, Model: "test"}, Key: "very-secret"}
	_, err := aiComplete(context.Background(), c, []aiMessage{{Role: "user", Content: "hi"}}, aiTools(), false)
	if err == nil || targetHits != 0 || strings.Contains(err.Error(), "very-secret") {
		t.Fatal("redirect or credential leak", err)
	}
}
func TestAIProviderRejectsMalformedToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"message":{"tool_calls":[{"id":"a","function":{"name":"start","arguments":"not json"}}]}}]}`)
	}))
	defer server.Close()
	_, err := aiComplete(context.Background(), aiStoredConfig{Config: AIConfig{Protocol: "openai", BaseURL: server.URL, Model: "test"}}, nil, aiTools(), false)
	if err == nil {
		t.Fatal("accepted invalid model arguments")
	}
}
func waitAIApproval(t *testing.T, s *AIService) string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		for id := range s.approvals {
			s.mu.Unlock()
			return id
		}
		s.mu.Unlock()
		time.Sleep(time.Millisecond)
	}
	t.Fatal("approval never arrived")
	return ""
}
func TestAIApprovalRejectCancelAndStaleState(t *testing.T) {
	for _, mode := range []string{"deny", "cancel", "stale"} {
		t.Run(mode, func(t *testing.T) {
			s := testAIService(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { _, err := s.invoke(ctx, "assistant", "stop", json.RawMessage(`{}`)); done <- err }()
			id := waitAIApproval(t, s)
			switch mode {
			case "deny":
				if err := s.Decide(id, false); err != nil {
					t.Fatal(err)
				}
			case "cancel":
				cancel()
			case "stale":
				s.settings.mu.Lock()
				s.settings.settings.Mode = "proxy"
				s.settings.mu.Unlock()
				if err := s.Decide(id, true); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("operation must not run")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("operation did not stop")
			}
			if s.Snapshot().Pending != 0 {
				t.Fatal("leaked approval")
			}
			if s.Decide(id, true) == nil {
				t.Fatal("replayed approval accepted")
			}
		})
	}
}
func TestAIRunReturnsToolErrorsToModelWithoutExecutingUnknownTools(t *testing.T) {
	s := testAIService(t)
	calls := 0
	s.complete = func(ctx context.Context, c aiStoredConfig, m []aiMessage, tools []aiTool, force bool) (aiMessage, error) {
		calls++
		if calls == 1 {
			call := aiToolCall{ID: "unknown", Type: "function"}
			call.Function.Name = "execute_shell"
			call.Function.Arguments = `{}`
			return aiMessage{Role: "assistant", Calls: []aiToolCall{call}}, nil
		}
		if m[len(m)-1].Role != "tool" || !strings.Contains(m[len(m)-1].Content, "未知工具") {
			t.Error("missing tool error")
		}
		return aiMessage{Role: "assistant", Content: "Cannot run shell."}, nil
	}
	if err := s.run(context.Background(), aiStoredConfig{}, []aiMessage{{Role: "user", Content: "hi"}}); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatal(calls)
	}
}
func TestAIMCPSecurityAndProtocol(t *testing.T) {
	s := testAIService(t)
	m := &aiMCPServer{token: "secret", status: AIMCPStatus{URL: "http://127.0.0.1:17863/mcp", ReadOnly: true}, slots: make(chan struct{}, 2)}
	handler := s.mcpHandler(m)
	request := func(body, token, origin, host string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "http://127.0.0.1:17863/mcp", strings.NewReader(body))
		r.Host = host
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+token)
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	bare := httptest.NewRequest("POST", "http://127.0.0.1:17863/mcp", strings.NewReader(body))
	bare.Header.Set("Content-Type", "application/json")
	bare.Header.Set("Authorization", "secret")
	bareResponse := httptest.NewRecorder()
	handler.ServeHTTP(bareResponse, bare)
	if bareResponse.Code != http.StatusUnauthorized {
		t.Fatal("accepted token without Bearer authentication scheme")
	}
	if w := request(body, "wrong", "", "127.0.0.1:17863"); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if w := request(body, "secret", "https://evil.example", "127.0.0.1:17863"); w.Code != 403 {
		t.Fatal(w.Code)
	}
	if w := request(body, "secret", "", "evil.example"); w.Code != 403 {
		t.Fatal(w.Code)
	}
	w := request(body, "secret", "", "127.0.0.1:17863")
	if w.Code != 200 || strings.Contains(w.Body.String(), `"name":"stop"`) {
		t.Fatal("write tools available in read-only mode", w.Body.String())
	}
	w = request(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"stop","arguments":{}}}`, "secret", "", "127.0.0.1:17863")
	if !strings.Contains(w.Body.String(), "Tool unavailable") || s.Snapshot().Pending != 0 {
		t.Fatal("read-only write was not blocked")
	}
	w = request(`{"jsonrpc":"2.0","id":3,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`, "secret", "", "127.0.0.1:17863")
	if !strings.Contains(w.Body.String(), `"protocolVersion":"2025-06-18"`) {
		t.Fatal(w.Body.String())
	}
	w = request(`{"jsonrpc":"2.0","method":"notifications/initialized"}`, "secret", "", "127.0.0.1:17863")
	if w.Code != 202 {
		t.Fatal(w.Code)
	}
}
func TestAIRedactionPreservesRuleMeaning(t *testing.T) {
	result := aiRedactValue(map[string]any{"address": "192.168.1.20", "detail": `C:\Users\Alice\app failed at 10.1.1.1`, "value": "192.168.1.0/24"})
	b, _ := json.Marshal(result)
	text := string(b)
	if strings.Contains(text, "Alice") || strings.Contains(text, "10.1.1.1") || strings.Contains(text, "192.168.1.20") {
		t.Fatal(text)
	}
	if !strings.Contains(text, "192.168.1.0/24") {
		t.Fatal("rule meaning lost")
	}
}
func TestAIRoutingStaleSaveRejected(t *testing.T) {
	settings := &SettingsService{settings: DefaultSettings(), autostartEnabled: func() (bool, error) { return false, nil }}
	routing := &RoutingRuleService{settings: settings}
	old := routingRevision(settings.Get())
	settings.settings.RoutingRules = []RoutingRule{{MatchType: "process", Value: "cs2.exe", Outbound: "direct"}}
	if _, err := routing.SaveOrderedChecked(nil, []string{"process", "domain", "ip"}, old); err == nil {
		t.Fatal("stale draft overwrote new rule")
	}
	if len(settings.Get().RoutingRules) != 1 {
		t.Fatal("rule lost")
	}
}
func TestAIShutdownRejectsPendingAndFutureWork(t *testing.T) {
	s := testAIService(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := s.invoke(ctx, "mcp", "stop", json.RawMessage(`{}`)); done <- err }()
	waitAIApproval(t, s)
	s.Shutdown()
	if err := <-done; err == nil {
		t.Fatal("shutdown allowed pending write")
	}
	if _, err := s.invoke(ctx, "mcp", "get_status", json.RawMessage(`{}`)); err == nil {
		t.Fatal("accepted work after shutdown")
	}
}
func TestAICancelErrorIsHonest(t *testing.T) {
	if !strings.Contains(safeAIError(context.Canceled), "不会自动撤销") {
		t.Fatal("missing partial execution warning")
	}
}

func TestAIApprovedRulePatchPreservesOtherRules(t *testing.T) {
	s := testAIService(t)
	t.Setenv("HYPOMUX_DATA_DIR", s.directory)
	s.settings.path = filepath.Join(s.directory, "settings.json")
	s.settings.settings.RoutingRules = []RoutingRule{{MatchType: "process", Value: "other.exe", Outbound: "aggregation"}}
	s.adapters = NewAdapterService(s.settings)
	s.routing = NewRoutingRuleService(s.settings, s.adapters, nil)
	done := make(chan error, 1)
	go func() {
		_, err := s.invoke(context.Background(), "assistant", "set_rule", json.RawMessage(`{"match_type":"process","value":"cs2.exe","outbound":"direct"}`))
		done <- err
	}()
	id := waitAIApproval(t, s)
	if len(s.settings.Get().RoutingRules) != 1 {
		t.Fatal("modified rules before approval")
	}
	if err := s.Decide(id, true); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("approved operation did not complete")
	}
	rules := s.settings.Get().RoutingRules
	if len(rules) != 2 {
		t.Fatalf("expected existing and new rule: %#v", rules)
	}
	found := false
	for _, r := range rules {
		if r.Value == "cs2.exe" && r.Outbound == "direct" {
			found = true
		}
	}
	if !found || s.Snapshot().Revision != 1 {
		t.Fatal("rule or UI revision missing")
	}
	if err := s.Decide(id, true); err == nil {
		t.Fatal("approval replayed")
	}
}
