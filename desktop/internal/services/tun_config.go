package services

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/Hypostasis-Cat/HypoMux/desktop/internal/engineclient"
)

type clashAPIConfig struct {
	Endpoint string
	Secret   string
}

type dnsResolveResult struct {
	Domain     string `json:"domain"`
	Adapter    string `json:"adapter"`
	RecordType string `json:"record_type"`
	Address    string `json:"address"`
	Transport  string `json:"transport"`
	Server     string `json:"server"`
	Cached     bool   `json:"cached"`
}

type tunConfigOptions struct {
	DNSPolicy     string
	IPv6Available bool
	ConfigName    string
	ClashAPI      *clashAPIConfig
	ConfigSHA256  *string
}

func writeSingBoxConfig(
	endpoints map[string]string,
	dnsAdapter AdapterView,
	dnsResult dnsResolveResult,
	rules []RoutingRule,
	compatibility compatibilityPlan,
	strictRoute bool,
) (string, string, clashAPIConfig, error) {
	return writeSingBoxConfigWithOptions(
		endpoints, dnsAdapter, dnsResult, rules, compatibility, strictRoute,
		tunConfigOptions{
			DNSPolicy:     "auto",
			IPv6Available: strings.TrimSpace(dnsAdapter.SourceIPv6) != "",
		},
	)
}

