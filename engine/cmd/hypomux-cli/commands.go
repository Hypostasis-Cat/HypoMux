package main

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// commandStatus implements `hypomux-cli status` (engine.status).
func commandStatus(c *commandContext) int {
	flags := c.newFlagSet("status")
	if err := flags.Parse(c.args); err != nil {
		return 2
	}
	return c.callOnce("engine.status", nil, printStatus)
}

type statusResult struct {
	Engine struct {
		State          string    `json:"state"`
		Sequence       uint64    `json:"sequence"`
		StateChangedAt time.Time `json:"state_changed_at"`
		Reason         string    `json:"reason,omitempty"`
	} `json:"engine"`
	HostUptimeMS int64            `json:"host_uptime_ms"`
	Proxy        *proxyStatusView `json:"proxy,omitempty"`
	Tun          *tunStatusView   `json:"tun,omitempty"`
}

type proxyStatusView struct {
	Mode      string           `json:"mode"`
	Running   bool             `json:"running"`
	Endpoints endpointsView    `json:"endpoints"`
	Telemetry telemetrySummary `json:"telemetry"`
}

type endpointsView struct {
	SOCKS    string            `json:"socks,omitempty"`
	HTTP     string            `json:"http,omitempty"`
	Channels map[string]string `json:"channels,omitempty"`
}

type tunStatusView struct {
	State      string    `json:"state"`
	PID        int       `json:"pid,omitempty"`
	StartedAt  time.Time `json:"started_at,omitempty"`
	LastError  string    `json:"last_error,omitempty"`
	ConfigPath string    `json:"config_path,omitempty"`
}

type telemetrySummary struct {
	Scheduling struct {
		Strategy string `json:"strategy"`
	} `json:"scheduling"`
	Adapters []struct {
		Name             string `json:"name"`
		Connections      uint64 `json:"connections"`
		BytesUp          uint64 `json:"bytes_up"`
		BytesDown        uint64 `json:"bytes_down"`
		HealthState      string `json:"health_state"`
		DomainQuarantine int    `json:"domain_quarantines"`
	} `json:"adapters"`
}

func printStatus(w io.Writer, raw json.RawMessage) error {
	var status statusResult
	if err := json.Unmarshal(raw, &status); err != nil {
		return err
	}
	fmt.Fprintf(w, "引擎状态:  %s", status.Engine.State)
	if status.Engine.Reason != "" {
		fmt.Fprintf(w, "（%s）", status.Engine.Reason)
	}
	fmt.Fprintf(w, "\n核心运行:  %s\n", formatDuration(time.Duration(status.HostUptimeMS)*time.Millisecond))
	if status.Proxy != nil {
		proxy := status.Proxy
		state := "已停止"
		if proxy.Running {
			state = "运行中"
		}
		fmt.Fprintf(w, "代理模式:  %s（%s）\n", proxy.Mode, state)
		if proxy.Endpoints.SOCKS != "" {
			fmt.Fprintf(w, "SOCKS5:    %s\n", proxy.Endpoints.SOCKS)
		}
		if proxy.Endpoints.HTTP != "" {
			fmt.Fprintf(w, "HTTP:      %s\n", proxy.Endpoints.HTTP)
		}
		for _, name := range sortedChannelNames(proxy.Endpoints.Channels) {
			fmt.Fprintf(w, "通道 %s:  %s\n", name, proxy.Endpoints.Channels[name])
		}
		if strategy := proxy.Telemetry.Scheduling.Strategy; strategy != "" {
			fmt.Fprintf(w, "调度策略:  %s\n", strategy)
		}
		for _, adapter := range proxy.Telemetry.Adapters {
			fmt.Fprintf(w, "网卡 %s:  连接 %d，↑ %s，↓ %s，健康 %s\n",
				adapter.Name, adapter.Connections,
				formatBytes(adapter.BytesUp), formatBytes(adapter.BytesDown),
				translateHealth(adapter.HealthState))
		}
	}
	if status.Tun != nil {
		fmt.Fprintf(w, "TUN:       %s", status.Tun.State)
		if status.Tun.PID != 0 {
			fmt.Fprintf(w, "（PID %d）", status.Tun.PID)
		}
		fmt.Fprintln(w)
	}
	return nil
}

