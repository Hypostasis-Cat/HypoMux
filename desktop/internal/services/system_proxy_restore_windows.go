//go:build windows

package services

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows/registry"
)

type proxyRegistryKey interface {
	GetIntegerValue(string) (uint64, uint32, error)
	GetStringValue(string) (string, uint32, error)
	SetDWordValue(string, uint32) error
	SetStringValue(string, string) error
	DeleteValue(string) error
}

type proxyRegistryValues struct {
	HasEnable   bool   `json:"has_enable"`
	Enable      uint64 `json:"enable"`
	HasServer   bool   `json:"has_server"`
	Server      string `json:"server"`
	HasOverride bool   `json:"has_override"`
	Override    string `json:"override"`
}

type proxyRecoveryActions struct {
	save   func(proxySnapshot) error
	notify func() error
	remove func() error
}

func readProxyRegistry(key proxyRegistryKey) (proxyRegistryValues, error) {
	var values proxyRegistryValues
	enable, _, err := key.GetIntegerValue("ProxyEnable")
	if err != nil && !errors.Is(err, registry.ErrNotExist) {
		return values, err
	}
	values.HasEnable, values.Enable = err == nil, enable
	server, _, err := key.GetStringValue("ProxyServer")
	if err != nil && !errors.Is(err, registry.ErrNotExist) {
		return values, err
	}
	values.HasServer, values.Server = err == nil, server
	override, _, err := key.GetStringValue("ProxyOverride")
	if err != nil && !errors.Is(err, registry.ErrNotExist) {
		return values, err
	}
	values.HasOverride, values.Override = err == nil, override
	return values, nil
}

func proxyOriginalValues(snapshot proxySnapshot) proxyRegistryValues {
	return proxyRegistryValues{snapshot.HasProxyEnable, snapshot.ProxyEnable,
		snapshot.HasProxyServer, snapshot.ProxyServer, snapshot.HasProxyOverride, snapshot.ProxyOverride}
}

// Each field can be at either end of our restoration. Any third value is an
// external edit, including an edit made after a failed restoration attempt.
func proxyRestoreValuesMatch(current, from, original proxyRegistryValues) bool {
	equal := func(hasA bool, a any, hasB bool, b any) bool { return hasA == hasB && (!hasA || a == b) }
	return (equal(current.HasEnable, current.Enable, from.HasEnable, from.Enable) || equal(current.HasEnable, current.Enable, original.HasEnable, original.Enable)) &&
		(equal(current.HasServer, current.Server, from.HasServer, from.Server) || equal(current.HasServer, current.Server, original.HasServer, original.Server)) &&
		(equal(current.HasOverride, current.Override, from.HasOverride, from.Override) || equal(current.HasOverride, current.Override, original.HasOverride, original.Override))
}

func restoreProxySnapshot(key proxyRegistryKey, snapshot proxySnapshot, actions proxyRecoveryActions) (string, error) {
	current, err := readProxyRegistry(key)
	if err != nil {
		return "", fmt.Errorf("读取当前系统代理设置失败：%w", err)
	}
	original := proxyOriginalValues(snapshot)
	from := current
	if snapshot.State == "restoring" {
		if snapshot.RestoreFrom == nil {
			return "", fmt.Errorf("代理恢复点缺少恢复前状态")
		}
		from = *snapshot.RestoreFrom
	} else if snapshot.Version >= 1 && snapshot.OwnedServer != "" {
		override := snapshot.OwnedOverride
		if override == "" {
			override = legacySystemProxyBypass
		}
		from = proxyRegistryValues{true, 1, true, snapshot.OwnedServer, true, override}
	}
	matches := proxyRestoreValuesMatch(current, from, original)
	if snapshot.Version >= 2 && snapshot.State == "active" {
		// New markers always journal before restoration, so an active marker
		// cannot describe our partial restore. Even a user edit back to an
		// original value (for example disabling the proxy) ends ownership.
		matches = current == from
	}
	if !matches {
		if err := actions.remove(); err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf("用户已修改系统代理，但清理旧恢复点失败：%w", err)
		}
		return "检测到系统代理已由用户或其他软件修改，HypoMux 未覆盖该设置", nil
	}
	if snapshot.State != "restoring" {
		// Commit the journal before the first registry write. Persist the actual
		// starting values also for legacy markers, whose ownership fields are absent.
		snapshot.Version, snapshot.State, snapshot.RestoreFrom = 2, "restoring", &current
		if err := actions.save(snapshot); err != nil {
			return "", fmt.Errorf("保存代理恢复进度失败：%w", err)
		}
	}
	if snapshot.HasProxyEnable {
		err = key.SetDWordValue("ProxyEnable", uint32(snapshot.ProxyEnable))
	} else {
		err = key.DeleteValue("ProxyEnable")
		if errors.Is(err, registry.ErrNotExist) {
			err = nil
		}
	}
	if err != nil {
		return "", fmt.Errorf("恢复代理开关失败：%w", err)
	}
	if snapshot.HasProxyServer {
		err = key.SetStringValue("ProxyServer", snapshot.ProxyServer)
	} else {
		err = key.DeleteValue("ProxyServer")
		if errors.Is(err, registry.ErrNotExist) {
			err = nil
		}
	}
	if err != nil {
		return "", fmt.Errorf("恢复代理地址失败：%w", err)
	}
	if snapshot.HasProxyOverride {
		err = key.SetStringValue("ProxyOverride", snapshot.ProxyOverride)
	} else {
		err = key.DeleteValue("ProxyOverride")
		if errors.Is(err, registry.ErrNotExist) {
			err = nil
		}
	}
	if err != nil {
		return "", fmt.Errorf("恢复代理绕过列表失败：%w", err)
	}
	if err := actions.notify(); err != nil {
		return "", err
	}
	if err := actions.remove(); err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("清理代理恢复点失败：%w", err)
	}
	return "", nil
}
