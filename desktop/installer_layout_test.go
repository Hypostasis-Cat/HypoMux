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

func TestFrontendUsesWailsV3RuntimeDetection(t *testing.T) {
	runtimeData, err := os.ReadFile("frontend/src/platform/runtime.ts")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(runtimeData), `System.IsDesktop()`) {
		t.Fatal("frontend runtime detection does not use the Wails v3 API")
	}

	paths := []string{
		"frontend/src/theme/background.service.ts",
		"frontend/src/state/useEngineState.ts",
		"frontend/src/pages/HealthPage.tsx",
		"frontend/src/pages/RoutingPage.tsx",
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "__WAILS__") {
			t.Fatalf("%s still uses the obsolete Wails v2 runtime marker", path)
		}
	}

	backgroundData, err := os.ReadFile("frontend/src/theme/background.service.ts")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(backgroundData), "isDesktopRuntime()") < 2 {
		t.Fatal("appearance persistence is not guarded by the Wails v3 desktop check")
	}
}

func TestFrontendFreshInstallAppearanceDefaults(t *testing.T) {
	presetData, err := os.ReadFile("frontend/src/theme/appearance.presets.ts")
	if err != nil {
		t.Fatal(err)
	}
	preset := string(presetData)
	for _, required := range []string{
		`mode: "system"`,
		`panelOpacity: 50`,
		`panelBlur: 20`,
	} {
		if !strings.Contains(preset, required) {
			t.Fatalf("appearance defaults are missing %q", required)
		}
	}

	tokenData, err := os.ReadFile("frontend/src/theme/material.tokens.css")
	if err != nil {
		t.Fatal(err)
	}
	tokens := string(tokenData)
	if !strings.Contains(tokens, `--hm-panel-opacity: 0.5`) ||
		!strings.Contains(tokens, `--hm-panel-blur: 20px`) {
		t.Fatal("pre-hydration material tokens do not match the appearance defaults")
	}

	settingsPageData, err := os.ReadFile("frontend/src/pages/SettingsPage.tsx")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(settingsPageData), `close_to_tray: false`) {
		t.Fatal("settings page fallback does not exit directly on close")
	}
}

func TestCardsUseThemeAccentHoverGlow(t *testing.T) {
	cssData, err := os.ReadFile("frontend/src/app.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(cssData)
	for _, required := range []string{
		`.hm-card:hover:not(:has(.hm-card:hover))`,
		`.hm-card:hover:not(:has(.hm-card:hover))::before`,
		`border-color: var(--hm-card-glow-border)`,
		`border: 1px solid var(--hm-card-glow-border)`,
		`drop-shadow(0 0 14px var(--hm-card-glow-far))`,
		`@media (hover: hover) and (pointer: fine)`,
	} {
		if !strings.Contains(css, required) {
			t.Fatalf("card hover treatment is missing %q", required)
		}
	}
	for _, removed := range []string{
		`.network-adapter:hover {`,
		`.health-adapter-choice:hover {`,
	} {
		if strings.Contains(css, removed) {
			t.Fatalf("card hover still changes the surface fill via %q", removed)
		}
	}

	tokenData, err := os.ReadFile("frontend/src/theme/material.tokens.css")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(tokenData), `--hm-card-glow-border: color-mix(in srgb, var(--hm-accent)`) {
		t.Fatal("card glow border is not derived from the active theme accent")
	}

	surfaceData, err := os.ReadFile("frontend/src/components/material/GlassSurface.tsx")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(surfaceData), `glass-surface hm-card`) {
		t.Fatal("shared card component does not opt into the hover glow")
	}
}

func TestFrontendUsesWindowsNeutralPaletteAndDistinctWindowMaterials(t *testing.T) {
	tokenData, err := os.ReadFile("frontend/src/theme/material.tokens.css")
	if err != nil {
		t.Fatal(err)
	}
	tokens := string(tokenData)
	for _, required := range []string{
		`--hm-window-base: #f3f3f3`,
		`--hm-window-base: #202020`,
		`--hm-solid-chrome: #f9f9f9`,
		`--hm-solid-chrome: #1c1c1c`,
	} {
		if !strings.Contains(tokens, required) {
			t.Fatalf("Windows neutral appearance token is missing %q", required)
		}
	}

	cssData, err := os.ReadFile("frontend/src/app.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(cssData)
	for _, required := range []string{
		`[data-material="mica"][data-background-source="system"] .app-shell`,
		`[data-material="solid"][data-background-source="system"] .app-shell`,
		`background: var(--hm-window-base)`,
		`[data-material="solid"][data-background-source="system"] .wallpaper-noise`,
	} {
		if !strings.Contains(css, required) {
			t.Fatalf("window material distinction is missing %q", required)
		}
	}
}
