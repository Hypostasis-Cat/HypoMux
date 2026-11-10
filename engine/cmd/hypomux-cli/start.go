package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
)

// startAdapter mirrors the protocol v1 engine.start adapter entry.
type startAdapter struct {
	Name       string `json:"name"`
	SourceIP   string `json:"source_ip,omitempty"`
	IfIndex    int    `json:"if_index,omitempty"`
	SourceIPv6 string `json:"source_ipv6,omitempty"`
	Weight     int    `json:"weight,omitempty"`
}

// startDNSConfig mirrors the protocol v1 engine.start dns entry.
type startDNSConfig struct {
	Policy         string   `json:"policy,omitempty"`
	LegacyServers  []string `json:"legacy_servers,omitempty"`
	DoHServers     []string `json:"doh_servers,omitempty"`
	DoTServers     []string `json:"dot_servers,omitempty"`
	CacheTTLMS     int      `json:"cache_ttl_ms,omitempty"`
	QueryTimeoutMS int      `json:"query_timeout_ms,omitempty"`
}

type startParams struct {
	Mode             string         `json:"mode"`
	ListenHost       string         `json:"listen_host"`
	SOCKSPort        int            `json:"socks_port"`
	HTTPPort         int            `json:"http_port"`
	Strategy         string         `json:"strategy,omitempty"`
	Weighted         bool           `json:"weighted"`
	Adapters         []startAdapter `json:"adapters"`
	ConnectTimeoutMS int            `json:"connect_timeout_ms"`
	DNS              startDNSConfig `json:"dns"`
	SteamCDNEnabled  bool           `json:"steam_cdn_enabled,omitempty"`
}

