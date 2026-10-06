package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
	"unicode/utf16"
)

func installerFunction(t *testing.T, source, name string) string {
	t.Helper()
	start := strings.Index(source, "Function "+name+"\n")
	if start < 0 {
		t.Fatalf("missing installer function %s", name)
	}
	end := strings.Index(source[start:], "FunctionEnd")
	if end < 0 {
		t.Fatalf("unterminated installer function %s", name)
	}
	return source[start : start+end+len("FunctionEnd")]
}

func TestInstallerCoreWriteProbeWindows(t *testing.T) {
	script, err := filepath.Abs("build/windows/nsis/stop-core-for-upgrade.ps1")
	if err != nil {
		t.Fatal(err)
	}
	host := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	for _, name := range []string{"absent", "writable", "readonly", "shared-reader", "write-locked"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "hypomux-engine.exe")
			const contents = "diagnostic fixture, not executable"
			if name != "absent" {
				if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
					t.Fatal(err)
				}
			}
			wide, err := syscall.UTF16PtrFromString(path)
			if err != nil {
				t.Fatal(err)
			}
			if name == "readonly" {
				if err := syscall.SetFileAttributes(wide, syscall.FILE_ATTRIBUTE_READONLY|syscall.FILE_ATTRIBUTE_HIDDEN); err != nil {
					t.Fatal(err)
				}
				defer syscall.SetFileAttributes(wide, syscall.FILE_ATTRIBUTE_NORMAL)
			}
			if name == "shared-reader" || name == "write-locked" {
				share := uint32(syscall.FILE_SHARE_READ)
				if name == "shared-reader" {
					share |= syscall.FILE_SHARE_WRITE | syscall.FILE_SHARE_DELETE
				}
				handle, err := syscall.CreateFile(wide, syscall.GENERIC_READ, share, nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
				if err != nil {
					t.Fatal(err)
				}
				defer syscall.CloseHandle(handle)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, host, "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", script, "-EnginePath", path, "-TimeoutSeconds", "1")
			for _, entry := range os.Environ() {
				if !strings.HasPrefix(strings.ToUpper(entry), "PSMODULEPATH=") {
					cmd.Env = append(cmd.Env, entry)
				}
			}
			out, err := cmd.CombinedOutput()
			if name == "write-locked" {
				exit, ok := err.(*exec.ExitError)
				if !ok || exit.ExitCode() != 11 || !strings.Contains(string(out), "HRESULT=0x80070020") {
					t.Fatalf("real write lock must fail with diagnostic error 32, got %v\n%s", err, out)
				}
			} else if err != nil {
				t.Fatalf("probe failed: %v\n%s", err, out)
			}
			if name != "absent" {
				b, err := os.ReadFile(path)
				if err != nil || string(b) != contents {
					t.Fatalf("probe changed file contents: %q, %v", b, err)
				}
			} else if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatal("probe created a missing Core file")
			}
			if name == "readonly" {
				attributes, err := syscall.GetFileAttributes(wide)
				if err != nil || attributes&syscall.FILE_ATTRIBUTE_READONLY != 0 || attributes&syscall.FILE_ATTRIBUTE_HIDDEN == 0 {
					t.Fatalf("must clear only ReadOnly, attributes=%x, error=%v", attributes, err)
				}
			}
		})
	}
}

func readInstallerUTF16(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
	}
	return string(utf16.Decode(u))
}

