package services

import (
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type AIConfig struct {
	Protocol string `json:"protocol"`
	AuthMode string `json:"auth_mode,omitempty"`
	BaseURL  string `json:"base_url"`
	Model    string `json:"model"`
	HasKey   bool   `json:"has_key"`
}

type aiStoredConfig struct {
	Config    AIConfig `json:"config"`
	Key       string   `json:"key"`
	ContextID string   `json:"context_id,omitempty"`
}

func validateAIConfig(c AIConfig) (AIConfig, error) {
	c.BaseURL = strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	c.Model = strings.TrimSpace(c.Model)
	if c.Protocol != "openai" && c.Protocol != "anthropic" && c.Protocol != "responses" {
		return c, errors.New("请选择 Chat Completions、Responses 或 Anthropic Messages 协议")
	}
	if c.AuthMode == "auto" {
		c.AuthMode = ""
	}
	if c.AuthMode != "" && c.AuthMode != "bearer" && c.AuthMode != "x-api-key" {
		return c, errors.New("请选择有效的认证方式")
	}
	u, err := url.Parse(c.BaseURL)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return c, errors.New("API 地址必须是无凭据、查询参数的完整地址")
	}
	ip := net.ParseIP(u.Hostname())
	local := u.Hostname() == "localhost" || (ip != nil && ip.IsLoopback())
	if u.Scheme != "https" && !(u.Scheme == "http" && local) {
		return c, errors.New("远程 API 必须使用 HTTPS；本机模型可使用 HTTP")
	}
	if c.Model == "" || len(c.Model) > 200 {
		return c, errors.New("请输入有效的模型名称")
	}
	return c, nil
}

// Accept host roots, versioned/custom prefixes and complete endpoint URLs.
// An explicit operation URL preserves even a non-versioned gateway route.
func aiEndpoint(c AIConfig, operation string) string {
	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(c.BaseURL), "/"))
	if err != nil {
		return c.BaseURL
	}
	path := strings.TrimRight(u.EscapedPath(), "/")
	explicit := false
	for _, suffix := range []string{"/chat/completions", "/messages", "/responses", "/models"} {
		if strings.HasSuffix(path, suffix) {
			path = strings.TrimSuffix(path, suffix)
			explicit = true
			break
		}
	}
	if path == "" && !explicit {
		path = "/v1"
	}
	u.RawPath = path + "/" + operation
	u.Path, _ = url.PathUnescape(u.RawPath)
	return u.String()
}

func sameAIEndpoint(a, b AIConfig) bool {
	return a.Protocol == b.Protocol && aiEndpoint(a, "models") == aiEndpoint(b, "models")
}

func (s *AIService) Config() AIConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.config.Config
	c.HasKey = s.config.Key != ""
	return c
}

// Empty key preserves the existing secret only for the same endpoint and protocol.
func (s *AIService) SaveConfig(c AIConfig, key string, clearKey bool) (AIConfig, error) {
	c, err := validateAIConfig(c)
	if err != nil {
		return c, err
	}
	if len(key) > 8192 {
		return c, errors.New("密钥过长")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Running {
		return c, errors.New("请先停止当前对话任务")
	}
	next := aiStoredConfig{Config: c, Key: strings.TrimSpace(key)}
	if clearKey {
		next.Key = ""
	}
	if strings.TrimSpace(key) == "" && !clearKey && sameAIEndpoint(c, s.config.Config) {
		next.Key = s.config.Key
	}
	next.ContextID = s.config.ContextID
	if next.ContextID == "" || !sameAIEndpoint(c, s.config.Config) || c.Model != s.config.Config.Model || c.AuthMode != s.config.Config.AuthMode || next.Key != s.config.Key {
		next.ContextID = aiID()
	}
	next.Config.HasKey = next.Key != ""
	data, err := json.Marshal(next)
	if err != nil {
		return c, err
	}
	encrypted, err := protectAIData(data, true)
	if err != nil {
		return c, err
	}
	if err = atomicWriteFile(filepath.Join(s.directory, "provider.bin"), encrypted, 0600); err != nil {
		return c, err
	}
	s.config = next
	s.state.Error = ""
	return next.Config, nil
}

func (s *AIService) loadConfig() {
	s.config = aiStoredConfig{Config: AIConfig{Protocol: "openai", BaseURL: "https://api.openai.com/v1"}}
	data, err := os.ReadFile(filepath.Join(s.directory, "provider.bin"))
	if os.IsNotExist(err) {
		return
	}
	if err == nil {
		data, err = protectAIData(data, false)
	}
	var loaded aiStoredConfig
	if err == nil {
		err = json.Unmarshal(data, &loaded)
	}
	if err == nil {
		loaded.Config, err = validateAIConfig(loaded.Config)
	}
	if err != nil {
		s.state.Error = "AI 配置无法读取，请重新配置模型。"
		return
	}
	// Legacy records remain visible, but cannot be attributed to this provider.
	s.config = loaded
	if loaded.ContextID == "" {
		if _, err := s.SaveConfig(loaded.Config, loaded.Key, false); err != nil {
			s.config.ContextID = aiID()
			s.state.Error = "旧版 AI 配置迁移失败，请重新保存模型配置。"
		}
	}
}
