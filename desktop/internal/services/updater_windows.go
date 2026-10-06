//go:build windows

package services

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	desktopplatform "github.com/Hypostasis-Cat/HypoMux/desktop/internal/platform"
	"golang.org/x/sys/windows"
)

// The copied, already signed desktop binary also provides a headless native
// updater. Launching the installed image would keep it locked during upgrade.
func launchInstallerAfterExit(installerPath string, processID int) error {
	absolute, err := validateDownloadedInstallerPath(installerPath)
	if err != nil {
		return err
	}
	if err = verifyDownloadedInstallerAuthenticity(absolute); err != nil {
		return fmt.Errorf("拒绝启动未通过 Authenticode 验证的安装包：%w", err)
	}
	source, err := os.Executable()
	if err != nil {
		return err
	}
	nonce := make([]byte, 16)
	if _, err = rand.Read(nonce); err != nil {
		return err
	}
	token := hex.EncodeToString(nonce)
	helper := filepath.Join(filepath.Dir(absolute), "hypomux-update-helper-"+token+".exe")
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(helper, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	syncErr := out.Sync()
	closeErr := out.Close()
	if err = errors.Join(copyErr, syncErr, closeErr); err != nil {
		return err
	}
	ready := filepath.Join(filepath.Dir(absolute), "ready-"+token)
	cmd := exec.Command(helper, "--run-update", absolute, strconv.Itoa(processID), token)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	if err = cmd.Start(); err != nil {
		return fmt.Errorf("启动更新助手失败：%w", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	// The app stays alive until the helper owns a parent handle and a verified,
	// write/delete-locked installer. Starting a process alone is not readiness.
	timer := time.NewTimer(60 * time.Second)
	defer timer.Stop()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
			if b, e := os.ReadFile(ready); e == nil {
				_ = os.Remove(ready)
				if string(b) == "ready" {
					return nil
				}
				return fmt.Errorf("更新助手准备失败：%s", b)
			}
		case e := <-done:
			return fmt.Errorf("更新助手提前退出：%v；报告：%s", e, filepath.Join(filepath.Dir(absolute), "update-result.log"))
		case <-timer.C:
			_ = cmd.Process.Kill()
			return errors.New("更新助手准备超时，应用未退出；请手动运行已下载的安装包")
		}
	}
}

func RunUpdateHelper(args []string) int {
	if len(args) != 3 {
		return 2
	}
	installer, err := validateDownloadedInstallerPath(args[0])
	if err != nil {
		return 2
	}
	pid, err := strconv.ParseUint(args[1], 10, 32)
	if err != nil || pid == 0 {
		return 2
	}
	token, err := hex.DecodeString(args[2])
	if err != nil || len(token) != 16 {
		return 2
	}
	ready := filepath.Join(filepath.Dir(installer), "ready-"+args[2])
	logPath := filepath.Join(filepath.Dir(installer), "update-result.log")
	logResult := func(message string) {
		f, e := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if e == nil {
			fmt.Fprintf(f, "%s %s\n", time.Now().Format(time.RFC3339), message)
			_ = f.Sync()
			_ = f.Close()
		}
	}
	readySent := false
	fail := func(e error) int {
		logResult("FAILED: " + e.Error())
		if !readySent {
			_ = writeUpdateReady(ready, e.Error())
		} else {
			desktopplatform.ShowErrorMessage("HypoMux 更新未完成", fmt.Sprintf("%v\n\n安装包已保留，可重新运行：\n%s\n\n报告：%s", e, installer, logPath))
		}
		return 1
	}
	parent, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return fail(err)
	}
	defer windows.CloseHandle(parent)
	wide, err := windows.UTF16PtrFromString(installer)
	if err != nil {
		return fail(err)
	}
	locked, err := windows.CreateFile(wide, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return fail(err)
	}
	defer func() {
		if locked != 0 {
			_ = windows.CloseHandle(locked)
		}
	}()
	if err = verifyDownloadedInstallerAuthenticity(installer); err != nil {
		return fail(err)
	}
	if err = writeUpdateReady(ready, "ready"); err != nil {
		return fail(err)
	}
	readySent = true
	logResult("READY; waiting for desktop shutdown")
	err = finishUpdate(func() error {
		status, e := windows.WaitForSingleObject(parent, 180000)
		if e != nil {
			return e
		}
		if status != windows.WAIT_OBJECT_0 {
			return errors.New("等待 HypoMux 退出超时，未启动安装程序")
		}
		return nil
	}, func() (uint32, error) { return executeUpdateInstaller(installer) }, func() error {
		_ = windows.CloseHandle(locked)
		locked = 0
		return os.Remove(installer)
	}, logResult)
	if err != nil {
		return fail(err)
	}
	return 0
}

