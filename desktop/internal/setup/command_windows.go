//go:build windows

package setup

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

func hideCommand(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
}

// Run is called before desktop/WebView initialization. No public API exposes
// arbitrary transaction roots: machine journals always use protected ProgramData.
func Run(args []string, out io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(out, "missing setup operation")
		return 2
	}
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.SetOutput(out)
	scope := fs.String("scope", "machine", "machine or user")
	install := fs.String("install-dir", "", "desktop destination")
	payload := fs.String("payload", "", "staged installer payload")
	previous := fs.String("previous-dir", "", "previous desktop directory")
	if e := fs.Parse(args[1:]); e != nil {
		return 2
	}
	if *scope != "machine" && *scope != "user" {
		fmt.Fprintln(out, "invalid install scope")
		return 2
	}
	machine := *scope == "machine"
	if machine && !windows.GetCurrentProcessToken().IsElevated() {
		fmt.Fprintln(out, "administrator privileges required")
		return 2
	}
	folder := windows.FOLDERID_LocalAppData
	if machine {
		folder = windows.FOLDERID_ProgramData
	}
	base, e := windows.KnownFolderPath(folder, 0)
	if e != nil {
		fmt.Fprintln(out, e)
		return 1
	}
	appRoot := filepath.Join(base, "HypoMux")
	for _, d := range []string{appRoot, filepath.Join(appRoot, "SetupTransaction")} {
		if e = secureDirectory(d, machine, machine && d == appRoot); e != nil {
			fmt.Fprintln(out, e)
			return 1
		}
	}
	root := filepath.Join(appRoot, "SetupTransaction")
	mutexName := `Local\HypoMux-Setup-Transaction`
	if machine {
		mutexName = `Global\HypoMux-Setup-Transaction`
	}
	name, _ := windows.UTF16PtrFromString(mutexName)
	// Win32 mutex ownership belongs to an OS thread, not a Go goroutine.
	// Keep that thread alive and pinned until ReleaseMutex has executed.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	mutex, e := windows.CreateMutex(nil, false, name)
	if e != nil && !errors.Is(e, windows.ERROR_ALREADY_EXISTS) {
		fmt.Fprintln(out, e)
		return 1
	}
	defer windows.CloseHandle(mutex)
	result, e := windows.WaitForSingleObject(mutex, 0)
	if e != nil || (result != windows.WAIT_OBJECT_0 && result != windows.WAIT_ABANDONED) {
		fmt.Fprintln(out, "another setup transaction is active")
		return 1
	}
	defer windows.ReleaseMutex(mutex)
	platform := windowsPlatform{machine: machine, coreBin: filepath.Join(appRoot, "Core", "bin")}
	t := Transaction{Root: root, Platform: platform}
	switch args[0] {
	case "recover":
		e = t.Recover()
	case "begin":
		var items []Item
		items, e = makePlan(*install, *payload, platform)
		if e == nil {
			e = t.Begin(items)
		}
	case "apply":
		e = t.Apply()
	case "commit":
		e = t.Commit()
	case "rollback":
		e = t.Rollback()
	case "cleanup-previous":
		var j *Journal
		j, e = t.load()
		if e == nil && j.Phase != "committed" {
			e = errors.New("cleanup requires a committed installation")
		}
		if e == nil {
			e = cleanupPrevious(*previous, *install, platform)
		}
	default:
		e = errors.New("unknown setup operation")
	}
	if e != nil {
		t.log(args[0] + " FAILED: " + e.Error())
		fmt.Fprintf(out, "%s failed: %v\nRecovery and report: %s\n", args[0], e, root)
		return 1
	}
	fmt.Fprintf(out, "%s complete. Report: %s\n", args[0], filepath.Join(root, "setup.log"))
	return 0
}

// Cleanup is post-commit and exact-file-only. Validate the whole old layout
// before deleting anything; aliases and links must never reach the new Core.
func cleanupPrevious(previous, current string, p windowsPlatform) error {
	if err := noLinks(previous); err != nil {
		return err
	}
	if err := noLinks(current); err != nil {
		return err
	}
	oldInfo, err := os.Stat(previous)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	newInfo, err := os.Stat(current)
	if err != nil {
		return err
	}
	if os.SameFile(oldInfo, newInfo) {
		return nil
	}
	bin := filepath.Join(previous, "bin")
	if oldBin, e := os.Stat(bin); e == nil {
		if coreBin, e := os.Stat(p.coreBin); e == nil && os.SameFile(oldBin, coreBin) {
			return errors.New("refusing to clean protected Core as an old desktop")
		}
	}
	marker := filepath.Join(bin, "hypomux-engine.exe")
	if _, err = os.Stat(marker); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	names := []string{"hypomux.exe", "uninstall.exe", filepath.Join("bin", "hypomux-engine.exe"), filepath.Join("bin", "sing-box.exe"), filepath.Join("bin", "wintun.dll"), filepath.Join("bin", "libcronet.dll")}
	for _, name := range names {
		path := filepath.Join(previous, name)
		if err = p.ValidateTarget(path); err != nil {
			return err
		}
		if old, e := os.Stat(path); e == nil {
			if now, e := os.Stat(filepath.Join(current, name)); e == nil && os.SameFile(old, now) {
				return errors.New("old file aliases current installation")
			}
		}
	}
	for _, name := range names {
		path := filepath.Join(previous, name)
		if _, err = os.Stat(path); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		if err = p.PrepareTarget(path); err != nil {
			return err
		}
		if err = os.Remove(path); err != nil {
			return err
		}
	}
	_ = os.Remove(bin)
	_ = os.Remove(previous) // Empty directories only.
	return nil
}
func makePlan(install, payload string, p windowsPlatform) ([]Item, error) {
	if e := noLinks(install); e != nil {
		return nil, e
	}
	if e := noLinks(payload); e != nil {
		return nil, e
	}
	install = filepath.Clean(install)
	if filepath.Dir(install) == install {
		return nil, errors.New("cannot install in drive root")
	}
	items := []Item{{filepath.Join(install, "hypomux.exe"), filepath.Join(payload, "hypomux.exe")}, {filepath.Join(install, "uninstall.exe"), ""}}
	if info, e := os.Stat(items[0].Source); e != nil || info.Size() == 0 {
		return nil, errors.New("desktop payload missing or empty")
	}
	for _, name := range []string{"hypomux-engine.exe", "sing-box.exe", "wintun.dll", "libcronet.dll"} {
		source := filepath.Join(payload, name)
		if info, e := os.Stat(source); e != nil || info.Size() == 0 {
			return nil, fmt.Errorf("required payload missing or empty: %s", name)
		}
		items = append(items, Item{filepath.Join(install, "bin", name), source})
		if p.machine {
			if strings.EqualFold(filepath.Join(install, "bin"), p.coreBin) {
				return nil, errors.New("desktop cannot use the protected Core directory")
			}
			items = append(items, Item{filepath.Join(p.coreBin, name), source})
		}
	}
	folders := []*windows.KNOWNFOLDERID{windows.FOLDERID_Desktop, windows.FOLDERID_Programs}
	if p.machine {
		folders = []*windows.KNOWNFOLDERID{windows.FOLDERID_PublicDesktop, windows.FOLDERID_CommonPrograms}
	}
	for _, id := range folders {
		dir, e := windows.KnownFolderPath(id, 0)
		if e != nil {
			return nil, e
		}
		items = append(items, Item{filepath.Join(dir, "HypoMux.lnk"), ""})
	}
	return items, nil
}
