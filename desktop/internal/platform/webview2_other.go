//go:build !windows

package platform

import (
	"fmt"
	"os"
)

func WebView2Available() bool {
	return true
}

func ShowWebView2MissingMessage() {}

func ShowErrorMessage(title string, message string) {
	_, _ = fmt.Fprintf(os.Stderr, "%s: %s\n", title, message)
}
