// Command hypomux-cli is the official command-line client for the HypoMux
// aggregation engine. It speaks protocol v1 over the authenticated local Core
// Service pipe and is authenticated by the same executable path and SHA-256
// fingerprint mechanism as the desktop client: the service only accepts an
// installed, pinned hypomux-cli.exe. The matched CLI pin grants a restricted
// client role (proxy-mode aggregation and read-only diagnostics, no privileged
// TUN/MTU/WFP orchestration), enforced server-side, and every invocation is a
// short-lived session of the one shared service engine.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"
)

var (
	version = "dev"
	commit  = "unknown"
)

// defaultCallTimeout bounds every core call; long probes override it.
const defaultCallTimeout = 30 * time.Second

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, newServiceTransport))
}

type transportFactory func() transport

// commandContext carries one parsed command invocation. The shared flags
// (--json, --timeout) are registered into it by the per-command flag set.
type commandContext struct {
	args    []string
	stdout  io.Writer
	stderr  io.Writer
	factory transportFactory
	jsonOut bool
	timeout time.Duration
}

type cliCommand struct {
	name    string
	summary string
	run     func(*commandContext) int
}

// commands is populated in init to break the package initialization cycle:
// the table references commandHelp, whose output renders this very table.
var commands []cliCommand

func init() {
	commands = []cliCommand{
		{name: "status", summary: "查看聚合引擎当前状态", run: commandStatus},
		{name: "hello", summary: "查看引擎版本、能力与运行身份", run: commandHello},
		{name: "health", summary: "核心存活检查", run: commandHealth},
		{name: "start", summary: "以系统代理模式启动聚合引擎", run: commandStart},
		{name: "stop", summary: "停止聚合引擎", run: commandStop},
		{name: "telemetry", summary: "查看实时遥测（速率、健康、连接）", run: commandTelemetry},
		{name: "adapters", summary: "列出本机可用于聚合的网卡", run: commandAdapters},
		{name: "diagnose", summary: "对指定链路做丢包/延迟/抖动探测", run: commandDiagnose},
		{name: "rules", summary: "查看或调整域名隔离规则", run: commandRules},
		{name: "dns", summary: "域名解析与 DNS 状态", run: commandDNS},
		{name: "tun", summary: "查看或关闭 TUN 状态", run: commandTun},
		{name: "raw", summary: "调用任意已协商的协议 v1 方法", run: commandRaw},
		{name: "version", summary: "显示 CLI 版本", run: commandVersion},
		{name: "help", summary: "显示帮助", run: commandHelp},
	}
}

func run(args []string, stdout, stderr io.Writer, factory transportFactory) int {
	invocation := &commandContext{
		stdout:  stdout,
		stderr:  stderr,
		factory: factory,
		timeout: defaultCallTimeout,
	}
	if len(args) == 0 {
		fmt.Fprintln(stderr, "缺少命令；用法见 hypomux-cli help")
		return 2
	}
	name := args[0]
	for _, command := range commands {
		if command.name == name {
			invocation.args = args[1:]
			return command.run(invocation)
		}
	}
	fmt.Fprintf(stderr, "未知命令 %q；用法见 hypomux-cli help\n", name)
	return 2
}

func (c *commandContext) newFlagSet(name string) *flag.FlagSet {
	set := flag.NewFlagSet(name, flag.ContinueOnError)
	set.SetOutput(c.stderr)
	set.BoolVar(&c.jsonOut, "json", false, "以 JSON 输出原始协议结果")
	set.DurationVar(&c.timeout, "timeout", c.timeout, "单次核心调用超时（如 30s、1m）")
	return set
}

// callOnce performs the common one-request command flow: dial, call, emit the
// raw protocol result, close. The callback renders the human-readable output.
func (c *commandContext) callOnce(
	method string,
	params any,
	print func(io.Writer, json.RawMessage) error,
) int {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	conn, err := c.factory().Connect(ctx)
	if err != nil {
		fmt.Fprintf(c.stderr, "%v\n", err)
		return 1
	}
	s := newSession(conn, c.stderr)
	defer s.Close()
	var raw json.RawMessage
	if err := s.call(ctx, method, params, &raw); err != nil {
		fmt.Fprintf(c.stderr, "%s 失败：%v\n", method, err)
		return 1
	}
	if c.jsonOut {
		return c.printRaw(raw)
	}
	if print == nil {
		return 0
	}
	if err := print(c.stdout, raw); err != nil {
		fmt.Fprintf(c.stderr, "输出结果失败：%v\n", err)
		return 1
	}
	return 0
}

func (c *commandContext) printRaw(raw json.RawMessage) int {
	var buffer bytes.Buffer
	if err := json.Indent(&buffer, raw, "", "  "); err != nil {
		fmt.Fprintf(c.stderr, "格式化 JSON 失败：%v\n", err)
		return 1
	}
	fmt.Fprintln(c.stdout, buffer.String())
	return 0
}

func commandVersion(c *commandContext) int {
	fmt.Fprintf(c.stdout, "hypomux-cli %s (%s)\n", version, commit)
	return 0
}

func commandHelp(c *commandContext) int {
	c.printUsage(c.stdout)
	return 0
}

func (c *commandContext) printUsage(w io.Writer) {
	fmt.Fprintf(w, "hypomux-cli %s — HypoMux 官方命令行客户端（协议 v1，经 Core 服务管道认证）\n\n", version)
	fmt.Fprintln(w, "用法: hypomux-cli <命令> [参数] [--json] [--timeout 时长]")
	fmt.Fprintln(w, "全局选项:")
	fmt.Fprintln(w, "  --json      输出原始协议 v1 结果 JSON（适合脚本与 AI Agent）")
	fmt.Fprintln(w, "  --timeout   单次核心调用超时，默认 30s")
	fmt.Fprintln(w, "\n命令:")
	width := 0
	for _, command := range commands {
		if len(command.name) > width {
			width = len(command.name)
		}
	}
	for _, command := range commands {
		fmt.Fprintf(w, "  %-*s  %s\n", width, command.name, command.summary)
	}
	fmt.Fprintln(w, "\n示例:")
	fmt.Fprintln(w, "  hypomux-cli adapters")
	fmt.Fprintln(w, "  hypomux-cli start --all")
	fmt.Fprintln(w, "  hypomux-cli start --adapter Ethernet --adapter \"Wi-Fi:3\" --socks-port 10800 --http-port 10801")
	fmt.Fprintln(w, "  hypomux-cli status --json")
	fmt.Fprintln(w, "  hypomux-cli telemetry --connections")
	fmt.Fprintln(w, "  hypomux-cli diagnose --src-ip 192.168.1.10")
	fmt.Fprintln(w, "  hypomux-cli dns resolve example.com")
	fmt.Fprintln(w, "  hypomux-cli raw engine.telemetry '{\"include_connections\":true}'")
}
