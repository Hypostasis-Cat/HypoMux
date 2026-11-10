package main

import (
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// commandTun implements `hypomux-cli tun`:
//
//	tun status       查看托管 TUN 生命周期状态
//	tun deactivate   安全关闭 TUN（可重试）
//
// tun.activate 需要桌面端生成并固定 sing-box 配置摘要，属于特权桌面编排，
// CLI 不暴露该入口。
func commandTun(c *commandContext) int {
	flags := c.newFlagSet("tun")
	if err := flags.Parse(c.args); err != nil {
		return 2
	}
	rest := flags.Args()
	if len(rest) == 0 {
		fmt.Fprintln(c.stderr, "用法: hypomux-cli tun status | hypomux-cli tun deactivate")
		return 2
	}
	switch rest[0] {
	case "status":
		if len(rest) != 1 {
			fmt.Fprintln(c.stderr, "用法: hypomux-cli tun status")
			return 2
		}
		return c.callOnce("tun.status", nil, printTunStatus)
	case "deactivate":
		if len(rest) != 1 {
			fmt.Fprintln(c.stderr, "用法: hypomux-cli tun deactivate")
			return 2
		}
		return c.callOnce("tun.deactivate", nil, printTunDeactivate)
	default:
		fmt.Fprintf(c.stderr, "未知 tun 子命令 %q（可用: status、deactivate）\n", rest[0])
		return 2
	}
}

type tunLifecycleResult struct {
	Accepted bool          `json:"accepted"`
	Tun      tunStatusView `json:"tun"`
}

func printTunStatus(w io.Writer, raw json.RawMessage) error {
	var status tunStatusView
	if err := json.Unmarshal(raw, &status); err != nil {
		return err
	}
	fmt.Fprintf(w, "TUN 状态: %s", status.State)
	if status.PID != 0 {
		fmt.Fprintf(w, "（PID %d）", status.PID)
	}
	if !status.StartedAt.IsZero() {
		fmt.Fprintf(w, "，启动于 %s", status.StartedAt.Local().Format("2006-01-02 15:04:05"))
	}
	fmt.Fprintln(w)
	if status.ConfigPath != "" {
		fmt.Fprintf(w, "配置文件: %s\n", status.ConfigPath)
	}
	if status.LastError != "" {
		fmt.Fprintf(w, "最近错误: %s\n", status.LastError)
	}
	return nil
}

func printTunDeactivate(w io.Writer, raw json.RawMessage) error {
	var result tunLifecycleResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return err
	}
	if result.Accepted {
		fmt.Fprintf(w, "TUN 已关闭（当前状态 %s）\n", result.Tun.State)
		return nil
	}
	fmt.Fprintf(w, "TUN 本就未激活（当前状态 %s）\n", result.Tun.State)
	return nil
}

// commandDiagnose implements `hypomux-cli diagnose` (diagnostic.run). The
// engine executes the probe from its own process, so the probe path matches
// what aggregation traffic actually takes.
func commandDiagnose(c *commandContext) int {
	flags := c.newFlagSet("diagnose")
	sourceIP := flags.String("src-ip", "", "源 IPv4 地址（网卡上绑定的出站地址）")
	targetIP := flags.String("target-ip", "223.5.5.5", "目标 IPv4 地址")
	count := flags.Int("count", 10, "探测次数")
	timeoutMS := flags.Int("timeout-ms", 1000, "单次探测超时")
	if err := flags.Parse(c.args); err != nil {
		return 2
	}
	if flags.NArg() > 0 {
		fmt.Fprintf(c.stderr, "未知参数 %q\n", flags.Arg(0))
		return 2
	}
	if *count < 1 || *count > 100 || *timeoutMS < 1 || *timeoutMS > 10000 {
		fmt.Fprintln(c.stderr, "--count 必须在 1 到 100 之间，--timeout-ms 必须在 1 到 10000 之间")
		return 2
	}
	params := map[string]any{
		"src_ip":     stringsOrEmpty(*sourceIP),
		"target_ip":  stringsOrEmpty(*targetIP),
		"count":      *count,
		"timeout_ms": *timeoutMS,
	}
	// Probe time scales with count * timeout; the shared default would cut
	// long probes short.
	c.timeout = maxDuration(c.timeout, time.Duration(*count+2)*time.Duration(*timeoutMS)*time.Millisecond)
	return c.callOnce("diagnostic.run", params, printDiagnose)
}

type diagnoseResult struct {
	Status       string  `json:"status"`
	LossRate     float64 `json:"loss_rate"`
	AvgLatencyMS float64 `json:"avg_latency_ms"`
	JitterMS     float64 `json:"jitter_ms"`
	Sent         int     `json:"sent"`
	Received     int     `json:"received"`
	SrcIP        string  `json:"src_ip"`
	TargetIP     string  `json:"target_ip"`
	Note         string  `json:"note"`
}

func printDiagnose(w io.Writer, raw json.RawMessage) error {
	var result diagnoseResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return err
	}
	fmt.Fprintf(w, "链路诊断 %s → %s:  %s\n", result.SrcIP, result.TargetIP, translateProbeStatus(result.Status))
	fmt.Fprintf(w, "  发送 %d，接收 %d，丢包率 %.1f%%\n", result.Sent, result.Received, result.LossRate)
	if result.Received > 0 {
		fmt.Fprintf(w, "  平均延迟 %.1f ms，抖动 %.1f ms\n", result.AvgLatencyMS, result.JitterMS)
	}
	if result.Note != "" {
		fmt.Fprintf(w, "  说明: %s\n", result.Note)
	}
	return nil
}

func translateProbeStatus(status string) string {
	switch status {
	case "available":
		return "可用"
	case "unstable":
		return "不稳定"
	case "unavailable":
		return "不可用"
	default:
		return status
	}
}

func stringsOrEmpty(value string) string {
	if value == "" {
		return ""
	}
	return value
}

func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}
