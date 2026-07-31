//go:build windows

package platform

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

const autostartRegistryKey = `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`

func hiddenCommand(name string, arguments ...string) *exec.Cmd {
	command := exec.Command(name, arguments...)
	command.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000,
	}
	return command
}

func SetAutostart(enabled bool) error {
	if !enabled {
		command := hiddenCommand("reg.exe", "DELETE", autostartRegistryKey, "/v", "HypoMux", "/f")
		output, err := command.CombinedOutput()
		if err != nil && !strings.Contains(strings.ToLower(string(output)), "unable to find") &&
			!strings.Contains(string(output), "找不到") {
			return fmt.Errorf("关闭开机自启失败：%s", strings.TrimSpace(string(output)))
		}
		return nil
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("无法定位 HypoMux 程序：%w", err)
	}
	runCommand := fmt.Sprintf(`"%s" --silent`, executable)
	output, err := hiddenCommand(
		"reg.exe", "ADD", autostartRegistryKey,
		"/v", "HypoMux", "/t", "REG_SZ", "/d", runCommand, "/f",
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("开启开机自启失败：%s", strings.TrimSpace(string(output)))
	}
	return nil
}

func AutostartEnabled() (bool, error) {
	output, err := hiddenCommand("reg.exe", "QUERY", autostartRegistryKey, "/v", "HypoMux").CombinedOutput()
	if err != nil {
		text := strings.ToLower(string(output))
		if strings.Contains(text, "unable to find") || strings.Contains(string(output), "找不到") {
			return false, nil
		}
		return false, fmt.Errorf("读取开机自启状态失败：%s", strings.TrimSpace(string(output)))
	}
	return strings.Contains(string(output), "HypoMux"), nil
}
