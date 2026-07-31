//go:build windows

package platform

import (
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const webView2ClientID = `{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}`

func WebView2Available() bool {
	checks := []struct {
		root registry.Key
		path string
	}{
		{registry.LOCAL_MACHINE, `SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\` + webView2ClientID},
		{registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\EdgeUpdate\Clients\` + webView2ClientID},
		{registry.CURRENT_USER, `Software\Microsoft\EdgeUpdate\Clients\` + webView2ClientID},
	}
	for _, check := range checks {
		key, err := registry.OpenKey(check.root, check.path, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		version, _, readErr := key.GetStringValue("pv")
		_ = key.Close()
		if readErr == nil && version != "" {
			return true
		}
	}
	return false
}

func ShowWebView2MissingMessage() {
	title, _ := windows.UTF16PtrFromString("HypoMux - 缺少 WebView2 Runtime")
	message, _ := windows.UTF16PtrFromString(
		"未检测到 Microsoft Edge WebView2 Runtime，HypoMux 无法显示桌面界面。\n\n" +
			"请重新运行 HypoMux 安装程序；安装程序会自动安装 WebView2 Runtime。安装完成后再启动 HypoMux。",
	)
	user32 := windows.NewLazySystemDLL("user32.dll")
	messageBox := user32.NewProc("MessageBoxW")
	_, _, _ = messageBox.Call(
		0,
		uintptr(unsafe.Pointer(message)),
		uintptr(unsafe.Pointer(title)),
		0x00000010,
	)
}
