//go:build !windows

package services

import (
	"context"
	"errors"
	"os/exec"
)

func hotspotSupported() bool { return false }
func listHotspotAdapters(context.Context) ([]HotspotAdapter, error) {
	return nil, errors.New("热点发射网卡选择仅支持 Windows")
}
func protectHotspotPreferences(data []byte, encrypt bool) ([]byte, error) {
	return nil, errors.New("热点配置加密仅支持 Windows")
}
func hotspotCommand() (*exec.Cmd, error) {
	return nil, errors.New("聚合热点目前仅支持 Windows 10/11")
}
