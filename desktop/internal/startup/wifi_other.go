//go:build !windows

package startup

import "errors"

func openWiFiSession() (wifiSession, error) {
	return nil, errors.New("开机自动连接 Wi-Fi 仅支持 Windows")
}
