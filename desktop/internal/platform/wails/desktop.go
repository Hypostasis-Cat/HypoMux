package wails

import (
	"fmt"
	"os/exec"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/Hypostasis-Cat/HypoMux/desktop/internal/platform"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

var _ platform.DesktopHost = (*DesktopHost)(nil)

// DesktopHost contains every direct Wails desktop dependency used by HypoMux.
// Future services should depend on platform.DesktopHost instead of Wails types.
type DesktopHost struct {
	app            *application.App
	window         application.Window
	trayMenuWindow application.Window
	tray           *application.SystemTray
	onQuit         func()
	closeToTray    func() bool
	startSilent    bool
	startupShown   atomic.Bool
	quitting       atomic.Bool
	cleanupOnce    sync.Once
}

func NewDesktopHost(app *application.App, window application.Window, trayMenuWindow application.Window, startSilent bool, onQuit func(), closeToTray func() bool) *DesktopHost {
	return &DesktopHost{app: app, window: window, trayMenuWindow: trayMenuWindow, startSilent: startSilent, onQuit: onQuit, closeToTray: closeToTray}
}

func (d *DesktopHost) ConfigureTray(icon []byte) {
	d.tray = d.app.SystemTray.New()
	d.tray.SetIcon(icon)
	d.tray.SetTooltip("HypoMux · 聚合引擎未启动")

	// Keep the tray flyout alive between clicks. Wails owns its positioning,
	// focus-loss hiding and Windows notification-area click debounce.
	if d.trayMenuWindow != nil {
		d.trayMenuWindow.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
			d.trayMenuWindow.Hide()
			event.Cancel()
		})
		d.tray.AttachWindow(d.trayMenuWindow).WindowOffset(8)
	}

	d.tray.OnClick(func() {
		d.tray.ToggleWindow()
	})
	d.tray.OnRightClick(func() {
		d.tray.ToggleWindow()
	})
}

func (d *DesktopHost) SetEngineTrayStatus(phase string, mode string) {
	state := "未启动"
	switch phase {
	case "running":
		state = "运行中"
	case "degraded":
		state = "降级运行"
	case "starting":
		state = "正在启动"
	case "stopping":
		state = "正在停止"
	case "failed":
		state = "异常"
	}
	modeName := "系统代理"
	if mode == "tun" {
		modeName = "虚拟网卡"
	}
	label := fmt.Sprintf("聚合引擎：%s · %s", state, modeName)
	if d.tray != nil {
		d.tray.SetTooltip("HypoMux · " + label)
	}
	d.app.Event.Emit("engine:status", map[string]string{
		"phase": phase,
		"mode":  mode,
	})
}

func (d *DesktopHost) ConfigureCloseToTray() {
	d.window.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		if d.quitting.Load() {
			return
		}
		if d.closeToTray == nil || d.closeToTray() {
			d.HideToTray()
			event.Cancel()
			return
		}
		// A system tray keeps the Wails application alive after its last
		// window closes. Cancel the window-only close and explicitly quit the
		// application so the user's "direct exit" choice is authoritative.
		event.Cancel()
		d.window.Hide()
		go d.Quit()
	})
}

func (d *DesktopHost) Minimise() {
	d.window.Minimise()
}

func (d *DesktopHost) ToggleMaximise() {
	d.window.ToggleMaximise()
}

func (d *DesktopHost) HideToTray() {
	d.window.Hide()
}

func (d *DesktopHost) Show() {
	d.startupShown.Store(true)
	d.window.Show().Focus()
}

func (d *DesktopHost) ShowStartup() {
	if d.startSilent || !d.startupShown.CompareAndSwap(false, true) {
		return
	}
	d.window.Show().Focus()
}

func (d *DesktopHost) Quit() {
	if !d.quitting.CompareAndSwap(false, true) {
		return
	}
	d.cleanupOnce.Do(func() {
		if d.onQuit != nil {
			d.onQuit()
		}
	})
	d.app.Quit()
}

func (d *DesktopHost) OpenJSONFile(title string) (string, error) {
	return d.app.Dialog.OpenFile().
		AttachToWindow(d.window).
		SetTitle(title).
		SetButtonText("导入").
		AddFilter("JSON 文件 (*.json)", "*.json").
		PromptForSingleSelection()
}

func (d *DesktopHost) SaveJSONFile(title string, filename string) (string, error) {
	return d.app.Dialog.SaveFileWithOptions(&application.SaveFileDialogOptions{
		Title:      title,
		Filename:   filename,
		ButtonText: "导出",
		Window:     d.window,
		Filters: []application.FileFilter{
			{DisplayName: "JSON 文件 (*.json)", Pattern: "*.json"},
		},
		CanCreateDirectories: true,
	}).PromptForSingleSelection()
}

func (d *DesktopHost) SaveTextFile(title string, filename string) (string, error) {
	return d.app.Dialog.SaveFileWithOptions(&application.SaveFileDialogOptions{
		Title:      title,
		Filename:   filename,
		ButtonText: "导出",
		Window:     d.window,
		Filters: []application.FileFilter{
			{DisplayName: "日志文件 (*.log)", Pattern: "*.log"},
			{DisplayName: "文本文件 (*.txt)", Pattern: "*.txt"},
		},
		CanCreateDirectories: true,
	}).PromptForSingleSelection()
}

func (d *DesktopHost) OpenDirectory(path string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		command = exec.Command("explorer.exe", path)
	case "darwin":
		command = exec.Command("open", path)
	default:
		command = exec.Command("xdg-open", path)
	}
	if err := command.Start(); err != nil {
		return fmt.Errorf("打开目录失败：%w", err)
	}
	return nil
}
