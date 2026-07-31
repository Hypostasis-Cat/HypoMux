package services

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Hypostasis-Cat/HypoMux/desktop/internal/engineclient"
)

type dnsResolveResult struct {
	Domain     string `json:"domain"`
	Adapter    string `json:"adapter"`
	RecordType string `json:"record_type"`
	Address    string `json:"address"`
	Transport  string `json:"transport"`
	Server     string `json:"server"`
	Cached     bool   `json:"cached"`
}

func writeSingBoxConfig(
	endpoints map[string]string,
	dnsAdapter AdapterView,
	dnsResult dnsResolveResult,
	rules []RoutingRule,
	strictRoute bool,
) (string, string, error) {
	ethernetPort, err := loopbackPort(endpoints, "nic_ethernet")
	if err != nil {
		return "", "", err
	}
	wifiPort, err := loopbackPort(endpoints, "nic_wifi")
	if err != nil {
		return "", "", err
	}
	aggregationPort, err := loopbackPort(endpoints, "aggregation")
	if err != nil {
		return "", "", err
	}
	upstream, err := buildDNSUpstream(dnsAdapter, dnsResult)
	if err != nil {
		return "", "", err
	}
	singBox, err := resolveRuntimeAsset("sing-box.exe")
	if err != nil {
		return "", "", err
	}
	processPaths := []string{}
	for _, candidate := range []string{os.Args[0], engineExecutableOrEmpty(), singBox} {
		if candidate == "" {
			continue
		}
		if absolute, absoluteErr := filepath.Abs(candidate); absoluteErr == nil {
			processPaths = append(processPaths, absolute)
		}
	}
	routeRules := []any{
		map[string]any{"action": "sniff", "timeout": "300ms"},
		map[string]any{"process_path": processPaths, "outbound": "system-direct"},
		map[string]any{
			"process_name": []string{"HypoMux.exe", "hypomux-engine.exe", "sing-box.exe"},
			"outbound":     "system-direct",
		},
		map[string]any{"port": []int{53}, "action": "hijack-dns"},
		map[string]any{"protocol": []string{"dns"}, "action": "hijack-dns"},
	}
	for _, rule := range rules {
		entry := map[string]any{"outbound": rule.Outbound}
		switch rule.MatchType {
		case MatchProcess:
			entry["process_name"] = []string{rule.Value}
		case MatchDomain:
			entry["domain"] = []string{rule.Value}
			entry["domain_suffix"] = []string{"." + strings.TrimPrefix(rule.Value, ".")}
		case MatchIP:
			entry["ip_cidr"] = []string{rule.Value}
		default:
			continue
		}
		routeRules = append(routeRules, entry)
	}
	routeRules = append(routeRules, map[string]any{
		"action": "resolve", "server": "dns-local", "strategy": "prefer_ipv4",
	})
	directOutbound := map[string]any{"type": "direct", "tag": "direct"}
	if directPort, directErr := loopbackPort(endpoints, "direct"); directErr == nil {
		directOutbound = socksOutbound("direct", directPort)
	}
	outbounds := []any{
		socksOutbound("nic_ethernet", ethernetPort),
		socksOutbound("nic_wifi", wifiPort),
		socksOutbound("aggregation", aggregationPort),
		directOutbound,
		map[string]any{"type": "direct", "tag": "system-direct"},
	}
	for name, endpoint := range endpoints {
		if name == "nic_ethernet" || name == "nic_wifi" || name == "aggregation" || name == "direct" ||
			!strings.HasPrefix(name, "nic_") {
			continue
		}
		port, portErr := loopbackPort(endpoints, name)
		if portErr != nil {
			return "", "", portErr
		}
		_ = endpoint
		outbounds = append(outbounds, socksOutbound(name, port))
	}
	config := map[string]any{
		"log": map[string]any{"level": "warn", "timestamp": true},
		"dns": map[string]any{
			"servers": []any{
				upstream,
				map[string]any{
					"type": "fakeip", "tag": "dns-fakeip",
					"inet4_range": "198.18.0.0/15", "inet6_range": "fc00::/18",
				},
			},
			"rules": []any{map[string]any{"query_type": []string{"A", "AAAA"}, "server": "dns-fakeip"}},
			"final": "dns-local", "reverse_mapping": true,
		},
		"inbounds": []any{map[string]any{
			"type": "tun", "tag": "tun-in", "interface_name": "HypoMux-Tun",
			"address": []string{"172.19.0.1/30", "fdfe:dcba:9876::1/126"},
			"mtu":     1492, "auto_route": true, "strict_route": strictRoute, "stack": "system",
		}},
		"outbounds": outbounds,
		"route": map[string]any{
			"auto_detect_interface": true, "default_domain_resolver": "dns-local",
			"final": "aggregation", "rules": routeRules,
		},
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return "", "", fmt.Errorf("生成 TUN 配置失败：%w", err)
	}
	directory := filepath.Join(settingsDirectory(), "runtime")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return "", "", fmt.Errorf("创建 TUN 运行目录失败：%w", err)
	}
	path := filepath.Join(directory, "sing-box.json")
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return "", "", fmt.Errorf("写入 TUN 配置失败：%w", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		return "", "", fmt.Errorf("提交 TUN 配置失败：%w", err)
	}
	return singBox, path, nil
}