// Run the production NSIS control flow with a harmless child fixture. This
// verifies fresh installs never launch the helper and upgrades still stop on
// failures, including failures with no output. No real installation is touched.
func TestInstallerCoreUpgradeNSIS(t *testing.T) {
	compiler := os.Getenv("MAKENSIS")
	if compiler == "" {
		var err error
		compiler, err = exec.LookPath("makensis")
		if err != nil {
			t.Skip("set MAKENSIS to run NSIS integration tests")
		}
	}
	b, err := os.ReadFile("build/windows/nsis/project.nsi")
	if err != nil {
		t.Fatal(err)
	}
	script := strings.ReplaceAll(string(b), "\r\n", "\n")
	var functions strings.Builder
	functions.WriteString("Function RollbackSetupTransaction\nFunctionEnd\n")
	for _, name := range []string{"StopCoreProcessesForUpgrade", "CheckExistingCoreForUpgrade", "LogCoreUpgradeCheckFailure"} {
		functions.WriteString(installerFunction(t, script, name) + "\n")
	}
	var language strings.Builder
	for _, line := range strings.Split(script, "\n") {
		if strings.HasPrefix(line, "LangString Core") && strings.Contains(line, "${LANG_ENGLISH}") {
			// Include only the strings used by the three functions under test.
			for _, name := range []string{"CoreProcessStopping", "CoreProcessStopFailed", "CoreCheckDetails", "CoreCheckLogUnavailable"} {
				if strings.HasPrefix(line, "LangString "+name+" ") {
					language.WriteString(line + "\n")
				}
			}
		}
	}
	for _, tc := range []struct {
		name                    string
		previous, current, core bool
		changed                 bool
		helperExit              int
	}{
		{name: "fresh-missing-files"},
		{name: "removed-previous-directory", changed: true},
		{name: "current-upgrade", current: true},
		{name: "moved-upgrade", previous: true, changed: true},
		{name: "protected-service-only", core: true},
		{name: "all-copies", previous: true, current: true, core: true, changed: true},
		{name: "silent-child-failure", current: true, helperExit: 42},
		{name: "previous-child-failure", previous: true, changed: true, helperExit: 10},
		{name: "protected-child-failure", core: true, helperExit: 11},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			paths := []string{filepath.Join(dir, "previous", "bin", "hypomux-engine.exe"), filepath.Join(dir, "new 中文", "bin", "hypomux-engine.exe"), filepath.Join(dir, "protected", "hypomux-engine.exe")}
			var expected []string
			for i, exists := range []bool{tc.previous, tc.current, tc.core} {
				if !exists {
					continue
				}
				if err := os.MkdirAll(filepath.Dir(paths[i]), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(paths[i], []byte("fixture"), 0600); err != nil {
					t.Fatal(err)
				}
				expected = append(expected, paths[i])
			}
			calls := filepath.Join(dir, "calls.txt")
			helper := fmt.Sprintf("param([string]$EnginePath)\r\n[IO.File]::AppendAllText('%s', $EnginePath + [Environment]::NewLine)\r\nexit %d\r\n", strings.ReplaceAll(calls, "'", "''"), tc.helperExit)
			if err := os.WriteFile(filepath.Join(dir, "stop-core-for-upgrade.ps1"), append([]byte{0xef, 0xbb, 0xbf}, []byte(helper)...), 0600); err != nil {
				t.Fatal(err)
			}
			changed := "0"
			if tc.changed {
				changed = "1"
			}
			source := fmt.Sprintf(`Unicode true
!include "LogicLib.nsh"
!define LANG_ENGLISH 1033
!define INFO_PRODUCTVERSION "test"
!define WAILS_INSTALL_SCOPE "machine"
!define HYPOMUX_PROTECTED_CORE_BIN "%s"
Name "HypoMux Core regression"
OutFile "%s"
RequestExecutionLevel user
SilentInstall silent
Var HypoMuxPreviousInstallDir
Var HypoMuxInstallPathChanged
Var HypoMuxCoreCheckTarget
Var HypoMuxCoreCheckLog
%s
%s
Function .onInit
    StrCpy $INSTDIR "%s"
    StrCpy $HypoMuxPreviousInstallDir "%s"
    StrCpy $HypoMuxInstallPathChanged "%s"
    System::Call 'kernel32::SetEnvironmentVariable(t "PSModulePath", p 0)'
FunctionEnd
Function .onInstFailed
    FileOpen $2 "$EXEDIR\failure-report-path.txt" w
    FileWriteUTF16LE $2 "$HypoMuxCoreCheckLog"
    FileClose $2
FunctionEnd
Section
    Call StopCoreProcessesForUpgrade
    FileOpen $2 "$EXEDIR\passed.txt" w
    FileWrite $2 "passed"
    FileClose $2
SectionEnd
`, filepath.Dir(paths[2]), filepath.Join(dir, "probe.exe"), language.String(), functions.String(), filepath.Dir(filepath.Dir(paths[1])), filepath.Dir(filepath.Dir(paths[0])), changed)
			nsi := filepath.Join(dir, "probe.nsi")
			if err := os.WriteFile(nsi, append([]byte{0xef, 0xbb, 0xbf}, []byte(source)...), 0600); err != nil {
				t.Fatal(err)
			}
			if out, err := exec.Command(compiler, "/V2", nsi).CombinedOutput(); err != nil {
				t.Fatalf("compile: %v\n%s", err, out)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			out, runErr := exec.CommandContext(ctx, filepath.Join(dir, "probe.exe"), "/S").CombinedOutput()
			if ctx.Err() != nil {
				t.Fatalf("installer did not finish: %v", ctx.Err())
			}
			if tc.helperExit == 0 {
				if runErr != nil {
					t.Fatalf("run: %v\n%s", runErr, out)
				}
				if _, err := os.Stat(filepath.Join(dir, "passed.txt")); err != nil {
					t.Fatal("successful check did not continue installation")
				}
			} else {
				exit, ok := runErr.(*exec.ExitError)
				if !ok || exit.ExitCode() != 67 {
					t.Fatalf("failed helper must abort with 67, got %v\n%s", runErr, out)
				}
				if _, err := os.Stat(filepath.Join(dir, "passed.txt")); !os.IsNotExist(err) {
					t.Fatal("failed check continued installation")
				}
				logPath := readInstallerUTF16(t, filepath.Join(dir, "failure-report-path.txt"))
				log := readInstallerUTF16(t, logPath)
				defer os.Remove(logPath)
				for _, required := range []string{fmt.Sprintf("Result: %d", tc.helperExit), "Target: " + expected[0], "PowerShell:", "Helper:"} {
					if !strings.Contains(log, required) {
						t.Fatalf("failure report missing %q: %s", required, log)
					}
				}
				expected = expected[:1]
			}
			actual, err := os.ReadFile(calls)
			if len(expected) == 0 {
				if !os.IsNotExist(err) {
					t.Fatal("fresh install launched the upgrade helper")
				}
			} else if err != nil || strings.TrimSpace(string(actual)) != strings.Join(expected, "\r\n") {
				t.Fatalf("helper paths: %q, want %v (error: %v)", actual, expected, err)
			}
		})
	}
}

