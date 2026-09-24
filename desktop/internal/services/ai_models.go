package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

type AIModel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ListModels uses the form values without saving them or sending chat history.
func (s *AIService) ListModels(c AIConfig, key string, clearKey bool) ([]AIModel, error) {
	c.Model = "model-discovery"
	c, err := validateAIConfig(c)
	if err != nil {
		return nil, err
	}
	if len(key) > 8192 {
		return nil, errors.New("密钥过长")
	}
	s.mu.Lock()
	if strings.TrimSpace(key) == "" && !clearKey && sameAIEndpoint(c, s.config.Config) {
		key = s.config.Key
	}
	s.mu.Unlock()
	if clearKey {
		key = ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	models := []AIModel{}
	seen := map[string]bool{}
	cursor := ""
	for page := 0; page < 20; page++ {
		endpoint := aiEndpoint(c, "models")
		if cursor != "" {
			endpoint += "?after_id=" + url.QueryEscape(cursor)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, errors.New("API 地址无效")
		}
		aiSetAuth(req, c, key)
		resp, err := aiHTTPClient.Do(req)
		if err != nil {
			return nil, errors.New("无法读取模型列表，请检查 API 地址、网络和证书；也可以手动填写模型名称")
		}
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024+1))
		resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("模型列表返回 HTTP %d；请检查密钥和接口支持情况，也可以手动填写模型名称", resp.StatusCode)
		}
		if readErr != nil || len(data) > 2*1024*1024 {
			return nil, errors.New("模型列表响应过大或读取失败")
		}
		var wire struct {
			Data []struct {
				ID   string `json:"id"`
				Name string `json:"display_name"`
			} `json:"data"`
			HasMore bool   `json:"has_more"`
			LastID  string `json:"last_id"`
		}
		if json.Unmarshal(data, &wire) != nil {
			return nil, errors.New("服务未返回兼容的模型列表，可以手动填写模型名称")
		}
		for _, model := range wire.Data {
			if model.ID == "" || len(model.ID) > 200 || seen[model.ID] {
				continue
			}
			seen[model.ID] = true
			name := model.Name
			if name == "" || len(name) > 300 {
				name = model.ID
			}
			models = append(models, AIModel{ID: model.ID, Name: name})
			if len(models) > 5000 {
				return nil, errors.New("模型列表过大，请手动填写模型名称")
			}
		}
		if !wire.HasMore {
			if len(models) == 0 {
				return nil, errors.New("该 API 未返回可选模型，请检查权限或手动填写")
			}
			sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
			return models, nil
		}
		if wire.LastID == "" || wire.LastID == cursor {
			return nil, errors.New("模型列表分页无效，请手动填写模型名称")
		}
		cursor = wire.LastID
	}
	return nil, errors.New("模型列表页数过多，请手动填写模型名称")
}