// commandHello implements `hypomux-cli hello` (engine.hello).
func commandHello(c *commandContext) int {
	flags := c.newFlagSet("hello")
	if err := flags.Parse(c.args); err != nil {
		return 2
	}
	return c.callOnce("engine.hello", nil, printHello)
}

type helloResult struct {
	Engine               string              `json:"engine"`
	EngineVersion        string              `json:"engine_version"`
	Commit               string              `json:"commit"`
	ProtocolVersion      int                 `json:"protocol_version"`
	Transport            string              `json:"transport"`
	SchedulingStrategies []string            `json:"scheduling_strategies"`
	Capabilities         []string            `json:"capabilities"`
	Modes                []string            `json:"modes"`
	ModeFeatures         map[string][]string `json:"mode_features"`
	OS                   string              `json:"os"`
	Arch                 string              `json:"arch"`
	PID                  int                 `json:"pid"`
	Elevated             bool                `json:"elevated"`
}

func printHello(w io.Writer, raw json.RawMessage) error {
	var hello helloResult
	if err := json.Unmarshal(raw, &hello); err != nil {
		return err
	}
	fmt.Fprintf(w, "核心:        %s %s (%s)\n", hello.Engine, hello.EngineVersion, hello.Commit)
	fmt.Fprintf(w, "协议:        v%d %s\n", hello.ProtocolVersion, hello.Transport)
	fmt.Fprintf(w, "平台:        %s/%s，PID %d，管理员 %s\n",
		hello.OS, hello.Arch, hello.PID, yesNo(hello.Elevated))
	fmt.Fprintf(w, "模式:        %s\n", strings.Join(hello.Modes, ", "))
	if len(hello.SchedulingStrategies) > 0 {
		fmt.Fprintf(w, "调度策略:    %s\n", strings.Join(hello.SchedulingStrategies, ", "))
	}
	fmt.Fprintf(w, "能力 (%d):   %s\n", len(hello.Capabilities), strings.Join(hello.Capabilities, ", "))
	return nil
}

// commandHealth implements `hypomux-cli health` (health.check).
func commandHealth(c *commandContext) int {
	flags := c.newFlagSet("health")
	if err := flags.Parse(c.args); err != nil {
		return 2
	}
	return c.callOnce("health.check", nil, printHealth)
}

type healthResult struct {
	OK           bool   `json:"ok"`
	State        string `json:"state"`
	HostUptimeMS int64  `json:"host_uptime_ms"`
}

func printHealth(w io.Writer, raw json.RawMessage) error {
	var health healthResult
	if err := json.Unmarshal(raw, &health); err != nil {
		return err
	}
	fmt.Fprintf(w, "健康: %s，状态 %s，核心运行 %s\n",
		yesNo(health.OK), health.State,
		formatDuration(time.Duration(health.HostUptimeMS)*time.Millisecond))
	return nil
}

// commandStop implements `hypomux-cli stop` (engine.stop).
func commandStop(c *commandContext) int {
	flags := c.newFlagSet("stop")
	if err := flags.Parse(c.args); err != nil {
		return 2
	}
	return c.callOnce("engine.stop", nil, printStop)
}

type stopResult struct {
	Accepted bool        `json:"accepted"`
	State    engineState `json:"state"`
}

type engineState struct {
	State          string    `json:"state"`
	Sequence       uint64    `json:"sequence"`
	StateChangedAt time.Time `json:"state_changed_at"`
	Reason         string    `json:"reason,omitempty"`
}

func printStop(w io.Writer, raw json.RawMessage) error {
	var stop stopResult
	if err := json.Unmarshal(raw, &stop); err != nil {
		return err
	}
	if !stop.Accepted {
		fmt.Fprintf(w, "引擎已处于 %s 状态，无需停止\n", stop.State.State)
		return nil
	}
	fmt.Fprintf(w, "聚合引擎已停止（%s）\n", stop.State.State)
	return nil
}