func TestInstallerLegacyMigrationNeverRunsUninstaller(t *testing.T) {
	b, err := os.ReadFile("build/windows/nsis/project.nsi")
	if err != nil {
		t.Fatal(err)
	}
	function := installerFunction(t, strings.ReplaceAll(string(b), "\r\n", "\n"), "RemoveLegacyInstallations")
	if strings.Contains(function, "ExecWait") || strings.Contains(function, "RMDir") {
		t.Fatal("legacy migration may destroy the new payload")
	}
}

func TestInstallerFreshUICheckNSIS(t *testing.T) {
	compiler := os.Getenv("MAKENSIS")
	if compiler == "" {
		t.Skip("set MAKENSIS to run NSIS integration tests")
	}
	b, err := os.ReadFile("build/windows/nsis/project.nsi")
	if err != nil {
		t.Fatal(err)
	}
	function := installerFunction(t, strings.ReplaceAll(string(b), "\r\n", "\n"), "CloseRunningHypoMux")
	// Replace only the subprocess boundary, leaving production NSIS branching
	// intact. Any attempt to close a UI fails, even with an empty error message.
	lines := strings.Split(function, "\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "nsExec::ExecToStack ") {
			lines[i] = "Push \"\"\nPush 42"
		}
	}
	function = strings.Join(lines, "\n") + "\nFunction RollbackSetupTransaction\nFunctionEnd\n"
	for _, existing := range []bool{false, true} {
		t.Run(fmt.Sprintf("existing=%t", existing), func(t *testing.T) {
			dir := t.TempDir()
			if existing {
				if err := os.WriteFile(filepath.Join(dir, "hypomux-regression-ui-fixture.exe"), []byte("fixture"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			source := fmt.Sprintf(`Unicode true
!include "LogicLib.nsh"
!define PRODUCT_EXECUTABLE "hypomux-regression-ui-fixture.exe"
Name "Fresh UI check regression"
OutFile "%s"
RequestExecutionLevel user
SilentInstall silent
Var HypoMuxPreviousInstallDir
LangString RunningAppClosing 1033 "Checking old UI"
LangString RunningAppCloseFailed 1033 "UI check failed"
%s
Function .onInit
    StrCpy $INSTDIR "%s"
FunctionEnd
Section
    Call CloseRunningHypoMux
SectionEnd
`, filepath.Join(dir, "probe.exe"), function, dir)
			nsi := filepath.Join(dir, "probe.nsi")
			if err := os.WriteFile(nsi, []byte(source), 0600); err != nil {
				t.Fatal(err)
			}
			if out, err := exec.Command(compiler, "/V2", nsi).CombinedOutput(); err != nil {
				t.Fatalf("compile: %v\n%s", err, out)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			out, err := exec.CommandContext(ctx, filepath.Join(dir, "probe.exe"), "/S").CombinedOutput()
			if existing {
				exit, ok := err.(*exec.ExitError)
				if !ok || exit.ExitCode() != 68 {
					t.Fatalf("existing UI check must fail without an interactive prompt: %v\n%s", err, out)
				}
			} else if err != nil {
				t.Fatalf("fresh install must not depend on UI closing: %v\n%s", err, out)
			}
		})
	}
}

func TestInstallerWebViewFailureCannotReachPayloadNSIS(t *testing.T) {
	compiler := os.Getenv("MAKENSIS")
	if compiler == "" {
		t.Skip("set MAKENSIS")
	}
	b, e := os.ReadFile("build/windows/nsis/project.nsi")
	if e != nil {
		t.Fatal(e)
	}
	original := installerFunction(t, strings.ReplaceAll(string(b), "\r\n", "\n"), "EnsureWebViewRuntime")
	for _, code := range []int{0, 67} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			dir := t.TempDir()
			child := filepath.Join(dir, "bootstrap.exe")
			parent := filepath.Join(dir, "parent.exe")
			marker := filepath.Join(dir, "payload-touched")
			function := strings.ReplaceAll(strings.ReplaceAll(original, "Call LogSetupFailure", "DetailPrint \"runtime failed\""), `File "MicrosoftEdgeWebview2Setup.exe"`, `File /oname=MicrosoftEdgeWebview2Setup.exe "`+child+`"`)
			lines := strings.Split(function, "\n")
			for i, line := range lines {
				if strings.HasPrefix(strings.TrimSpace(line), "nsExec::ExecToStack ") {
					lines[i] = "Push \"\"\nPush 1"
				}
			}
			function = strings.Join(lines, "\n")
			sources := []string{fmt.Sprintf("Unicode true\nName bootstrap\nOutFile \"%s\"\nRequestExecutionLevel user\nSilentInstall silent\nSection\nSetErrorLevel %d\nSectionEnd\n", child, code), fmt.Sprintf(`Unicode true
!include "LogicLib.nsh"
!include "MUI.nsh"
!insertmacro MUI_LANGUAGE "English"
Name "WebView gate fixture"
OutFile "%s"
RequestExecutionLevel user
SilentInstall silent
Var HypoMuxSetupHelper
Var HypoMuxSetupOperation
LangString WailsWebViewInstall ${LANG_ENGLISH} "runtime"
%s
Section
InitPluginsDir
Call EnsureWebViewRuntime
FileOpen $0 "%s" w
FileWrite $0 "unsafe"
FileClose $0
SectionEnd
`, parent, function, marker)}
			for i, source := range sources {
				nsi := filepath.Join(dir, fmt.Sprintf("%d.nsi", i))
				if e := os.WriteFile(nsi, []byte("\xef\xbb\xbf"+source), 0600); e != nil {
					t.Fatal(e)
				}
				if out, e := exec.Command(compiler, "/V2", nsi).CombinedOutput(); e != nil {
					t.Fatalf("compile: %v %s", e, out)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			out, e := exec.CommandContext(ctx, parent, "/S").CombinedOutput()
			exit, ok := e.(*exec.ExitError)
			if !ok || exit.ExitCode() != 71 {
				t.Fatalf("runtime failure accepted: %v %s", e, out)
			}
			if _, e := os.Stat(marker); !os.IsNotExist(e) {
				t.Fatal("payload changed after runtime failure")
			}
		})
	}
}

func TestInstallerNativeHelperEntrypoints(t *testing.T) {
	helper := os.Getenv("HYPOMUX_DESKTOP_HELPER_TEST")
	if helper == "" {
		t.Skip("set HYPOMUX_DESKTOP_HELPER_TEST to a freshly built desktop executable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, helper, "--setup-transaction", "begin", "--scope", "invalid").CombinedOutput()
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 2 || !strings.Contains(string(out), "invalid install scope") {
		t.Fatalf("setup entry did not run headlessly: %v %s", err, out)
	}
	dir, err := os.MkdirTemp("", "HypoMuxUpdate-中文 % & ")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	installer := filepath.Join(dir, "HypoMux_Setup_2.7.1.exe")
	if err = os.WriteFile(installer, []byte("unsigned harmless fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	token := "0123456789abcdef0123456789abcdef"
	out, err = exec.CommandContext(ctx, helper, "--run-update", installer, fmt.Sprint(os.Getpid()), token).CombinedOutput()
	exit, ok = err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 1 {
		t.Fatalf("update entry did not reject unsigned fixture headlessly: %v %s", err, out)
	}
	report, err := os.ReadFile(filepath.Join(dir, "update-result.log"))
	if err != nil || !strings.Contains(string(report), "FAILED:") {
		t.Fatalf("missing durable failure report: %v %s", err, report)
	}
	ready, err := os.ReadFile(filepath.Join(dir, "ready-"+token))
	if err != nil || len(ready) == 0 || string(ready) == "ready" {
		t.Fatalf("bad failure handshake: %v %s", err, ready)
	}
	if _, err = os.Stat(installer); err != nil {
		t.Fatal("failed installer was deleted", err)
	}
}
