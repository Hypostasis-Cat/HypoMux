//go:build windows

package services

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func launchInstallerAfterExit(installerPath string, processID int) error {
	absolute, err := validateDownloadedInstallerPath(installerPath)
	if err != nil {
		return err
	}
	// Revalidate immediately before creating the launcher so a file replaced
	// after Download returned can never cross the execution boundary.
	if err := verifyDownloadedInstallerAuthenticity(absolute); err != nil {
		return fmt.Errorf("拒绝启动未通过 Authenticode 验证的安装包：%w", err)
	}
	script := filepath.Join(filepath.Dir(absolute), "run-update.cmd")
	content := "@echo off\r\n" +
		"set \"target_pid=" + strconv.Itoa(processID) + "\"\r\n" +
		":wait_for_hypomux\r\n" +
		"tasklist /FI \"PID eq %target_pid%\" /NH | find \"%target_pid%\" >nul\r\n" +
		"if not errorlevel 1 (\r\n" +
		"  timeout /t 1 /nobreak >nul\r\n" +
		"  goto wait_for_hypomux\r\n" +
		")\r\n" +
		"start \"\" /wait \"" + absolute + "\"\r\n" +
		"del /q \"" + absolute + "\"\r\n" +
		"set \"update_dir=%~dp0\"\r\n" +
		"del /q \"%~f0\"\r\n" +
		"cd /d \"%TEMP%\"\r\n" +
		"rd \"%update_dir%\" >nul 2>&1\r\n"
	if err := os.WriteFile(script, []byte(content), 0o600); err != nil {
		return fmt.Errorf("创建更新启动器失败：%w", err)
	}
	commandShell := os.Getenv("COMSPEC")
	if commandShell == "" {
		commandShell = `C:\Windows\System32\cmd.exe`
	}
	command := exec.Command(commandShell, "/d", "/c", script)
	command.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
	if err := command.Start(); err != nil {
		return fmt.Errorf("启动安装程序失败：%w", err)
	}
	return command.Process.Release()
}

func validateDownloadedInstallerPath(installerPath string) (string, error) {
	absolute, err := filepath.Abs(installerPath)
	if err != nil {
		return "", errors.New("下载的安装包路径无效")
	}
	// 路径会被写入批处理脚本执行，拒绝可能被 cmd.exe 解释的元字符。
	if strings.ContainsAny(absolute, "%&|<>^\"") {
		return "", errors.New("下载的安装包路径包含不安全字符")
	}
	info, err := os.Stat(absolute)
	if err != nil || info.IsDir() || !installerNamePattern.MatchString(filepath.Base(absolute)) {
		return "", errors.New("下载的安装包不存在或名称无效")
	}
	tempRoot, err := filepath.Abs(os.TempDir())
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(tempRoot, absolute)
	if err != nil || strings.HasPrefix(relative, "..") || filepath.IsAbs(relative) {
		return "", errors.New("拒绝启动临时更新目录之外的安装包")
	}
	updateDirectory := filepath.Dir(relative)
	if filepath.Dir(updateDirectory) != "." ||
		!strings.HasPrefix(filepath.Base(updateDirectory), "HypoMuxUpdate-") {
		return "", errors.New("拒绝启动非 HypoMux 更新目录中的安装包")
	}
	return absolute, nil
}
