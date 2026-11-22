//go:build !windows

package services

import "net/url"

// 非 Windows 平台无注册表系统代理，返回直连。

func downloadRegistryProxy() (*url.URL, error) {
	return nil, nil
}
