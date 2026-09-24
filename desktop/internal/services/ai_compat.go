package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

func aiSetAuth(req *http.Request, c AIConfig, key string) {
	if c.Protocol == "anthropic" {
		req.Header.Set("anthropic-version", "2023-06-01")
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	mode := c.AuthMode
	if mode == "" || mode == "auto" {
		mode = "bearer"
		if c.Protocol == "anthropic" {
			mode = "x-api-key"
		}
	}
	if mode == "x-api-key" {
		req.Header.Set("x-api-key", key)
	} else {
		req.Header.Set("Authorization", "Bearer "+key)
	}
}

// Do not echo upstream bodies: they can contain credentials or conversation data.
func aiHTTPError(status int) error {
	hint := "请检查协议、模型和服务状态"
	switch status {
	case 400, 422:
		hint = "请求格式或模型能力不兼容，请检查接口协议并选择支持工具调用的模型；推理模型可尝试 Responses 协议"
	case 401, 403:
		hint = "认证失败或无权限，请检查密钥和认证方式；Anthropic 中转站可能需要 Bearer"
	case 404, 405:
		hint = "接口或模型不存在，请检查 API 地址、协议和模型 ID"
	case 429:
		hint = "服务限流或额度不足，请稍后重试或检查账户额度"
	default:
		if status >= 500 {
			hint = "模型服务暂时不可用，请稍后重试"
		}
	}
	return fmt.Errorf("模型服务返回 HTTP %d；%s", status, hint)
}

func aiResponsesBody(model string, messages []aiMessage, tools []aiTool, forceTool bool) map[string]any {
	input := []any{}
	for _, m := range messages {
		if m.Role == "tool" {
			input = append(input, map[string]any{"type": "function_call_output", "call_id": m.ToolID, "output": m.Content})
		} else if m.Role == "assistant" && len(m.Blocks) > 0 {
			for _, block := range m.Blocks {
				input = append(input, block)
			}
		} else {
			input = append(input, map[string]any{"role": m.Role, "content": m.Content})
		}
	}
	definitions := []any{}
	for _, t := range tools {
		definitions = append(definitions, map[string]any{"type": "function", "name": t.Name, "description": t.Description, "parameters": t.Schema, "strict": false})
	}
	body := map[string]any{"model": model, "instructions": aiSystemPrompt + aiReplyGuidance, "input": input, "tools": definitions, "store": false, "include": []string{"reasoning.encrypted_content"}}
	if forceTool {
		body["tool_choice"] = map[string]any{"type": "function", "name": tools[0].Name}
	}
	return body
}

func aiParseResponses(data []byte) (aiMessage, error) {
	result := aiMessage{Role: "assistant"}
	var wire struct {
		Status string            `json:"status"`
		Error  json.RawMessage   `json:"error"`
		Output []json.RawMessage `json:"output"`
	}
	if json.Unmarshal(data, &wire) != nil || len(wire.Output) == 0 {
		return result, errors.New("模型未返回有效 Responses 消息，请检查接口协议")
	}
	if (wire.Status != "" && wire.Status != "completed") || (len(wire.Error) > 0 && string(wire.Error) != "null") {
		return result, errors.New("模型响应未完成，本次工具调用未执行；请检查服务状态或输出限制")
	}
	result.Blocks = wire.Output
	for _, raw := range wire.Output {
		var item struct {
			Type      string `json:"type"`
			CallID    string `json:"call_id"`
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
			Content   []struct {
				Type    string `json:"type"`
				Text    string `json:"text"`
				Refusal string `json:"refusal"`
			} `json:"content"`
		}
		if json.Unmarshal(raw, &item) != nil {
			return result, errors.New("Responses 输出格式无效")
		}
		switch item.Type {
		case "function_call":
			call := aiToolCall{ID: item.CallID, Type: "function"}
			call.Function.Name, call.Function.Arguments = item.Name, item.Arguments
			result.Calls = append(result.Calls, call)
		case "message":
			for _, b := range item.Content {
				if b.Type == "output_text" {
					result.Content += b.Text
				}
				if b.Type == "refusal" {
					result.Content += b.Refusal
				}
			}
		}
	}
	return result, nil
}