// commandStart implements `hypomux-cli start` (engine.start, proxy mode).
//
// TUN orchestration stays a desktop concern: building and pinning the
// sing-box configuration is a privileged desktop workflow, so the CLI starts
// proxy-mode aggregation only and refuses tun_tcp_pool with guidance.
func commandStart(c *commandContext) int {
	flags := c.newFlagSet("start")
	options, err := parseStartFlags(flags, c.args)
	if err != nil {
		fmt.Fprintf(c.stderr, "%v\n", err)
		return 2
	}
	available, err := listAdapters()
	if err != nil {
		fmt.Fprintf(c.stderr, "%v\n", err)
		return 1
	}
	selected := options.adapters
	if options.all {
		for _, adapter := range available {
			selected = append(selected, adapter.Name)
		}
	}
	if len(selected) == 0 {
		fmt.Fprintln(c.stderr, "未选择聚合网卡。可用网卡:")
		for _, adapter := range available {
			fmt.Fprintf(c.stderr, "  %s（%s）\n", adapter.Name, adapter.SourceIP)
		}
		fmt.Fprintln(c.stderr, "使用 --adapter 名称（可重复）、--adapter 名称:权重 或 --all 选择。")
		return 2
	}
	adapters, err := resolveRequestedAdapters(selected, available)
	if err != nil {
		fmt.Fprintf(c.stderr, "%v\n", err)
		return 2
	}
	if err := applyWeightOverrides(adapters, options.weights); err != nil {
		fmt.Fprintf(c.stderr, "%v\n", err)
		return 2
	}

	params := startParams{
		Mode:             "proxy",
		ListenHost:       options.listenHost,
		SOCKSPort:        options.socksPort,
		HTTPPort:         options.httpPort,
		Strategy:         options.strategy,
		Weighted:         options.strategy == strategyWeighted,
		Adapters:         adapters,
		ConnectTimeoutMS: options.connectTimeoutMS,
		DNS: startDNSConfig{
			Policy:         options.dnsPolicy,
			LegacyServers:  options.dnsServers,
			DoHServers:     options.dohServers,
			DoTServers:     options.dotServers,
			CacheTTLMS:     options.dnsCacheTTLMS,
			QueryTimeoutMS: options.dnsQueryTimeoutMS,
		},
		SteamCDNEnabled: options.steamCDN,
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	conn, err := c.factory().Connect(ctx)
	if err != nil {
		fmt.Fprintf(c.stderr, "%v\n", err)
		return 1
	}
	s := newSession(conn, c.stderr)
	defer s.Close()

	// The strategy flag is authoritative on the wire; a core that does not
	// advertise it must hear about it before the engine starts, not after.
	if params.Strategy != "" && params.Strategy != strategyRoundRobin {
		var hello helloResult
		if err := s.call(ctx, "engine.hello", nil, &hello); err != nil {
			fmt.Fprintf(c.stderr, "engine.hello 失败：%v\n", err)
			return 1
		}
		if !containsString(hello.SchedulingStrategies, params.Strategy) {
			fmt.Fprintf(c.stderr, "核心不支持调度策略 %q（可用: %s）\n",
				params.Strategy, strings.Join(hello.SchedulingStrategies, ", "))
			return 1
		}
	}

	var raw json.RawMessage
	if err := s.call(ctx, "engine.start", params, &raw); err != nil {
		fmt.Fprintf(c.stderr, "engine.start 失败：%v\n", err)
		return 1
	}
	if c.jsonOut {
		return c.printRaw(raw)
	}
	var start startResult
	if err := json.Unmarshal(raw, &start); err != nil {
		fmt.Fprintf(c.stderr, "解析启动结果失败：%v\n", err)
		return 1
	}
	return printStartResult(c.stdout, &start)
}

type startResult struct {
	State struct {
		State string `json:"state"`
	} `json:"state"`
	Mode      string        `json:"mode"`
	Endpoints endpointsView `json:"endpoints"`
}

func printStartResult(w io.Writer, start *startResult) int {
	fmt.Fprintf(w, "聚合引擎已启动:  %s（%s 模式）\n", start.State.State, start.Mode)
	if start.Endpoints.SOCKS != "" {
		fmt.Fprintf(w, "SOCKS5 代理:    %s\n", start.Endpoints.SOCKS)
	}
	if start.Endpoints.HTTP != "" {
		fmt.Fprintf(w, "HTTP 代理:      %s\n", start.Endpoints.HTTP)
	}
	if len(start.Endpoints.Channels) > 0 {
		for _, name := range sortedChannelNames(start.Endpoints.Channels) {
			fmt.Fprintf(w, "通道 %s:  %s\n", name, start.Endpoints.Channels[name])
		}
	}
	fmt.Fprintln(w, "提示: 将应用的本地代理指向以上端口即可开始多网卡聚合。")
	return 0
}

type startOptions struct {
	all               bool
	adapters          []string
	weights           []string
	listenHost        string
	socksPort         int
	httpPort          int
	strategy          string
	connectTimeoutMS  int
	dnsPolicy         string
	dnsServers        []string
	dohServers        []string
	dotServers        []string
	dnsCacheTTLMS     int
	dnsQueryTimeoutMS int
	steamCDN          bool
}

const (
	strategyRoundRobin      = "round-robin"
	strategyWeighted        = "weighted"
	strategyAdaptive        = "adaptive-throughput"
	strategyLatencyFirst    = "latency-first"
	defaultConnectTimeoutMS = 6000
)

func parseStartFlags(flags *flag.FlagSet, args []string) (startOptions, error) {
	options := startOptions{
		listenHost:       "127.0.0.1",
		connectTimeoutMS: defaultConnectTimeoutMS,
	}
	flags.BoolVar(&options.all, "all", false, "聚合全部可用网卡（权重 1）")
	flags.Var(repeatableValue(&options.adapters), "adapter", "参与聚合的网卡，可重复；支持 名称:权重（如 \"Wi-Fi:3\"）")
	flags.Var(repeatableValue(&options.weights), "weight", "网卡权重覆盖，格式 名称=权重，可重复")
	flags.StringVar(&options.listenHost, "listen-host", options.listenHost, "本地监听地址（仅回环地址）")
	flags.IntVar(&options.socksPort, "socks-port", 0, "SOCKS5 端口，0 表示自动分配")
	flags.IntVar(&options.httpPort, "http-port", 0, "HTTP 端口，0 表示自动分配")
	flags.StringVar(&options.strategy, "strategy", "", "调度策略: round-robin | weighted | adaptive-throughput | latency-first")
	flags.IntVar(&options.connectTimeoutMS, "connect-timeout-ms", options.connectTimeoutMS, "出站连接超时")
	flags.StringVar(&options.dnsPolicy, "dns-policy", "", "DNS 策略: auto | off | system | alidns | dnspod | google | custom | dot")
	flags.Var(repeatableValue(&options.dnsServers), "dns-server", "传统 DNS 服务器 IP，可重复")
	flags.Var(repeatableValue(&options.dohServers), "doh-server", "DoH 服务器 URL，可重复")
	flags.Var(repeatableValue(&options.dotServers), "dot-server", "DoT 服务器 tls://URL，可重复")
	flags.IntVar(&options.dnsCacheTTLMS, "dns-cache-ttl-ms", 0, "DNS 缓存 TTL")
	flags.IntVar(&options.dnsQueryTimeoutMS, "dns-timeout-ms", 0, "DNS 查询超时")
	flags.BoolVar(&options.steamCDN, "steam-cdn", false, "启用 Steam CDN 优选")
	if err := flags.Parse(args); err != nil {
		return options, err
	}
	if flags.NArg() > 0 {
		return options, fmt.Errorf("未知参数 %q", flags.Arg(0))
	}
	switch options.strategy {
	case "", strategyRoundRobin, strategyWeighted, strategyAdaptive, strategyLatencyFirst:
	default:
		return options, fmt.Errorf("不支持的调度策略 %q", options.strategy)
	}
	if options.all && len(options.adapters) > 0 {
		return options, errors.New("--all 不能与 --adapter 同时使用")
	}
	if options.socksPort < 0 || options.socksPort > 65535 || options.httpPort < 0 || options.httpPort > 65535 {
		return options, errors.New("端口必须在 0 到 65535 之间")
	}
	if (options.socksPort == 0) != (options.httpPort == 0) {
		return options, errors.New("SOCKS 与 HTTP 端口必须同时为 0（自动分配）或同时指定")
	}
	if options.socksPort != 0 && options.socksPort == options.httpPort {
		return options, errors.New("SOCKS 与 HTTP 端口不能相同")
	}
	return options, nil
}

// applyWeightOverrides applies `--weight 名称=权重` on top of the resolved
// selection so scripts can spell both forms.
func applyWeightOverrides(adapters []startAdapter, overrides []string) error {
	for _, override := range overrides {
		name, weight, err := parseWeightOverride(override)
		if err != nil {
			return err
		}
		found := false
		for index := range adapters {
			if strings.EqualFold(adapters[index].Name, name) {
				adapters[index].Weight = weight
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("--weight 引用的适配器 %q 不在本次聚合选择中", name)
		}
	}
	return nil
}

func parseWeightOverride(override string) (string, int, error) {
	index := strings.Index(override, "=")
	if index <= 0 || index == len(override)-1 {
		return "", 0, fmt.Errorf("--weight 需要 名称=权重 格式：%q", override)
	}
	name := strings.TrimSpace(override[:index])
	weight, err := atoiStrict(strings.TrimSpace(override[index+1:]))
	if err != nil || weight < 1 || weight > 100 {
		return "", 0, fmt.Errorf("--weight 的权重必须在 1 到 100 之间：%q", override)
	}
	if name == "" {
		return "", 0, fmt.Errorf("--weight 的适配器名称不能为空：%q", override)
	}
	return name, weight, nil
}

// repeatableValue collects repeated string flag occurrences in order.
func repeatableValue(target *[]string) flag.Value {
	return &repeatableCollector{target: target}
}

type repeatableCollector struct {
	target *[]string
}

func (collector *repeatableCollector) String() string {
	if collector == nil || collector.target == nil {
		return ""
	}
	return strings.Join(*collector.target, ", ")
}

func (collector *repeatableCollector) Set(value string) error {
	*collector.target = append(*collector.target, value)
	return nil
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
