//go:build windows

package services

// 从 Windows 注册表读取系统代理设置（与 HypoMux 系统代理模式兼容）。

import (
	"net/url"
	"strings"

	"golang.org/x/sys/windows/registry"
)

func downloadRegistryProxy() (*url.URL, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Internet Settings`, registry.QUERY_VALUE)
	if err != nil {
		return nil, nil // 无系统代理 → 直连
	}
	defer key.Close()
	enable, _, err := key.GetIntegerValue("ProxyEnable")
	if err != nil || enable == 0 {
		return nil, nil
	}
	server, _, err := key.GetStringValue("ProxyServer")
	if err != nil || server == "" {
		return nil, nil
	}
	return parseWindowsProxyServer(server), nil
}

// parseWindowsProxyServer 解析 ProxyServer 值：
// 简单形式 "127.0.0.1:7890" 或按协议区分 "http=...;https=...;socks=..."。
func parseWindowsProxyServer(server string) *url.URL {
	var httpProxy string
	for _, part := range strings.Split(server, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if scheme, addr, found := strings.Cut(part, "="); found {
			if strings.EqualFold(scheme, "http") || strings.EqualFold(scheme, "https") {
				httpProxy = addr
			}
		} else {
			httpProxy = part
		}
	}
	if httpProxy == "" {
		return nil
	}
	proxyURL, err := url.Parse("http://" + strings.TrimPrefix(httpProxy, "http://"))
	if err != nil || proxyURL.Host == "" {
		return nil
	}
	return proxyURL
}
