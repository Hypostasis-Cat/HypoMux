package services

import (
	"fmt"
	"slices"

	"github.com/Hypostasis-Cat/HypoMux/desktop/internal/engineclient"
)

func validateIPv6PolicyCore(disabled bool, hello engineclient.Hello, mode string) error {
	if !disabled {
		return nil
	}
	if mode == "tun" {
		mode = "tun_tcp_pool"
	}
	if !slices.Contains(hello.ModeFeatures[mode], "disable_ipv6") {
		return fmt.Errorf("当前连接的 Core %s 不支持禁用 IPv6，尚未接管网络；请使用本测试版配套的新核心，避免继续连接旧版 HypoMuxCore 服务", hello.EngineVersion)
	}
	return nil
}
