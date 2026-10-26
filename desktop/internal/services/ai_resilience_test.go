package services

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func fastAIRetries(t *testing.T) {
	t.Helper()
	delays, maxWait := aiRetryDelays, aiRetryMaxWait
	aiRetryDelays = []time.Duration{time.Millisecond, time.Millisecond}
	aiRetryMaxWait = time.Second
	t.Cleanup(func() { aiRetryDelays, aiRetryMaxWait = delays, maxWait })
}

func TestAITruncatedOutputIsMarkedAndToolCallsRefused(t *testing.T) {
	cases := []struct {
		name, protocol, body string
		wantErr              bool
	}{
		{"openai text", "openai", `{"choices":[{"finish_reason":"length","message":{"content":"partial"}}]}`, false},
		{"openai tool", "openai", `{"choices":[{"finish_reason":"length","message":{"tool_calls":[{"id":"a","function":{"name":"start","arguments":"{}"}}]}}]}`, true},
		{"anthropic text", "anthropic", `{"stop_reason":"max_tokens","content":[{"type":"text","text":"partial"}]}`, false},
		{"anthropic tool", "anthropic", `{"stop_reason":"max_tokens","content":[{"type":"tool_use","id":"a","name":"start","input":{}}]}`, true},
		{"anthropic empty", "anthropic", `{"stop_reason":"max_tokens","content":[{"type":"thinking","thinking":"..."}]}`, true},
		{"responses text", "responses", `{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output":[{"type":"message","content":[{"type":"output_text","text":"partial"}]}]}`, false},
		{"responses tool", "responses", `{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output":[{"type":"function_call","call_id":"a","name":"start","arguments":"{}"}]}`, true},
		{"responses filtered", "responses", `{"status":"incomplete","incomplete_details":{"reason":"content_filter"},"output":[{"type":"message","content":[{"type":"output_text","text":"partial"}]}]}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, tc.body) }))
			defer server.Close()
			c := aiStoredConfig{Config: AIConfig{Protocol: tc.protocol, BaseURL: server.URL, Model: "test"}}
			result, err := aiComplete(context.Background(), c, []aiMessage{{Role: "user", Content: "hi"}}, aiTools(), aiToolAuto)
			if tc.wantErr {
				if err == nil {
					t.Fatal("truncated response accepted", result)
				}
				return
			}
			if err != nil || !strings.HasPrefix(result.Content, "partial") || !strings.HasSuffix(result.Content, aiTruncatedNotice) {
				t.Fatal(result.Content, err)
			}
		})
	}
}

func TestAIProviderRetriesTransientFailures(t *testing.T) {
	fastAIRetries(t)
	cases := []struct {
		name       string
		status     func(hit int32) int
		retryAfter string
		hits       int32
		ok         bool
		retried    bool
	}{
		{"recovers after 503", func(hit int32) int {
			if hit < 3 {
				return 503
			}
			return 200
		}, "", 3, true, false},
		{"gives up on persistent 429", func(int32) int { return 429 }, "", 3, false, true},
		{"no retry on 401", func(int32) int { return 401 }, "", 1, false, false},
		{"no retry on 400", func(int32) int { return 400 }, "", 1, false, false},
		{"long Retry-After not waited", func(int32) int { return 429 }, "120", 1, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var hits int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				if !json.Valid(body) {
					t.Error("retried request lost its body")
				}
				status := tc.status(atomic.AddInt32(&hits, 1))
				if tc.retryAfter != "" {
					w.Header().Set("Retry-After", tc.retryAfter)
				}
				w.WriteHeader(status)
				if status == 200 {
					_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"done"}}]}`)
				}
			}))
			defer server.Close()
			c := aiStoredConfig{Config: AIConfig{Protocol: "openai", BaseURL: server.URL, Model: "test"}}
			result, err := aiComplete(context.Background(), c, []aiMessage{{Role: "user", Content: "hi"}}, aiTools(), aiToolAuto)
			if got := atomic.LoadInt32(&hits); got != tc.hits {
				t.Fatalf("hits = %d, want %d", got, tc.hits)
			}
			if tc.ok != (err == nil && result.Content == "done") {
				t.Fatal(result, err)
			}
			if err != nil && tc.retried != strings.Contains(err.Error(), "已自动重试 2 次") {
				t.Fatal(err)
			}
		})
	}
}

func TestAIRetryStopsOnCancellation(t *testing.T) {
	delays := aiRetryDelays
	aiRetryDelays = []time.Duration{time.Hour}
	t.Cleanup(func() { aiRetryDelays = delays })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	c := aiStoredConfig{Config: AIConfig{Protocol: "openai", BaseURL: server.URL, Model: "test"}}
	if _, err := aiComplete(ctx, c, []aiMessage{{Role: "user", Content: "hi"}}, aiTools(), aiToolAuto); err != context.DeadlineExceeded {
		t.Fatal(err)
	}
}

