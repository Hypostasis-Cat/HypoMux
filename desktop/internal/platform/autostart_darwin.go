//go:build darwin

package platform

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const darwinLaunchAgentName = "io.hypomux.desktop.plist"

func SetAutostart(enabled bool) error {
	path, err := darwinLaunchAgentPath()
	if err != nil {
		return err
	}
	if !enabled {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("关闭开机自启失败：%w", err)
		}
		return nil
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("无法定位 HypoMux 程序：%w", err)
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return fmt.Errorf("无法解析 HypoMux 程序路径：%w", err)
	}
	if strings.Contains(executable, "/AppTranslocation/") {
		return errors.New("请先将 HypoMux.app 移到“应用程序”文件夹，再开启登录时启动")
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("创建 LaunchAgents 目录失败：%w", err)
	}
	plist := launchAgentPlist(executable)
	temporary, err := os.CreateTemp(directory, ".hypomux-autostart-*")
	if err != nil {
		return fmt.Errorf("创建登录项临时文件失败：%w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if chmodErr := temporary.Chmod(0o600); chmodErr != nil {
		_ = temporary.Close()
		return fmt.Errorf("保护登录项文件失败：%w", chmodErr)
	}
	_, err = temporary.WriteString(plist)
	closeErr := temporary.Close()
	if err != nil {
		return fmt.Errorf("写入登录项失败：%w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("提交登录项失败：%w", closeErr)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("安装登录项失败：%w", err)
	}
	return nil
}

func AutostartEnabled() (bool, error) {
	path, err := darwinLaunchAgentPath()
	if err != nil {
		return false, err
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return false, nil
	} else if err != nil {
		return false, fmt.Errorf("读取登录项失败：%w", err)
	}
	output, err := exec.Command("/usr/bin/plutil", "-extract", "ProgramArguments.0", "raw", "-o", "-", path).CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("解析登录项失败：%s", strings.TrimSpace(string(output)))
	}
	executable, err := os.Executable()
	if err != nil {
		return false, fmt.Errorf("无法定位 HypoMux 程序：%w", err)
	}
	executable, _ = filepath.EvalSymlinks(executable)
	configured, _ := filepath.EvalSymlinks(strings.TrimSpace(string(output)))
	return configured == executable, nil
}

func darwinLaunchAgentPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("无法定位用户目录：%w", err)
	}
	return filepath.Join(home, "Library", "LaunchAgents", darwinLaunchAgentName), nil
}

func launchAgentPlist(executable string) string {
	var escaped bytes.Buffer
	_ = xml.EscapeText(&escaped, []byte(executable))
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>io.hypomux.desktop</string>
  <key>ProgramArguments</key>
  <array>
    <string>` + escaped.String() + `</string>
    <string>--silent</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
</dict>
</plist>
`
}