// commandTelemetry implements `hypomux-cli telemetry` (engine.telemetry).
func commandTelemetry(c *commandContext) int {
	flags := c.newFlagSet("telemetry")
	includeConnections := flags.Bool("connections", false, "同时列出活动连接")
	if err := flags.Parse(c.args); err != nil {
		return 2
	}
	params := map[string]any{"include_connections": *includeConnections}
	return c.callOnce("engine.telemetry", params, printTelemetry)
}

type telemetryResult struct {
	StartedAt  time.Time `json:"started_at"`
	SampledAt  time.Time `json:"sampled_at"`
	Scheduling struct {
		Strategy             string `json:"strategy"`
		EffectiveTCPStrategy string `json:"effective_tcp_strategy,omitempty"`
		State                string `json:"state,omitempty"`
	} `json:"scheduling"`
	Adapters              []adapterTelemetry `json:"adapters"`
	Connections           []connectionView   `json:"active_connections,omitempty"`
	DomainQuarantines     []quarantineView   `json:"domain_quarantines,omitempty"`
	DomainIsolationExpiry *bool              `json:"domain_isolation_expiry,omitempty"`
	Total                 struct {
		Connections uint64 `json:"connections"`
		BytesUp     uint64 `json:"bytes_up"`
		BytesDown   uint64 `json:"bytes_down"`
	} `json:"total"`
}

type adapterTelemetry struct {
	Name                string `json:"name"`
	SourceIP            string `json:"source_ip"`
	IfIndex             int    `json:"if_index"`
	SourceIPv6          string `json:"source_ipv6,omitempty"`
	Connections         uint64 `json:"connections"`
	BytesUp             uint64 `json:"bytes_up"`
	BytesDown           uint64 `json:"bytes_down"`
	HealthState         string `json:"health_state"`
	ConsecutiveFailures int    `json:"consecutive_failures"`
	DomainQuarantines   int    `json:"domain_quarantines"`
}

// connectionView keeps only the fields a human scans for; `--json` still
// returns the complete protocol payload.
type connectionView struct {
	ID        uint64    `json:"id"`
	Protocol  string    `json:"protocol"`
	Adapter   string    `json:"adapter,omitempty"`
	Client    string    `json:"client"`
	Target    string    `json:"target,omitempty"`
	Remote    string    `json:"remote,omitempty"`
	StartedAt time.Time `json:"started_at"`
	BytesUp   uint64    `json:"bytes_up"`
	BytesDown uint64    `json:"bytes_down"`
}

type quarantineView struct {
	Adapter   string    `json:"adapter"`
	Domain    string    `json:"domain"`
	Evidence  int       `json:"evidence"`
	ExpiresAt time.Time `json:"expires_at"`
}

func printTelemetry(w io.Writer, raw json.RawMessage) error {
	var telemetry telemetryResult
	if err := json.Unmarshal(raw, &telemetry); err != nil {
		return err
	}
	strategy := telemetry.Scheduling.Strategy
	if telemetry.Scheduling.EffectiveTCPStrategy != "" {
		strategy += fmt.Sprintf("（TCP 生效: %s，状态 %s）",
			telemetry.Scheduling.EffectiveTCPStrategy, telemetry.Scheduling.State)
	}
	fmt.Fprintf(w, "调度策略:  %s\n", strategy)
	fmt.Fprintf(w, "总计:      连接 %d，↑ %s，↓ %s\n",
		telemetry.Total.Connections,
		formatBytes(telemetry.Total.BytesUp), formatBytes(telemetry.Total.BytesDown))
	for _, adapter := range telemetry.Adapters {
		source := adapter.SourceIP
		if adapter.SourceIPv6 != "" {
			source += " / " + adapter.SourceIPv6
		}
		fmt.Fprintf(w, "网卡 %s (%s):  连接 %d，↑ %s，↓ %s，健康 %s",
			adapter.Name, source, adapter.Connections,
			formatBytes(adapter.BytesUp), formatBytes(adapter.BytesDown),
			translateHealth(adapter.HealthState))
		if adapter.ConsecutiveFailures > 0 {
			fmt.Fprintf(w, "，连续失败 %d", adapter.ConsecutiveFailures)
		}
		if adapter.DomainQuarantines > 0 {
			fmt.Fprintf(w, "，隔离域名 %d", adapter.DomainQuarantines)
		}
		fmt.Fprintln(w)
	}
	if len(telemetry.DomainQuarantines) > 0 {
		fmt.Fprintln(w, "隔离中的域名:")
		for _, quarantine := range telemetry.DomainQuarantines {
			fmt.Fprintf(w, "  %s @ %s（证据 %d，到期 %s）\n",
				quarantine.Domain, quarantine.Adapter, quarantine.Evidence,
				quarantine.ExpiresAt.Local().Format("15:04:05"))
		}
	}
	if len(telemetry.Connections) > 0 {
		fmt.Fprintln(w, "活动连接:")
		for _, connection := range telemetry.Connections {
			fmt.Fprintf(w, "  #%d %s %s → %s（↑ %s，↓ %s）\n",
				connection.ID, connection.Protocol, connection.Adapter,
				firstNonEmpty(connection.Target, connection.Remote, connection.Client),
				formatBytes(connection.BytesUp), formatBytes(connection.BytesDown))
		}
	}
	return nil
}

