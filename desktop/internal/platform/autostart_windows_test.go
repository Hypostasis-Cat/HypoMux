//go:build windows

package platform

import "testing"

func TestAutostartCommandQuotesCustomInstallPath(t *testing.T) {
	executable := `D:\My Apps\HypoMux 测试\hypomux.exe`
	want := `"D:\My Apps\HypoMux 测试\hypomux.exe" --silent`
	if got := autostartCommand(executable); got != want {
		t.Fatalf("autostart command = %q, want %q", got, want)
	}
}

func TestAutostartCommandRejectsStaleInstallPath(t *testing.T) {
	current := `D:\Current\hypomux.exe`
	if autostartCommandMatches(`"C:\Old\hypomux.exe" --silent`, current) {
		t.Fatal("a Run entry pointing at a removed installation was reported enabled")
	}
	if !autostartCommandMatches(`"d:\current\HYPOMUX.EXE" --silent`, current) {
		t.Fatal("Windows path casing should not change autostart state")
	}
}

func TestStartupApprovalDisabledState(t *testing.T) {
	if startupApprovalAllows([]byte{3, 0, 0, 0}) {
		t.Fatal("Task Manager disabled entry was reported enabled")
	}
	if !startupApprovalAllows([]byte{2, 0, 0, 0}) {
		t.Fatal("Task Manager enabled entry was reported disabled")
	}
	if !startupApprovalAllows(nil) {
		t.Fatal("missing Task Manager approval should default to enabled")
	}
}
