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

func ShowElevationCompatibilityMessage(proxyCompatible bool, detail string) {
	title, _ := windows.UTF16PtrFromString("HypoMux - 管理员兼容模式")
	messageText := "HypoMux 无法自动切换到普通权限。\n\n"
	if proxyCompatible {
		messageText += "本次将以兼容模式继续：系统代理和 TUN 模式均可使用，高权限网络操作仍由独立聚合核心承接。建议下次使用普通权限启动 HypoMux。"
	} else {
		messageText += "无法确认当前管理员身份与桌面用户一致。TUN 仍可通过独立聚合核心使用；为避免修改错误账户，系统代理会继续执行账户身份保护。"
	}
	if detail != "" {
		messageText += "\n\n诊断信息：" + detail
	}
	message, _ := windows.UTF16PtrFromString(messageText)
	messageBox := windows.NewLazySystemDLL("user32.dll").NewProc("MessageBoxW")
	_, _, _ = messageBox.Call(
		0,
		uintptr(unsafe.Pointer(message)),
		uintptr(unsafe.Pointer(title)),
		0x00000030,
	)
}