// commandAdapters implements `hypomux-cli adapters`. It enumerates locally, so
// it works without connecting to the core.
func commandAdapters(c *commandContext) int {
	flags := c.newFlagSet("adapters")
	if err := flags.Parse(c.args); err != nil {
		return 2
	}
	available, err := listLocalAdapters()
	if err != nil {
		fmt.Fprintf(c.stderr, "%v\n", err)
		return 1
	}
	if c.jsonOut {
		data, err := json.MarshalIndent(available, "", "  ")
		if err != nil {
			fmt.Fprintf(c.stderr, "序列化网卡列表失败：%v\n", err)
			return 1
		}
		fmt.Fprintln(c.stdout, string(data))
		return 0
	}
	if len(available) == 0 {
		fmt.Fprintln(c.stdout, "没有发现可用于聚合的网卡（需要处于活动状态且有全局 IPv4/IPv6 地址）")
		return 0
	}
	fmt.Fprintf(c.stdout, "发现 %d 个可用网卡:\n", len(available))
	for _, adapter := range available {
		source := adapter.SourceIP
		if adapter.PrefixLength > 0 {
			source = fmt.Sprintf("%s/%d", source, adapter.PrefixLength)
		}
		if adapter.SourceIPv6 != "" {
			source += " / " + adapter.SourceIPv6
		}
		fmt.Fprintf(c.stdout, "  [%d] %s  %s  (%s，MAC %s)\n",
			adapter.IfIndex, adapter.Name, source, translateKind(adapter.Kind), adapter.MAC)
	}
	fmt.Fprintf(c.stdout, "提示: 启动示例  hypomux-cli start --adapter %q\n", available[0].Name)
	return 0
}

func sortedChannelNames(channels map[string]string) []string {
	names := make([]string, 0, len(channels))
	for name := range channels {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func formatBytes(value uint64) string {
	const unit = 1024
	if value < unit {
		return fmt.Sprintf("%d B", value)
	}
	units := []string{"KiB", "MiB", "GiB", "TiB"}
	display := float64(value)
	index := -1
	for display >= unit && index < len(units)-1 {
		display /= unit
		index++
	}
	return fmt.Sprintf("%.1f %s", display, units[index])
}

func formatDuration(value time.Duration) string {
	value = value.Round(time.Second)
	days := int64(value) / int64(24*time.Hour)
	value -= time.Duration(days) * 24 * time.Hour
	if days > 0 {
		return fmt.Sprintf("%dd%s", days, value.Truncate(time.Second))
	}
	return value.String()
}

func translateHealth(state string) string {
	switch state {
	case "healthy":
		return "健康"
	case "cooldown":
		return "冷却中"
	case "degraded":
		return "降级"
	case "unavailable":
		return "不可用"
	case "":
		return "未知"
	default:
		return state
	}
}

func translateKind(kind string) string {
	if kind == "wifi" {
		return "Wi-Fi"
	}
	return "以太网"
}

func yesNo(value bool) string {
	if value {
		return "是"
	}
	return "否"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