func writeUpdateReady(path, value string) error {
	if err := os.WriteFile(path+".new", []byte(value), 0600); err != nil {
		return err
	}
	return os.Rename(path+".new", path)
}

func finishUpdate(wait func() error, launch func() (uint32, error), cleanup func() error, record func(string)) error {
	if err := wait(); err != nil {
		return err
	}
	code, err := launch()
	if err != nil {
		return err
	}
	record(fmt.Sprintf("installer exit code: %d", code))
	if code != 0 {
		return fmt.Errorf("安装未完成（退出代码 %d）", code)
	}
	if err = cleanup(); err != nil {
		record("installed; cache cleanup deferred: " + err.Error())
	}
	record("SUCCESS")
	return nil
}

func executeUpdateInstaller(installer string) (uint32, error) {
	verb, _ := windows.UTF16PtrFromString("runas")
	file, e := windows.UTF16PtrFromString(installer)
	if e != nil {
		return 0, e
	}
	dir, e := windows.UTF16PtrFromString(filepath.Dir(installer))
	if e != nil {
		return 0, e
	}
	info := natFirewallShellExecuteInfo{Mask: 0x40 | 0x100, Verb: verb, File: file, Directory: dir, Show: 1}
	info.Size = uint32(unsafe.Sizeof(info))
	result, _, err := windows.NewLazySystemDLL("shell32.dll").NewProc("ShellExecuteExW").Call(uintptr(unsafe.Pointer(&info)))
	if result == 0 {
		return 0, err
	}
	if info.Process == 0 {
		return 0, errors.New("安装程序未返回进程句柄")
	}
	defer windows.CloseHandle(info.Process)
	status, err := windows.WaitForSingleObject(info.Process, windows.INFINITE)
	if err != nil {
		return 0, err
	}
	if status != windows.WAIT_OBJECT_0 {
		return 0, fmt.Errorf("unexpected installer wait status %d", status)
	}
	var code uint32
	err = windows.GetExitCodeProcess(info.Process, &code)
	return code, err
}

func validateDownloadedInstallerPath(installerPath string) (string, error) {
	absolute, err := filepath.Abs(installerPath)
	if err != nil {
		return "", errors.New("下载的安装包路径无效")
	}
	info, err := os.Stat(absolute)
	if err != nil || !info.Mode().IsRegular() || !installerNamePattern.MatchString(filepath.Base(absolute)) {
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
	if filepath.Dir(updateDirectory) != "." || !strings.HasPrefix(filepath.Base(updateDirectory), "HypoMuxUpdate-") {
		return "", errors.New("拒绝启动非 HypoMux 更新目录中的安装包")
	}
	// Native Unicode APIs accept Chinese names and shell metacharacters literally.
	// Reject reparse points, including parent directories, before trusting a path.
	for p := absolute; ; p = filepath.Dir(p) {
		w, e := windows.UTF16PtrFromString(p)
		if e != nil {
			return "", e
		}
		a, e := windows.GetFileAttributes(w)
		if e != nil {
			return "", e
		}
		if a&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return "", errors.New("更新路径不能包含链接或联接目录")
		}
		if filepath.Dir(p) == p {
			break
		}
	}
	return absolute, nil
}
