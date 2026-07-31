package main

import (
	"os"
	"strings"
	"testing"
)

func TestInstallerMigratesLegacyLayoutsBeforeWritingCorrectedRoot(t *testing.T) {
	data, err := os.ReadFile("build/windows/nsis/project.nsi")
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	for _, required := range []string{
		`!define UNINST_KEY_NAME "HypoMux"`,
		`InstallDir "$PROGRAMFILES64\${INFO_PRODUCTNAME}"`,
		`HypoMuxHypoMux`,
		`{7637d353-b9c0-4145-bc81-7a474e534d07}_is1`,
		`Call RemoveLegacyInstallations`,
		`%USERPROFILE%\.hypomux`,
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("installer is missing %q", required)
		}
	}
	if strings.Contains(script, `InstallDir "$PROGRAMFILES64\${INFO_COMPANYNAME}\${INFO_PRODUCTNAME}"`) {
		t.Fatal("installer still uses the duplicated company/product directory")
	}
	migrateAt := strings.Index(script, "Call RemoveLegacyInstallations")
	writeAt := strings.Index(script, "SetOutPath $INSTDIR")
	if migrateAt < 0 || writeAt < 0 || migrateAt >= writeAt {
		t.Fatalf("legacy migration must happen before files are written: migrate=%d write=%d", migrateAt, writeAt)
	}
}

func TestApplicationIdentityAndSingleInstanceAreStable(t *testing.T) {
	mainData, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	configData, err := os.ReadFile("build/config.yml")
	if err != nil {
		t.Fatal(err)
	}
	manifestData, err := os.ReadFile("build/windows/wails.exe.manifest")
	if err != nil {
		t.Fatal(err)
	}
	const applicationID = "io.hypomux.desktop"
	if !strings.Contains(string(mainData), `SingleInstance: &application.SingleInstanceOptions{`) ||
		!strings.Contains(string(mainData), `UniqueID: "`+applicationID+`"`) {
		t.Fatal("desktop entry point is missing the stable single-instance identity")
	}
	if !strings.Contains(string(configData), `productIdentifier: "`+applicationID+`"`) {
		t.Fatal("build configuration does not use the stable product identifier")
	}
	if !strings.Contains(string(manifestData), `name="`+applicationID+`"`) {
		t.Fatal("Windows manifest does not use the stable application identity")
	}
}
