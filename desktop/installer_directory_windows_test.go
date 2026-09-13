package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

func TestInstallerDirectoryInitializationWiring(t *testing.T) {
	data, err := os.ReadFile("build/windows/nsis/project.nsi")
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if strings.Contains(s, "InstallDirRegKey ") || strings.Contains(s, "MUI_PAGE_CUSTOMFUNCTION_PRE") {
		t.Fatal("directory must be resolved once in .onInit, without the implicit 32-bit registry lookup")
	}
	start := strings.Index(s, "Function .onInit")
	end := strings.Index(s[start:], "FunctionEnd") + start
	init := s[start:end]
	view := strings.Index(init, "SetRegView 64")
	for _, hive := range []string{"HKCU", "HKLM"} {
		read := strings.Index(init, `ReadRegStr $HypoMuxPreviousInstallDir `+hive+` "${UNINST_KEY}" "InstallLocation"`)
		resolve := strings.Index(init, "Call HypoMuxInitializeInstallDir")
		if view < 0 || read <= view || resolve <= read {
			t.Fatalf("%s directory lookup must use the 64-bit view before resolving the destination", hive)
		}
	}
}

// Compile the production directory resolver into a harmless NSIS executable.
// No application files, services or production registry keys are touched.
func TestInstallerDirectoryResolutionNSIS(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("requires Windows to execute NSIS")
	}
	compiler := os.Getenv("MAKENSIS")
	if compiler == "" {
		var err error
		compiler, err = exec.LookPath("makensis")
		if err != nil {
			t.Skip("set MAKENSIS to run NSIS integration tests")
		}
	}
	include, err := filepath.Abs("build/windows/nsis/install-directory.nsh")
	if err != nil {
		t.Fatal(err)
	}
	for _, scope := range []string{"machine", "user"} {
		defaultDir := `$PROGRAMFILES64\HypoMux`
		if scope == "user" {
			defaultDir = `$LOCALAPPDATA\Programs\HypoMux`
		}
		for _, tc := range []struct{ name, previous, override, expected string }{
			{"fresh", "", "", defaultDir},
			{"upgrade", `D:\我的应用\HypoMux`, "", `D:\我的应用\HypoMux`},
			{"trailing-separator", `D:\Apps\HypoMux\`, "", `D:\Apps\HypoMux`},
			{"explicit-custom", `D:\Old\HypoMux`, `C:\New Apps\HypoMux`, `C:\New Apps\HypoMux`},
			{"explicit-default", `D:\Old\HypoMux`, `C:\Program Files\HypoMux`, `C:\Program Files\HypoMux`},
		} {
			t.Run(scope+"/"+tc.name, func(t *testing.T) {
				dir := t.TempDir()
				probe := filepath.Join(dir, "probe.nsi")
				exe := filepath.Join(dir, "probe.exe")
				source := fmt.Sprintf(`Unicode true
!include "LogicLib.nsh"
!define WAILS_INSTALL_SCOPE "%s"
!define INFO_PRODUCTNAME "HypoMux"
Name "HypoMux directory regression"
OutFile "%s"
RequestExecutionLevel user
SilentInstall silent
InstallDir ""
Var HypoMuxPreviousInstallDir
!include "%s"
Function .onInit
    StrCpy $HypoMuxPreviousInstallDir "%s"
    Call HypoMuxInitializeInstallDir
FunctionEnd
Section
    StrCmp $INSTDIR "%s" passed
    SetErrorLevel 1
    Quit
    passed:
    SetErrorLevel 0
SectionEnd
`, scope, exe, include, tc.previous, tc.expected)
				if err := os.WriteFile(probe, []byte(source), 0600); err != nil {
					t.Fatal(err)
				}
				if out, err := exec.Command(compiler, "/V2", probe).CombinedOutput(); err != nil {
					t.Fatalf("compile: %v\n%s", err, out)
				}
				// NSIS requires /D= to be last and unquoted, including spaces.
				cmd := exec.Command(exe, "/S")
				if tc.override != "" {
					cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `"` + exe + `" /S /D=` + tc.override}
				}
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("directory resolution: %v\n%s", err, out)
				}
			})
		}
	}
}
