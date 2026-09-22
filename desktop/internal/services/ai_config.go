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
	BaseURL  string `json:"base_url"`
	Model    string `json:"model"`
	HasKey   bool   `json:"has_key"`
}

type aiStoredConfig struct {
	Config AIConfig `json:"config"`
	Key    string   `json:"key"`
}

func validateAIConfig(c AIConfig) (AIConfig, error) {
	c.BaseURL = strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	c.Model = strings.TrimSpace(c.Model)
	if c.Protocol != "openai" && c.Protocol != "anthropic" {
		return c, errors.New("请选择 OpenAI 兼容或 Anthropic 协议")
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
	if key == "" && !clearKey && c.BaseURL == s.config.Config.BaseURL && c.Protocol == s.config.Config.Protocol {
		next.Key = s.config.Key
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
	return next.Config, nil
}

func (s *AIService) loadConfig() {
	s.config.Config = AIConfig{Protocol: "openai", BaseURL: "https://api.openai.com/v1"}
	data, err := os.ReadFile(filepath.Join(s.directory, "provider.bin"))
	if os.IsNotExist(err) {
		return
	}
	if err == nil {
		data, err = protectAIData(data, false)
	}
	if err == nil {
		err = json.Unmarshal(data, &s.config)
	}
	if err != nil {
		s.state.Error = "AI 配置无法读取，请重新配置模型。"
	}
}
