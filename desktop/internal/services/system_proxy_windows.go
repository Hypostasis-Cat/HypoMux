//go:build windows

package services

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const internetSettingsPath = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`

type proxySnapshot struct {
	HasProxyEnable   bool   `json:"has_proxy_enable"`
	ProxyEnable      uint64 `json:"proxy_enable"`
	HasProxyServer   bool   `json:"has_proxy_server"`
	ProxyServer      string `json:"proxy_server"`
	HasProxyOverride bool   `json:"has_proxy_override"`
	ProxyOverride    string `json:"proxy_override"`
}

func proxyMarkerPath() string {
	return filepath.Join(settingsDirectory(), "proxy-owned")
}

func enableSystemProxy(httpPort int, socksPort int) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, internetSettingsPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("打开 Windows 系统代理设置失败：%w", err)
	}
	defer key.Close()
	enable, _, enableErr := key.GetIntegerValue("ProxyEnable")
	server, _, serverErr := key.GetStringValue("ProxyServer")
	override, _, overrideErr := key.GetStringValue("ProxyOverride")
	snapshot := proxySnapshot{
		HasProxyEnable: enableErr == nil, ProxyEnable: enable,
		HasProxyServer: serverErr == nil, ProxyServer: server,
		HasProxyOverride: overrideErr == nil, ProxyOverride: override,
	}
	data, _ := json.Marshal(snapshot)
	if err := os.MkdirAll(settingsDirectory(), 0o755); err != nil {
		return fmt.Errorf("创建代理恢复目录失败：%w", err)
	}
	if err := os.WriteFile(proxyMarkerPath(), data, 0o600); err != nil {
		return fmt.Errorf("保存代理恢复点失败：%w", err)
	}
	if err := key.SetDWordValue("ProxyEnable", 1); err != nil {
		return fmt.Errorf("启用系统代理失败：%w", err)
	}
	serverValue := fmt.Sprintf("http=127.0.0.1:%d;https=127.0.0.1:%d;socks=127.0.0.1:%d", httpPort, httpPort, socksPort)
	if err := key.SetStringValue("ProxyServer", serverValue); err != nil {
		return fmt.Errorf("写入系统代理地址失败：%w", err)
	}
	if err := key.SetStringValue("ProxyOverride", "<local>;localhost;127.*;10.*;172.16.*;172.17.*;172.18.*;172.19.*;172.2*;172.30.*;172.31.*;192.168.*"); err != nil {
		return fmt.Errorf("写入系统代理绕过列表失败：%w", err)
	}
	return notifyProxyChanged()
}

func restoreSystemProxy() error {
	data, err := os.ReadFile(proxyMarkerPath())
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("读取代理恢复点失败：%w", err)
	}
	var snapshot proxySnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return fmt.Errorf("代理恢复点损坏：%w", err)
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, internetSettingsPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("打开 Windows 系统代理设置失败：%w", err)
	}
	defer key.Close()
	if snapshot.HasProxyEnable {
		err = key.SetDWordValue("ProxyEnable", uint32(snapshot.ProxyEnable))
	} else {
		err = key.DeleteValue("ProxyEnable")
	}
	if err != nil && err != registry.ErrNotExist {
		return fmt.Errorf("恢复代理开关失败：%w", err)
	}
	if snapshot.HasProxyServer {
		err = key.SetStringValue("ProxyServer", snapshot.ProxyServer)
	} else {
		err = key.DeleteValue("ProxyServer")
	}
	if err != nil && err != registry.ErrNotExist {
		return fmt.Errorf("恢复代理地址失败：%w", err)
	}
	if snapshot.HasProxyOverride {
		err = key.SetStringValue("ProxyOverride", snapshot.ProxyOverride)
	} else {
		err = key.DeleteValue("ProxyOverride")
	}
	if err != nil && err != registry.ErrNotExist {
		return fmt.Errorf("恢复代理绕过列表失败：%w", err)
	}
	if err := notifyProxyChanged(); err != nil {
		return err
	}
	if err := os.Remove(proxyMarkerPath()); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("清理代理恢复点失败：%w", err)
	}
	return nil
}

func notifyProxyChanged() error {
	wininet := windows.NewLazySystemDLL("wininet.dll")
	internetSetOption := wininet.NewProc("InternetSetOptionW")
	for _, option := range []uintptr{39, 37} {
		result, _, callErr := internetSetOption.Call(0, option, 0, 0)
		if result == 0 {
			return fmt.Errorf("通知 Windows 刷新代理失败：%v", callErr)
		}
	}
	return nil
}
