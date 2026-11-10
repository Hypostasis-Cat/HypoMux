package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// commandDNS implements `hypomux-cli dns`:
//
//	dns resolve 域名 [--adapter 名称] [--type A|AAAA|HTTPS] [--bootstrap] [--timeout-ms N]
//	dns status
//
// Both methods require a running engine, matching the protocol v1 contract.
func commandDNS(c *commandContext) int {
	flags := c.newFlagSet("dns")
	if err := flags.Parse(c.args); err != nil {
		return 2
	}
	rest := flags.Args()
	if len(rest) == 0 {
		fmt.Fprintln(c.stderr, "用法: hypomux-cli dns resolve 域名 | hypomux-cli dns status")
		return 2
	}
	switch rest[0] {
	case "resolve":
		if len(rest) < 2 {
			fmt.Fprintln(c.stderr, "用法: hypomux-cli dns resolve 域名 [--adapter 名称] [--type A|AAAA|HTTPS] [--bootstrap] [--timeout-ms N]")
			return 2
		}
		return c.dnsResolve(rest[1], rest[2:])
	case "status":
		if len(rest) != 1 {
			fmt.Fprintln(c.stderr, "用法: hypomux-cli dns status")
			return 2
		}
		return c.callOnce("dns.status", nil, printDNSStatus)
	default:
		fmt.Fprintf(c.stderr, "未知 dns 子命令 %q（可用: resolve、status）\n", rest[0])
		return 2
	}
}

func (c *commandContext) dnsResolve(domain string, args []string) int {
	resolveFlags := c.newFlagSet("dns resolve")
	adapter := resolveFlags.String("adapter", "", "通过指定网卡的 DNS 解析")
	recordType := resolveFlags.String("type", "A", "记录类型: A | AAAA | HTTPS")
	bootstrap := resolveFlags.Bool("bootstrap", false, "使用源地址绑定的传统 DNS 引导解析")
	timeoutMS := resolveFlags.Int("timeout-ms", 0, "查询超时（毫秒，上限 30000）")
	if err := resolveFlags.Parse(args); err != nil {
		return 2
	}
	*recordType = strings.ToUpper(strings.TrimSpace(*recordType))
	switch *recordType {
	case "A", "AAAA", "HTTPS":
	default:
		fmt.Fprintf(c.stderr, "不支持的记录类型 %q（可用: A、AAAA、HTTPS）\n", *recordType)
		return 2
	}
	if *timeoutMS < 0 || *timeoutMS > 30000 {
		fmt.Fprintln(c.stderr, "--timeout-ms 必须在 0 到 30000 之间")
		return 2
	}
	params := map[string]any{
		"domain":      domain,
		"adapter":     strings.TrimSpace(*adapter),
		"record_type": *recordType,
		"bootstrap":   *bootstrap,
	}
	if *timeoutMS > 0 {
		params["timeout_ms"] = *timeoutMS
	}
	return c.callOnce("dns.resolve", params, printDNSResolve)
}

type dnsResolveResult struct {
	Domain     string   `json:"domain"`
	Address    string   `json:"address"`
	RecordType string   `json:"record_type"`
	Adapter    string   `json:"adapter,omitempty"`
	Transport  string   `json:"transport,omitempty"`
	Server     string   `json:"server,omitempty"`
	Cached     bool     `json:"cached"`
	Addresses  []string `json:"addresses,omitempty"`
}

func printDNSResolve(w io.Writer, raw json.RawMessage) error {
	var result dnsResolveResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return err
	}
	fmt.Fprintf(w, "%s %s = %s", result.RecordType, result.Domain, result.Address)
	if len(result.Addresses) > 1 {
		fmt.Fprintf(w, "（共 %d 个地址）", len(result.Addresses))
	}
	fmt.Fprintln(w)
	detail := make([]string, 0, 4)
	if result.Adapter != "" {
		detail = append(detail, "网卡 "+result.Adapter)
	}
	if result.Transport != "" {
		detail = append(detail, "传输 "+result.Transport)
	}
	if result.Server != "" {
		detail = append(detail, "服务器 "+result.Server)
	}
	if result.Cached {
		detail = append(detail, "缓存命中")
	}
	if len(detail) > 0 {
		fmt.Fprintf(w, "  %s\n", strings.Join(detail, "，"))
	}
	return nil
}

func printDNSStatus(w io.Writer, raw json.RawMessage) error {
	var buffer bytes.Buffer
	if err := json.Indent(&buffer, raw, "", "  "); err != nil {
		return err
	}
	fmt.Fprintln(w, buffer.String())
	return nil
}
