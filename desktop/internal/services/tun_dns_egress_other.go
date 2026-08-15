//go:build !windows

package services

import "errors"

func systemDefaultDNSAdapterID() (string, error) {
	return "", errors.New("当前平台不支持读取系统默认路由")
}
