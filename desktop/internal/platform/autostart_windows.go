//go:build windows

package platform

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"

	"golang.org/x/sys/windows/registry"
)

const (
	autostartRegistryKey       = `Software\Microsoft\Windows\CurrentVersion\Run`
	startupApprovedRegistryKey = `Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\Run`
	autostartValueName         = "HypoMux"
)

func autostartCommand(executable string) string {
	return fmt.Sprintf(`"%s" --silent`, executable)
}

func autostartCommandMatches(value, executable string) bool {
	return strings.EqualFold(strings.TrimSpace(value), autostartCommand(executable))
}

// Windows records a Task Manager "Disable" separately from the Run value.
// Byte 0 is 3 for a disabled entry and 2 for an enabled entry. Unknown or
// absent data is left to Windows instead of incorrectly reporting it disabled.
func startupApprovalAllows(data []byte) bool {
	return len(data) == 0 || data[0] != 3
}

func SetAutostart(enabled bool) error {
	if !enabled {
		key, err := registry.OpenKey(registry.CURRENT_USER, autostartRegistryKey, registry.SET_VALUE)
		if errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("关闭开机自启失败：%w", err)
		}
		defer key.Close()
		if err := key.DeleteValue(autostartValueName); err != nil && !errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
			return fmt.Errorf("关闭开机自启失败：%w", err)
		}
		return nil
	}

	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("无法定位 HypoMux 程序：%w", err)
	}
	// Re-enabling in HypoMux is an explicit user action. Remove a stale Task
	// Manager approval record first; otherwise a previously disabled entry can
	// exist in Run while Windows silently refuses to launch it.
	if err := clearStartupApproval(); err != nil {
		return fmt.Errorf("开启开机自启失败：%w", err)
	}
	key, _, err := registry.CreateKey(registry.CURRENT_USER, autostartRegistryKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("开启开机自启失败：%w", err)
	}
	defer key.Close()
	if err := key.SetStringValue(autostartValueName, autostartCommand(executable)); err != nil {
		return fmt.Errorf("开启开机自启失败：%w", err)
	}
	return nil
}

func clearStartupApproval() error {
	key, err := registry.OpenKey(registry.CURRENT_USER, startupApprovedRegistryKey, registry.SET_VALUE)
	if errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
		return nil
	}
	if err != nil {
		return err
	}
	defer key.Close()
	if err := key.DeleteValue(autostartValueName); err != nil && !errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
		return err
	}
	return nil
}

func AutostartEnabled() (bool, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, autostartRegistryKey, registry.QUERY_VALUE)
	if errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("读取开机自启状态失败：%w", err)
	}
	value, _, err := key.GetStringValue(autostartValueName)
	key.Close()
	if errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("读取开机自启状态失败：%w", err)
	}
	executable, err := os.Executable()
	if err != nil {
		return false, fmt.Errorf("无法定位 HypoMux 程序：%w", err)
	}
	if !autostartCommandMatches(value, executable) {
		return false, nil
	}

	approvalKey, err := registry.OpenKey(registry.CURRENT_USER, startupApprovedRegistryKey, registry.QUERY_VALUE)
	if errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("读取开机自启状态失败：%w", err)
	}
	approval, _, err := approvalKey.GetBinaryValue(autostartValueName)
	approvalKey.Close()
	if errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("读取开机自启状态失败：%w", err)
	}
	return startupApprovalAllows(approval), nil
}
