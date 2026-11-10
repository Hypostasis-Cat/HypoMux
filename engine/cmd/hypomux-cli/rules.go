package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// commandRules implements `hypomux-cli rules` over the protocol v1
// domain-quarantine surface:
//
//	rules                    查看当前隔离策略与隔离中的域名
//	rules clear              清空当前运行的隔离记录
//	rules expiry on|off      开关自动到期策略（无需重启）
//	rules remove ADAPTER DOMAIN   移除一条隔离记录
//
// 协议 v1 的进程/域名/IP 分流规则保存在桌面端设置中，引擎只持有运行期的
// 域名隔离状态，所以 CLI 的“规则”面就是这块公共边界。
func commandRules(c *commandContext) int {
	flags := c.newFlagSet("rules")
	if err := flags.Parse(c.args); err != nil {
		return 2
	}
	rest := flags.Args()
	if len(rest) == 0 {
		return c.showRules()
	}
	switch rest[0] {
	case "clear":
		if len(rest) != 1 {
			fmt.Fprintln(c.stderr, "用法: hypomux-cli rules clear")
			return 2
		}
		return c.updateQuarantine(map[string]any{"clear": true})
	case "expiry":
		if len(rest) != 2 {
			fmt.Fprintln(c.stderr, "用法: hypomux-cli rules expiry on|off")
			return 2
		}
		switch strings.ToLower(rest[1]) {
		case "on", "true", "yes":
			return c.updateQuarantine(map[string]any{"expiry": true})
		case "off", "false", "no":
			return c.updateQuarantine(map[string]any{"expiry": false})
		default:
			fmt.Fprintf(c.stderr, "expiry 只接受 on 或 off，收到 %q\n", rest[1])
			return 2
		}
	case "remove":
		if len(rest) != 3 {
			fmt.Fprintln(c.stderr, "用法: hypomux-cli rules remove 网卡名称 域名")
			return 2
		}
		return c.updateQuarantine(map[string]any{
			"remove": []map[string]string{
				{"adapter": rest[1], "domain": rest[2]},
			},
		})
	default:
		fmt.Fprintf(c.stderr, "未知 rules 子命令 %q（可用: clear、expiry、remove）\n", rest[0])
		return 2
	}
}

func (c *commandContext) updateQuarantine(params map[string]any) int {
	return c.callOnce("domain_quarantine.update", params, printQuarantineUpdate)
}

type quarantineUpdateResult struct {
	Applied bool `json:"applied"`
}

func printQuarantineUpdate(w io.Writer, raw json.RawMessage) error {
	var result quarantineUpdateResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return err
	}
	if !result.Applied {
		fmt.Fprintln(w, "未应用：聚合引擎尚未启动（隔离规则只作用于当前运行）")
		return nil
	}
	fmt.Fprintln(w, "隔离规则已更新")
	return nil
}

func (c *commandContext) showRules() int {
	params := map[string]any{"include_connections": false}
	return c.callOnce("engine.telemetry", params, printRules)
}

func printRules(w io.Writer, raw json.RawMessage) error {
	var telemetry telemetryResult
	if err := json.Unmarshal(raw, &telemetry); err != nil {
		return err
	}
	if telemetry.DomainIsolationExpiry != nil {
		policy := "已关闭（隔离记录在手动清除前一直生效）"
		if *telemetry.DomainIsolationExpiry {
			policy = "已开启（隔离记录约 30 分钟后自动到期）"
		}
		fmt.Fprintf(w, "自动到期:  %s\n", policy)
	} else {
		fmt.Fprintln(w, "自动到期:  未知（引擎未运行或核心不支持该能力）")
	}
	if len(telemetry.DomainQuarantines) == 0 {
		fmt.Fprintln(w, "当前没有隔离中的域名")
		return nil
	}
	fmt.Fprintf(w, "隔离中的域名 (%d):\n", len(telemetry.DomainQuarantines))
	for _, quarantine := range telemetry.DomainQuarantines {
		fmt.Fprintf(w, "  %s @ %s（证据 %d，到期 %s）\n",
			quarantine.Domain, quarantine.Adapter, quarantine.Evidence,
			quarantine.ExpiresAt.Local().Format("15:04:05"))
	}
	return nil
}