func engineExecutableOrEmpty() string {
	path, _ := engineclient.ResolveExecutable()
	return path
}

func socksOutbound(tag string, port int) map[string]any {
	return map[string]any{
		"type": "socks", "tag": tag, "server": "127.0.0.1",
		"server_port": port, "version": "5",
	}
}

func loopbackPort(endpoints map[string]string, name string) (int, error) {
	value := endpoints[name]
	host, portText, err := net.SplitHostPort(value)
	if err != nil {
		return 0, fmt.Errorf("聚合核心返回了无效的 %s 通道：%s", name, value)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 || (host != "127.0.0.1" && host != "localhost") {
		return 0, fmt.Errorf("聚合核心返回了不安全的 %s 通道：%s", name, value)
	}
	return port, nil
}

func buildDNSUpstream(adapter AdapterView, result dnsResolveResult) (map[string]any, error) {
	if strings.EqualFold(result.Transport, "doh") {
		parts := strings.SplitN(result.Server, "@", 2)
		if len(parts) != 2 || parts[0] == "" {
			return nil, fmt.Errorf("聚合核心返回了无效的 DoH 端点：%s", result.Server)
		}
		host, port, err := splitEndpoint(parts[1], 443)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"type": "https", "tag": "dns-local", "server": host, "server_port": port,
			"path":           "/dns-query",
			"tls":            map[string]any{"enabled": true, "server_name": parts[0]},
			"bind_interface": adapter.Name, "inet4_bind_address": adapter.Address,
		}, nil
	}
	host, port, err := splitEndpoint(result.Server, 53)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"type": "udp", "tag": "dns-local", "server": host, "server_port": port,
		"bind_interface": adapter.Name, "inet4_bind_address": adapter.Address,
	}, nil
}

func splitEndpoint(value string, defaultPort int) (string, int, error) {
	if host, portText, err := net.SplitHostPort(value); err == nil {
		port, parseErr := strconv.Atoi(portText)
		if parseErr != nil || port < 1 || port > 65535 {
			return "", 0, fmt.Errorf("无效端点：%s", value)
		}
		return host, port, nil
	}
	if net.ParseIP(value) != nil || !strings.Contains(value, ":") {
		return value, defaultPort, nil
	}
	return "", 0, fmt.Errorf("无效端点：%s", value)
}

func resolveRuntimeAsset(name string) (string, error) {
	candidates := []string{}
	if executable, err := os.Executable(); err == nil {
		root := filepath.Dir(executable)
		candidates = append(candidates, filepath.Join(root, "bin", name), filepath.Join(root, name))
	}
	if cwd, err := os.Getwd(); err == nil {
		for current, count := cwd, 0; count < 6; count++ {
			candidates = append(candidates, filepath.Join(current, "bin", name), filepath.Join(current, name))
			parent := filepath.Dir(current)
			if parent == current {
				break
			}
			current = parent
		}
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return filepath.Abs(candidate)
		}
	}
	return "", fmt.Errorf("未找到 %s", name)
}