func TestAIAnthropicPromptCachingAndToolChoiceNone(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Error("bad request")
		}
		system, ok := body["system"].([]any)
		if !ok || len(system) != 1 || system[0].(map[string]any)["cache_control"] == nil {
			t.Error("system prompt not cacheable", body["system"])
		}
		messages := body["messages"].([]any)
		last := messages[len(messages)-1].(map[string]any)
		blocks := last["content"].([]any)
		// Tool results and the following instruction share one user turn.
		if last["role"] != "user" || len(blocks) != 2 || blocks[0].(map[string]any)["type"] != "tool_result" {
			t.Error("instruction not merged after tool result", last)
		}
		if blocks[len(blocks)-1].(map[string]any)["cache_control"] == nil {
			t.Error("newest block not cacheable")
		}
		if choice, _ := body["tool_choice"].(map[string]any); choice["type"] != "none" {
			t.Error("tool_choice none missing", body["tool_choice"])
		}
		_, _ = io.WriteString(w, `{"stop_reason":"end_turn","content":[{"type":"text","text":"summary"}]}`)
	}))
	defer server.Close()
	call := aiToolCall{ID: "one", Type: "function"}
	call.Function.Name, call.Function.Arguments = "get_status", "{}"
	messages := []aiMessage{{Role: "user", Content: "check"}, {Role: "assistant", Calls: []aiToolCall{call}}, {Role: "tool", ToolID: "one", Content: "{}"}, {Role: "user", Content: aiRoundLimitPrompt}}
	c := aiStoredConfig{Config: AIConfig{Protocol: "anthropic", BaseURL: server.URL, Model: "test"}}
	if result, err := aiComplete(context.Background(), c, messages, aiTools(), aiToolNone); err != nil || result.Content != "summary" {
		t.Fatal(result, err)
	}
}

func TestAIToolChoiceNoneForOpenAIProtocols(t *testing.T) {
	if body := aiResponsesBody("m", nil, aiTools(), aiToolNone); body["tool_choice"] != "none" {
		t.Fatal(body["tool_choice"])
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["tool_choice"] != "none" {
			t.Error("tool_choice none missing", body["tool_choice"])
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"done"}}]}`)
	}))
	defer server.Close()
	c := aiStoredConfig{Config: AIConfig{Protocol: "openai", BaseURL: server.URL, Model: "test"}}
	if _, err := aiComplete(context.Background(), c, []aiMessage{{Role: "user", Content: "hi"}}, aiTools(), aiToolNone); err != nil {
		t.Fatal(err)
	}
}

func TestAIRoundLimitEndsWithToolFreeSummary(t *testing.T) {
	for _, mode := range []string{"summary", "ignores tool_choice", "summary fails"} {
		t.Run(mode, func(t *testing.T) {
			s := testAIService(t)
			rounds := 0
			s.complete = func(_ context.Context, _ aiStoredConfig, m []aiMessage, _ []aiTool, choice aiToolChoice) (aiMessage, error) {
				call := aiToolCall{ID: aiID(), Type: "function"}
				call.Function.Name, call.Function.Arguments = "get_capabilities", "{}"
				if choice != aiToolNone {
					rounds++
					return aiMessage{Role: "assistant", Calls: []aiToolCall{call}}, nil
				}
				if last := m[len(m)-1]; last.Role != "user" || last.Content != aiRoundLimitPrompt {
					t.Error("summary prompt missing", last)
				}
				switch mode {
				case "ignores tool_choice":
					return aiMessage{Role: "assistant", Calls: []aiToolCall{call}}, nil
				case "summary fails":
					return aiMessage{}, io.ErrUnexpectedEOF
				}
				return aiMessage{Role: "assistant", Content: "阶段总结"}, nil
			}
			err := s.run(context.Background(), aiStoredConfig{}, []aiMessage{{Role: "user", Content: "hi"}})
			if rounds != 12 {
				t.Fatal("rounds", rounds)
			}
			tools := 0
			for _, e := range s.Snapshot().Entries {
				if e.Role == "tool" {
					tools++
				}
			}
			if tools != 12 {
				t.Fatal("summary round executed tools", tools)
			}
			if mode != "summary" {
				if err == nil || !strings.Contains(err.Error(), "12 次") {
					t.Fatal(err)
				}
				return
			}
			entries := s.Snapshot().Entries
			if err != nil || len(entries) < 2 || entries[len(entries)-2].Text != "阶段总结" || entries[len(entries)-1].Role != "notice" {
				t.Fatal(err, entries)
			}
		})
	}
}