func writeSingBoxConfigWithOptions(
	endpoints map[string]string,
	dnsAdapter AdapterView,
	dnsResult dnsResolveResult,
	rules []RoutingRule,
	compatibility compatibilityPlan,
	strictRoute bool,
	options tunConfigOptions,
) (string, string, clashAPIConfig, error) {
	ethernetPort, err := loopbackPort(endpoints, "nic_ethernet")
	if err != nil {
		return "", "", clashAPIConfig{}, err
	}
	wifiPort, err := loopbackPort(endpoints, "nic_wifi")
	if err != nil {
		return "", "", clashAPIConfig{}, err
	}
	aggregationPort, err := loopbackPort(endpoints, "aggregation")
	if err != nil {
		return "", "", clashAPIConfig{}, err
	}
	dnsPolicy := normalizeTunDNSPolicy(options.DNSPolicy)
	var upstream map[string]any
	if dnsPolicy == "system" {
		upstream = map[string]any{"type": "local", "tag": "dns-local"}
	} else {
		upstream, err = buildDNSUpstreamForPolicy(dnsAdapter, dnsResult, dnsPolicy)
		if err != nil {
			return "", "", clashAPIConfig{}, err
		}
	}
	singBox, err := resolveRuntimeAsset("sing-box.exe")
	if err != nil {
		return "", "", clashAPIConfig{}, err
	}
	clashAPI := clashAPIConfig{}
	if options.ClashAPI != nil {
		clashAPI = *options.ClashAPI
	} else {
		clashAPI, err = reserveClashAPI()
		if err != nil {
			return "", "", clashAPIConfig{}, err
		}
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
	compatibilityPaths := append([]string(nil), compatibility.ProcessPaths...)
	routeRules := []any{
		map[string]any{"action": "sniff", "timeout": "300ms"},
		map[string]any{"process_path": processPaths, "outbound": "system-direct"},
		map[string]any{
			"process_name": []string{"HypoMux.exe", "hypomux-engine.exe", "sing-box.exe"},
			"outbound":     "system-direct",
		},
	}
	if len(compatibilityPaths) > 0 {
		routeRules = append(routeRules, map[string]any{
			"process_path": compatibilityPaths, "outbound": "system-direct",
		})
	}
	if len(compatibility.ProcessNames) > 0 {
		routeRules = append(routeRules, map[string]any{
			"process_name": compatibility.ProcessNames, "outbound": "system-direct",
		})
	}
	if dnsPolicy != "system" {
		routeRules = append(routeRules,
			map[string]any{"port": []int{53}, "action": "hijack-dns"},
			map[string]any{"protocol": []string{"dns"}, "action": "hijack-dns"},
			map[string]any{"action": "resolve", "server": "dns-local", "strategy": "prefer_ipv4"},
		)
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
			return "", "", clashAPIConfig{}, portErr
		}
		_ = endpoint
		outbounds = append(outbounds, socksOutbound(name, port))
	}
	usesFakeIP := tunDNSNeedsFakeIP(dnsPolicy, rules)
	dnsServers := []any{upstream}
	dnsConfig := map[string]any{
		"servers": dnsServers,
		"final":   "dns-local",
	}
	if usesFakeIP {
		dnsConfig["servers"] = append(dnsServers,
			map[string]any{
				"type": "fakeip", "tag": "dns-fakeip",
				"inet4_range": "198.18.0.0/15", "inet6_range": "fc00::/18",
			},
		)
		dnsConfig["rules"] = []any{
			map[string]any{"query_type": []string{"A", "AAAA"}, "server": "dns-fakeip"},
		}
		dnsConfig["reverse_mapping"] = true
	}
	address := []string{"172.19.0.1/30"}
	if options.IPv6Available {
		address = append(address, "fdfe:dcba:9876::1/126")
	}
	tunInbound := map[string]any{
		"type": "tun", "tag": "tun-in", "interface_name": "HypoMux-Tun",
		"address": address,
		"mtu":     1492, "auto_route": true, "strict_route": strictRoute, "stack": "system",
	}
	if exclusions := dnsBootstrapRouteExclusions(dnsResult); len(exclusions) > 0 && dnsPolicy != "system" {
		tunInbound["route_exclude_address"] = exclusions
	}
	config := map[string]any{
		"log":       map[string]any{"level": "warn", "timestamp": true},
		"dns":       dnsConfig,
		"inbounds":  []any{tunInbound},
		"outbounds": outbounds,
		"route": map[string]any{
			"auto_detect_interface": true, "default_domain_resolver": "dns-local",
			"find_process": true, "final": "aggregation", "rules": routeRules,
		},
		"experimental": map[string]any{
			"clash_api": map[string]any{
				"external_controller": clashAPI.Endpoint,
				"secret":              clashAPI.Secret,
			},
		},
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return "", "", clashAPIConfig{}, fmt.Errorf("生成 TUN 配置失败：%w", err)
	}
	if options.ConfigSHA256 != nil {
		digest := sha256.Sum256(data)
		*options.ConfigSHA256 = hex.EncodeToString(digest[:])
	}
	directory := filepath.Join(settingsDirectory(), "runtime")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return "", "", clashAPIConfig{}, fmt.Errorf("创建 TUN 运行目录失败：%w", err)
	}
	path := filepath.Join(directory, "sing-box.json")
	if configName := strings.TrimSpace(options.ConfigName); configName != "" {
		path = filepath.Join(directory, filepath.Base(configName))
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return "", "", clashAPIConfig{}, fmt.Errorf("写入 TUN 配置失败：%w", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		return "", "", clashAPIConfig{}, fmt.Errorf("提交 TUN 配置失败：%w", err)
	}
	return singBox, path, clashAPI, nil
}

func reserveClashAPI() (clashAPIConfig, error) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return clashAPIConfig{}, fmt.Errorf("预留 sing-box 元数据端口失败：%w", err)
	}
	endpoint := listener.Addr().String()
	if closeErr := listener.Close(); closeErr != nil {
		return clashAPIConfig{}, fmt.Errorf("释放 sing-box 元数据端口失败：%w", closeErr)
	}
	token := make([]byte, 24)
	if _, err := rand.Read(token); err != nil {
		return clashAPIConfig{}, fmt.Errorf("生成 sing-box 元数据凭据失败：%w", err)
	}
	return clashAPIConfig{Endpoint: endpoint, Secret: hex.EncodeToString(token)}, nil
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

func normalizeTunDNSPolicy(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "auto"
	}
	return value
}

