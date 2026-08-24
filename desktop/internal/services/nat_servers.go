package services

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

const natServerAutoID = "auto"

var natServerHostPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?$`)

type NATServer struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Address string `json:"address"`
	BuiltIn bool   `json:"built_in"`
}

type NATServerSnapshot struct {
	SelectedID string      `json:"selected_id"`
	Servers    []NATServer `json:"servers"`
}

type natServerDocument struct {
	Version    int         `json:"version"`
	SelectedID string      `json:"selected_id"`
	Servers    []NATServer `json:"servers"`
}

type natServerStore struct {
	mu       sync.Mutex
	path     string
	selected string
	servers  []NATServer
}

func defaultNATServers() []NATServer {
	return []NATServer{
		{ID: "builtin-miwifi", Name: "小米 STUN", Address: "stun.miwifi.com:3478", BuiltIn: true},
		{ID: "builtin-stuntman", Name: "STUNTMAN 官方", Address: "stunserver2025.stunprotocol.org:3478", BuiltIn: true},
		{ID: "builtin-voipgate", Name: "Pion / VoIPGate", Address: "stun.voipgate.com:3478", BuiltIn: true},
	}
}

func newNATServerStore(path string) *natServerStore {
	store := &natServerStore{path: path, selected: natServerAutoID, servers: defaultNATServers()}
	_ = store.load()
	return store
}

func newDefaultNATServerStore() *natServerStore {
	return newNATServerStore(filepath.Join(settingsDirectory(), "nat_servers.json"))
}

func (s *natServerStore) snapshot() NATServerSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshotLocked()
}

func (s *natServerStore) snapshotLocked() NATServerSnapshot {
	return NATServerSnapshot{SelectedID: s.selected, Servers: append([]NATServer(nil), s.servers...)}
}

func (s *natServerStore) selectedServers(id string) ([]NATServer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(id) == "" {
		id = s.selected
	}
	if id == natServerAutoID {
		if len(s.servers) == 0 {
			return nil, errors.New("请先添加至少一台 STUN 服务器")
		}
		return append([]NATServer(nil), s.servers...), nil
	}
	for _, server := range s.servers {
		if server.ID == id {
			return []NATServer{server}, nil
		}
	}
	return nil, errors.New("所选 STUN 服务器已不存在")
}

func (s *natServerStore) selectServer(id string) (NATServerSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id != natServerAutoID {
		found := false
		for _, server := range s.servers {
			if server.ID == id {
				found = true
				break
			}
		}
		if !found {
			return s.snapshotLocked(), errors.New("所选 STUN 服务器已不存在")
		}
	}
	previous := s.selected
	s.selected = id
	if err := s.saveLocked(); err != nil {
		s.selected = previous
		return s.snapshotLocked(), err
	}
	return s.snapshotLocked(), nil
}

func (s *natServerStore) add(name string, address string) (NATServerSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	normalized, err := normalizeNATServerAddress(address)
	if err != nil {
		return s.snapshotLocked(), err
	}
	for _, server := range s.servers {
		if strings.EqualFold(server.Address, normalized) {
			return s.snapshotLocked(), errors.New("该 STUN 服务器已在列表中")
		}
	}
	name = strings.TrimSpace(name)
	if name == "" {
		host, _, _ := net.SplitHostPort(normalized)
		name = host
	}
	if len([]rune(name)) > 40 {
		return s.snapshotLocked(), errors.New("服务器名称不能超过 40 个字符")
	}
	hash := sha256.Sum256([]byte(normalized))
	server := NATServer{ID: "custom-" + hex.EncodeToString(hash[:6]), Name: name, Address: normalized}
	s.servers = append(s.servers, server)
	if err := s.saveLocked(); err != nil {
		s.servers = s.servers[:len(s.servers)-1]
		return s.snapshotLocked(), err
	}
	return s.snapshotLocked(), nil
}

func (s *natServerStore) remove(id string) (NATServerSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	index := -1
	for i, server := range s.servers {
		if server.ID == id {
			index = i
			break
		}
	}
	if index < 0 {
		return s.snapshotLocked(), errors.New("STUN 服务器已不存在")
	}
	previousServers := append([]NATServer(nil), s.servers...)
	previousSelected := s.selected
	s.servers = append(s.servers[:index:index], s.servers[index+1:]...)
	if s.selected == id {
		s.selected = natServerAutoID
	}
	if err := s.saveLocked(); err != nil {
		s.servers = previousServers
		s.selected = previousSelected
		return s.snapshotLocked(), err
	}
	return s.snapshotLocked(), nil
}

func (s *natServerStore) reset() (NATServerSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	previousServers := s.servers
	previousSelected := s.selected
	s.servers = defaultNATServers()
	s.selected = natServerAutoID
	if err := s.saveLocked(); err != nil {
		s.servers = previousServers
		s.selected = previousSelected
		return s.snapshotLocked(), err
	}
	return s.snapshotLocked(), nil
}

func normalizeNATServerAddress(value string) (string, error) {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.TrimPrefix(value, "stun://")
	value = strings.TrimPrefix(value, "stun:")
	if value == "" {
		return "", errors.New("请输入 STUN 服务器地址")
	}
	if !strings.Contains(value, ":") {
		value = net.JoinHostPort(value, "3478")
	}
	host, portText, err := net.SplitHostPort(value)
	if err != nil {
		return "", errors.New("服务器地址格式应为 host:port，例如 stun.example.com:3478")
	}
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if host == "" || (net.ParseIP(host) == nil && !natServerHostPattern.MatchString(host)) {
		return "", errors.New("STUN 服务器主机名无效")
	}
	if ip := net.ParseIP(host); ip != nil && ip.To4() == nil {
		return "", errors.New("当前 NAT 类型检测仅支持 IPv4 STUN 服务器")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return "", errors.New("STUN 服务器端口必须在 1 到 65535 之间")
	}
	return net.JoinHostPort(host, strconv.Itoa(port)), nil
}

func (s *natServerStore) load() error {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var document natServerDocument
	if err := json.Unmarshal(data, &document); err != nil {
		return err
	}
	servers := make([]NATServer, 0, len(document.Servers))
	seen := map[string]struct{}{}
	for _, server := range document.Servers {
		address, normalizeErr := normalizeNATServerAddress(server.Address)
		if normalizeErr != nil {
			continue
		}
		key := strings.ToLower(address)
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		server.Address = address
		if server.ID == "" {
			hash := sha256.Sum256([]byte(address))
			server.ID = "custom-" + hex.EncodeToString(hash[:6])
		}
		servers = append(servers, server)
	}
	s.servers = servers
	s.selected = document.SelectedID
	if s.selected == "" {
		s.selected = natServerAutoID
	}
	if s.selected != natServerAutoID {
		found := false
		for _, server := range s.servers {
			if server.ID == s.selected {
				found = true
			}
		}
		if !found {
			s.selected = natServerAutoID
		}
	}
	return nil
}

func (s *natServerStore) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("创建 STUN 配置目录失败：%w", err)
	}
	data, err := json.MarshalIndent(natServerDocument{Version: 1, SelectedID: s.selected, Servers: s.servers}, "", "  ")
	if err != nil {
		return fmt.Errorf("编码 STUN 配置失败：%w", err)
	}
	temporary := s.path + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return fmt.Errorf("写入 STUN 配置失败：%w", err)
	}
	if err := os.Rename(temporary, s.path); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("保存 STUN 配置失败：%w", err)
	}
	return nil
}
