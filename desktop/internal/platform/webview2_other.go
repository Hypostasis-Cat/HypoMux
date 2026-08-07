//go:build !windows

package platform

func WebView2Available() bool {
	return true
}

func ShowWebView2MissingMessage() {}
