//go:build windows

package services

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const internetSettingsPath = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`

type proxySnapshot struct {
	Version          int    `json:"version,omitempty"`
	State            string `json:"state,omitempty"`
	OwnedServer      string `json:"owned_server,omitempty"`
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
	if _, err := restoreSystemProxyDetailed(); err != nil {
		return fmt.Errorf("启用代理前恢复上次状态失败：%w", err)
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, internetSettingsPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("打开 Windows 系统代理设置失败：%w", err)
	}
	defer key.Close()
	enable, _, enableErr := key.GetIntegerValue("ProxyEnable")
	server, _, serverErr := key.GetStringValue("ProxyServer")
	override, _, overrideErr := key.GetStringValue("ProxyOverride")
	serverValue := fmt.Sprintf("http=127.0.0.1:%d;https=127.0.0.1:%d;socks=127.0.0.1:%d", httpPort, httpPort, socksPort)
	snapshot := proxySnapshot{
		Version: 1, State: "prepared", OwnedServer: serverValue,
		HasProxyEnable: enableErr == nil, ProxyEnable: enable,
		HasProxyServer: serverErr == nil, ProxyServer: server,
		HasProxyOverride: overrideErr == nil, ProxyOverride: override,
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("生成代理恢复点失败：%w", err)
	}
	if err := atomicWriteFile(proxyMarkerPath(), data, 0o600); err != nil {
		return fmt.Errorf("保存代理恢复点失败：%w", err)
	}
	if err := key.SetDWordValue("ProxyEnable", 1); err != nil {
		return fmt.Errorf("启用系统代理失败：%w", err)
	}
	if err := key.SetStringValue("ProxyServer", serverValue); err != nil {
		return fmt.Errorf("写入系统代理地址失败：%w", err)
	}
	if err := key.SetStringValue("ProxyOverride", "<local>;localhost;127.*;10.*;172.16.*;172.17.*;172.18.*;172.19.*;172.2*;172.30.*;172.31.*;192.168.*"); err != nil {
		return fmt.Errorf("写入系统代理绕过列表失败：%w", err)
	}
	if err := notifyProxyChanged(); err != nil {
		return err
	}
	snapshot.State = "active"
	activeData, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("更新代理恢复点失败：%w", err)
	}
	if err := atomicWriteFile(proxyMarkerPath(), activeData, 0o600); err != nil {
		return fmt.Errorf("提交代理所有权状态失败：%w", err)
	}
	return nil
}

func restoreSystemProxy() error {
	_, err := restoreSystemProxyDetailed()
	return err
}

func restoreSystemProxyDetailed() (string, error) {
	data, err := os.ReadFile(proxyMarkerPath())
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("读取代理恢复点失败：%w", err)
	}
	var snapshot proxySnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return recoverCorruptProxyMarker(err)
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, internetSettingsPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return "", fmt.Errorf("打开 Windows 系统代理设置失败：%w", err)
	}
	defer key.Close()
	if snapshot.Version >= 1 && snapshot.State == "active" && snapshot.OwnedServer != "" {
		currentServer, _, currentErr := key.GetStringValue("ProxyServer")
		currentEnable, _, enableErr := key.GetIntegerValue("ProxyEnable")
		if (currentErr == nil && currentServer != snapshot.OwnedServer) || (enableErr == nil && currentEnable != 1) {
			if removeErr := os.Remove(proxyMarkerPath()); removeErr != nil && !os.IsNotExist(removeErr) {
				return "", fmt.Errorf("用户已修改系统代理，但清理旧恢复点失败：%w", removeErr)
			}
			return "检测到系统代理已由用户或其他软件修改，HypoMux 未覆盖该设置", nil
		}
	}
	if snapshot.HasProxyEnable {
		err = key.SetDWordValue("ProxyEnable", uint32(snapshot.ProxyEnable))
	} else {
		err = key.DeleteValue("ProxyEnable")
	}
	if err != nil && err != registry.ErrNotExist {
		return "", fmt.Errorf("恢复代理开关失败：%w", err)
	}
	if snapshot.HasProxyServer {
		err = key.SetStringValue("ProxyServer", snapshot.ProxyServer)
	} else {
		err = key.DeleteValue("ProxyServer")
	}
	if err != nil && err != registry.ErrNotExist {
		return "", fmt.Errorf("恢复代理地址失败：%w", err)
	}
	if snapshot.HasProxyOverride {
		err = key.SetStringValue("ProxyOverride", snapshot.ProxyOverride)
	} else {
		err = key.DeleteValue("ProxyOverride")
	}
	if err != nil && err != registry.ErrNotExist {
		return "", fmt.Errorf("恢复代理绕过列表失败：%w", err)
	}
	if err := notifyProxyChanged(); err != nil {
		return "", err
	}
	if err := os.Remove(proxyMarkerPath()); err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("清理代理恢复点失败：%w", err)
	}
	return "", nil
}

var hypoMuxProxyServerPattern = regexp.MustCompile(`^http=127\.0\.0\.1:[0-9]{1,5};https=127\.0\.0\.1:[0-9]{1,5};socks=127\.0\.0\.1:[0-9]{1,5}$`)

func recoverCorruptProxyMarker(parseErr error) (string, error) {
	corruptPath := fmt.Sprintf("%s.corrupt-%d", proxyMarkerPath(), time.Now().Unix())
	if err := os.Rename(proxyMarkerPath(), corruptPath); err != nil {
		return "", fmt.Errorf("代理恢复点损坏且无法隔离：%w（原始错误：%v）", err, parseErr)
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, internetSettingsPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return "", fmt.Errorf("代理恢复点损坏，且无法检查当前系统代理：%w", err)
	}
	defer key.Close()
	server, _, serverErr := key.GetStringValue("ProxyServer")
	if serverErr != nil || !hypoMuxProxyServerPattern.MatchString(server) {
		return "代理恢复点损坏，已保留诊断副本；当前代理不属于可确认的 HypoMux 本地端口，因此未覆盖用户设置", nil
	}
	if err := key.SetDWordValue("ProxyEnable", 0); err != nil {
		return "", fmt.Errorf("代理恢复点损坏，关闭遗留 HypoMux 代理失败：%w", err)
	}
	if err := key.SetStringValue("ProxyServer", ""); err != nil {
		return "", fmt.Errorf("代理恢复点损坏，清理遗留 HypoMux 代理地址失败：%w", err)
	}
	if err := notifyProxyChanged(); err != nil {
		return "", err
	}
	return "代理恢复点损坏，已安全关闭确认属于 HypoMux 的本地系统代理，并保留诊断副本", nil
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
