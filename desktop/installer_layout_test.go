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
		`Call RecoverLegacyV22Network`,
		`Call RecoverWailsInstallations`,
		`File /oname=legacy-v22-recover.ps1 "legacy-v22-recover.ps1"`,
		`%USERPROFILE%\.hypomux`,
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("installer is missing %q", required)
		}
	}
	if strings.Contains(script, `InstallDir "$PROGRAMFILES64\${INFO_COMPANYNAME}\${INFO_PRODUCTNAME}"`) {
		t.Fatal("installer still uses the duplicated company/product directory")
	}
	uninstallAt := strings.Index(script, `Section "uninstall"`)
	if uninstallAt < 0 {
		t.Fatal("installer is missing its uninstall section")
	}
	if strings.Contains(script[:uninstallAt], `IfFileExists "$INSTDIR\${PRODUCT_EXECUTABLE}" 0 +2`) {
		t.Fatal("installer can still launch a legacy Python UI with the Wails-only recovery argument")
	}
	closeAt := strings.Index(script, "Call CloseRunningHypoMux")
	legacyRecoverAt := strings.Index(script, "Call RecoverLegacyV22Network")
	wailsRecoverAt := strings.Index(script, "Call RecoverWailsInstallations")
	migrateAt := strings.Index(script, "Call RemoveLegacyInstallations")
	writeAt := strings.Index(script, "SetOutPath $INSTDIR")
	if closeAt < 0 || legacyRecoverAt <= closeAt || wailsRecoverAt <= legacyRecoverAt ||
		migrateAt <= wailsRecoverAt || writeAt <= migrateAt {
		t.Fatalf(
			"upgrade order is unsafe: close=%d legacy-recover=%d wails-recover=%d migrate=%d write=%d",
			closeAt, legacyRecoverAt, wailsRecoverAt, migrateAt, writeAt,
		)
	}
}

func TestLegacyRecoveryIsNarrowlyScoped(t *testing.T) {
	data, err := os.ReadFile("build/windows/nsis/legacy-v22-recover.ps1")
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	for _, required := range []string{
		`StartsWith($ownedRoot, [System.StringComparison]::OrdinalIgnoreCase)`,
		`Where-Object InterfaceAlias -EQ 'HypoMux-Tun'`,
		`Join-Path $DataRoot 'config.json'`,
		`[string]$current.ProxyServer -ne $expected`,
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("legacy recovery is missing safety guard %q", required)
		}
	}
}

func TestReleasePublishesLegacyUpdaterCompatibleInstallerName(t *testing.T) {
	data, err := os.ReadFile("../.github/workflows/build.yml")
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(data)
	if !strings.Contains(workflow, `version="${GITHUB_REF_NAME#v}"`) ||
		!strings.Contains(workflow, `HypoMux_Setup_${version}.exe`) {
		t.Fatal("release workflow does not publish the installer name recognized by v2.2.0")
	}
}

func TestVersionMetadataIsConsistent(t *testing.T) {
	const version = "2.5.0"
	files := []string{
		"Taskfile.yml",
		"build/config.yml",
		"build/windows/info.json",
		"build/windows/wails.exe.manifest",
		"build/windows/nsis/wails_tools.nsh",
		"build/windows/msix/template.xml",
		"build/windows/msix/app_manifest.xml",
		"frontend/package.json",
		"frontend/src/product.ts",
		"internal/services/updater.go",
	}
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), version) {
			t.Fatalf("%s does not contain release version %s", path, version)
		}
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
