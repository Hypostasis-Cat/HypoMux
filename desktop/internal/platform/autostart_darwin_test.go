//go:build darwin

package platform

import (
	"strings"
	"testing"
)

func TestLaunchAgentPlistEscapesExecutable(t *testing.T) {
	plist := launchAgentPlist(`/Applications/HypoMux & Friends.app/Contents/MacOS/hypomux`)
	if !strings.Contains(plist, "HypoMux &amp; Friends.app") || !strings.Contains(plist, "<string>--silent</string>") {
		t.Fatalf("unexpected plist: %s", plist)
	}
}
