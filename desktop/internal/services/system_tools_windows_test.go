//go:build windows

package services

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsSystemToolsDoNotDependOnPATH(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	for _, resolve := range []struct {
		name string
		call func() (string, error)
	}{
		{name: "powershell.exe", call: resolveWindowsPowerShellExecutable},
		{name: "curl.exe", call: func() (string, error) { return resolveWindowsSystemExecutable("curl.exe") }},
	} {
		path, err := resolve.call()
		if err != nil {
			t.Fatalf("resolve %s: %v", resolve.name, err)
		}
		if !filepath.IsAbs(path) || !strings.EqualFold(filepath.Base(path), resolve.name) {
			t.Fatalf("resolved %s path = %q", resolve.name, path)
		}
	}
}