func buildDNSUpstreamForPolicy(
	adapter AdapterView,
	result dnsResolveResult,
	policy string,
) (map[string]any, error) {
	policy = normalizeTunDNSPolicy(policy)
	transport := strings.ToLower(strings.TrimSpace(result.Transport))
	if policy == "off" && (transport == "doh" || transport == "dot") {
		return nil, fmt.Errorf("DNS 策略 off 不允许使用加密上游：%s", result.Server)
	}
	return buildDNSUpstream(adapter, result)
}

func buildDNSUpstream(adapter AdapterView, result dnsResolveResult) (map[string]any, error) {
	transport := strings.ToLower(strings.TrimSpace(result.Transport))
	server := strings.TrimSpace(result.Server)
	serverName := ""
	if parts := strings.SplitN(server, "@", 2); len(parts) == 2 {
		serverName = strings.TrimSpace(parts[0])
		server = strings.TrimSpace(parts[1])
	}
	defaultPort := 53
	upstreamType := transport
	switch transport {
	case "doh":
		if serverName == "" {
			return nil, fmt.Errorf("聚合核心返回了无效的 DoH 端点：%s", result.Server)
		}
		defaultPort = 443
		upstreamType = "https"
	case "dot":
		defaultPort = 853
		upstreamType = "tls"
	case "udp", "tcp":
	default:
		return nil, fmt.Errorf("聚合核心返回了不支持的 DNS 传输：%s", result.Transport)
	}
	host, port, err := splitEndpoint(server, defaultPort)
	if err != nil {
		return nil, err
	}
	upstream := map[string]any{
		"type": upstreamType, "tag": "dns-local", "server": host, "server_port": port,
	}
	if transport == "doh" {
		upstream["path"] = "/dns-query"
	}
	if transport == "doh" || transport == "dot" {
		if serverName == "" {
			serverName = host
		}
		upstream["tls"] = map[string]any{"enabled": true, "server_name": serverName}
	}
	if strings.TrimSpace(adapter.Name) != "" {
		upstream["bind_interface"] = adapter.Name
	}
	if strings.TrimSpace(adapter.Address) != "" {
		upstream["inet4_bind_address"] = adapter.Address
	}
	return upstream, nil
}

func tunDNSNeedsFakeIP(policy string, rules []RoutingRule) bool {
	if policy != "off" && policy != "system" {
		return true
	}
	for _, rule := range rules {
		if rule.MatchType == MatchDomain {
			return true
		}
	}
	return false
}

func dnsBootstrapRouteExclusions(result dnsResolveResult) []string {
	seen := map[string]struct{}{}
	for _, value := range literalIPsInEndpoint(result.Server) {
		ip := net.ParseIP(value)
		if ip == nil {
			continue
		}
		prefix := 128
		if ipv4 := ip.To4(); ipv4 != nil {
			ip = ipv4
			prefix = 32
		}
		seen[ip.String()+"/"+strconv.Itoa(prefix)] = struct{}{}
	}
	resulting := make([]string, 0, len(seen))
	for value := range seen {
		resulting = append(resulting, value)
	}
	sort.Strings(resulting)
	return resulting
}

func literalIPsInEndpoint(value string) []string {
	seen := map[string]struct{}{}
	add := func(candidate string) {
		candidate = strings.TrimSpace(strings.Trim(candidate, "[]"))
		if ip := net.ParseIP(candidate); ip != nil {
			if ipv4 := ip.To4(); ipv4 != nil {
				candidate = ipv4.String()
			} else {
				candidate = ip.String()
			}
			seen[candidate] = struct{}{}
		}
	}
	for _, part := range strings.Split(value, "@") {
		part = strings.TrimSpace(part)
		if host, _, err := net.SplitHostPort(part); err == nil {
			add(host)
		}
		if strings.HasPrefix(part, "[") {
			if end := strings.IndexByte(part, ']'); end > 1 {
				add(part[1:end])
			}
		}
		for _, candidate := range strings.FieldsFunc(part, func(r rune) bool {
			return r == '/' || r == '?' || r == '#'
		}) {
			add(candidate)
		}
	}
	resulting := make([]string, 0, len(seen))
	for candidate := range seen {
		resulting = append(resulting, candidate)
	}
	sort.Strings(resulting)
	return resulting
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
