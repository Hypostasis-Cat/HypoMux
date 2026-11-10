package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// commandRaw implements `hypomux-cli raw METHOD [JSON]`, the protocol v1
// escape hatch: any method the engine advertises in engine.hello.capabilities
// is callable with raw JSON params. New additive methods become usable
// immediately without a dedicated CLI command.
func commandRaw(c *commandContext) int {
	flags := c.newFlagSet("raw")
	if err := flags.Parse(c.args); err != nil {
		return 2
	}
	rest := flags.Args()
	if len(rest) == 0 || len(rest) > 2 {
		fmt.Fprintln(c.stderr, "用法: hypomux-cli raw <方法> [JSON 参数]")
		fmt.Fprintln(c.stderr, "示例: hypomux-cli raw engine.telemetry '{\"include_connections\":true}'")
		return 2
	}
	method := strings.TrimSpace(rest[0])
	var params json.RawMessage
	if len(rest) == 2 {
		decoder := json.NewDecoder(strings.NewReader(rest[1]))
		if err := decoder.Decode(&params); err != nil {
			fmt.Fprintf(c.stderr, "JSON 参数无效：%v\n", err)
			return 2
		}
		if decoder.More() {
			fmt.Fprintln(c.stderr, "JSON 参数包含多余内容")
			return 2
		}
	}

	ctx, cancel := contextWithBackgroundTimeout(c.timeout)
	defer cancel()
	conn, err := c.factory().Connect(ctx)
	if err != nil {
		fmt.Fprintf(c.stderr, "%v\n", err)
		return 1
	}
	s := newSession(conn, c.stderr)
	defer s.Close()

	var hello helloResult
	if err := s.call(ctx, "engine.hello", nil, &hello); err != nil {
		fmt.Fprintf(c.stderr, "engine.hello 失败：%v\n", err)
		return 1
	}
	if !containsString(hello.Capabilities, method) {
		fmt.Fprintf(c.stderr, "核心能力列表中没有 %q。可用方法:\n", method)
		for _, capability := range hello.Capabilities {
			fmt.Fprintf(c.stderr, "  %s\n", capability)
		}
		return 1
	}

	var raw json.RawMessage
	if err := s.call(ctx, method, params, &raw); err != nil {
		fmt.Fprintf(c.stderr, "%s 失败：%v\n", method, err)
		return 1
	}
	var buffer bytes.Buffer
	if err := json.Indent(&buffer, raw, "", "  "); err != nil {
		fmt.Fprintf(c.stderr, "格式化 JSON 失败：%v\n", err)
		return 1
	}
	fmt.Fprintln(c.stdout, buffer.String())
	return 0
}

func contextWithBackgroundTimeout(timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), timeout)
}
