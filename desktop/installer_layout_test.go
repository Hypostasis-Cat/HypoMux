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
		`Call StopCoreProcessesForUpgrade`,
		`Function RemoveLegacyAutostartTask`,
		`/Delete /TN "\HypoMuxAutoStart" /F`,
		`File /oname=legacy-v22-recover.ps1 "legacy-v22-recover.ps1"`,
		`File /oname=stop-core-for-upgrade.ps1 "stop-core-for-upgrade.ps1"`,
		`File /oname=compare-install-directories.ps1 "compare-install-directories.ps1"`,
		`File /oname=protect-core-directory.ps1 "protect-core-directory.ps1"`,
		`!define HYPOMUX_PROTECTED_CORE_ROOT "$APPDATA\HypoMux\Core"`,
		`!define HYPOMUX_CORE_POLICY_KEY "Software\HypoMux\CoreServicePolicy"`,
		`Call PrepareProtectedCoreDirectory`,
		`Call FinalizeProtectedCoreDirectory`,
		`Function RollbackFreshMachineInstall`,
		`Call RollbackFreshMachineInstall`,
		`ReadRegStr $2 HKLM "${HYPOMUX_NESTED_UNINST_KEY}" "InstallLocation"`,
		`Var HypoMuxPreviousInstallDir`,
		`Var HypoMuxAutostartEnabled`,
		`Call DetermineInstallPathChange`,
		`Call RecoverPreviousWailsInstallation`,
		`Call RemovePreviousWailsInstallation`,
		`Call RestoreAutostart`,
		`WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Run" "HypoMux" '$\"$INSTDIR\${PRODUCT_EXECUTABLE}$\" --silent'`,
		`"${HYPOMUX_PROTECTED_CORE_BIN}\hypomux-engine.exe" install-service --desktop "$INSTDIR\${PRODUCT_EXECUTABLE}"`,
		`Delete "$APPDATA\HypoMuxCoreRuntime\tun-config-*.json"`,
		`IfFileExists "$INSTDIR\bin\hypomux-engine.exe" 0 serviceRemoveRaw`,
		`"$SYSDIR\sc.exe" delete "${HYPOMUX_CORE_SERVICE}"`,
		`%USERPROFILE%\.hypomux`,
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("installer is missing %q", required)
		}
	}
	if !strings.Contains(script, `Call RemoveLegacyAutostartTask`) ||
		!strings.Contains(script, `Call un.RemoveLegacyAutostartTask`) {
		t.Fatal("installer and uninstaller must both remove the legacy elevated autostart task")
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
	quiesceAt := strings.Index(script, "Call StopCoreProcessesForUpgrade")
	writeAt := strings.Index(script, "SetOutPath $INSTDIR")
	legacyTaskCleanupAt := strings.Index(script, "Call RemoveLegacyAutostartTask")
	if closeAt < 0 || legacyRecoverAt <= closeAt || wailsRecoverAt <= legacyRecoverAt ||
		migrateAt <= wailsRecoverAt || quiesceAt <= migrateAt || writeAt <= quiesceAt ||
		legacyTaskCleanupAt < 0 || legacyTaskCleanupAt >= writeAt {
		t.Fatalf(
			"upgrade order is unsafe: task-cleanup=%d close=%d legacy-recover=%d wails-recover=%d migrate=%d quiesce=%d write=%d",
			legacyTaskCleanupAt, closeAt, legacyRecoverAt, wailsRecoverAt, migrateAt, quiesceAt, writeAt,
		)
	}
	disableAt := strings.Index(script, `sc.exe" config "${HYPOMUX_CORE_SERVICE}" start= disabled`)
	stopAt := strings.Index(script, `sc.exe" stop "${HYPOMUX_CORE_SERVICE}"`)
	if disableAt < 0 || stopAt <= disableAt {
		t.Fatalf("Core service restart must be disabled before stop: disable=%d stop=%d", disableAt, stopAt)
	}
}

func TestInstallerMigratesRegisteredWailsInstallWhenDirectoryChanges(t *testing.T) {
	data, err := os.ReadFile("build/windows/nsis/project.nsi")
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	captureAt := strings.Index(script, `ReadRegStr $HypoMuxPreviousInstallDir HKLM "${UNINST_KEY}" "InstallLocation"`)
	compareAt := strings.Index(script, `Call DetermineInstallPathChange`)
	recoverAt := strings.Index(script, `Call RecoverPreviousWailsInstallation`)
	copyAt := strings.Index(script, `!insertmacro wails.files`)
	commitAt := strings.Index(script, `!insertmacro wails.writeUninstaller`)
	removeAt := strings.Index(script, `Call RemovePreviousWailsInstallation`)
	restoreAt := strings.Index(script, `Call RestoreAutostart`)
	if captureAt < 0 || compareAt <= captureAt || recoverAt <= compareAt || copyAt <= recoverAt || commitAt <= copyAt || removeAt <= commitAt || restoreAt <= removeAt {
		t.Fatalf(
			"changed-directory migration order is unsafe: capture=%d compare=%d recover=%d copy=%d commit=%d remove=%d restore-autostart=%d",
			captureAt,
			compareAt,
			recoverAt,
			copyAt,
			commitAt,
			removeAt,
			restoreAt,
		)
	}
	comparison, err := os.ReadFile("build/windows/nsis/compare-install-directories.ps1")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		`GetFileInformationByHandle`,
		`VolumeSerialNumber`,
		`FileIndexHigh`,
		`FileIndexLow`,
		`FileFlagBackupSemantics`,
	} {
		if !strings.Contains(string(comparison), required) {
			t.Fatalf("directory identity comparison is missing %q", required)
		}
	}
	for _, unsafe := range []string{
		`RMDir /r "$HypoMuxPreviousInstallDir"`,
		`RMDir /r "$2"`,
	} {
		if strings.Contains(script, unsafe) {
			t.Fatalf("changed-directory migration recursively removes the previous path via %q", unsafe)
		}
	}
}

func TestMachineInstallerSeparatesProtectedServiceFromCustomDesktopPath(t *testing.T) {
	data, err := os.ReadFile("build/windows/nsis/project.nsi")
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	protectedCopyAt := strings.Index(script, `SetOutPath "${HYPOMUX_PROTECTED_CORE_BIN}"`)
	protectAt := strings.Index(script, `Call FinalizeProtectedCoreDirectory`)
	installAt := strings.Index(script, `"${HYPOMUX_PROTECTED_CORE_BIN}\hypomux-engine.exe" install-service --desktop "$INSTDIR\${PRODUCT_EXECUTABLE}"`)
	if protectedCopyAt < 0 || protectAt <= protectedCopyAt || installAt <= protectAt {
		t.Fatalf(
			"protected Core order is unsafe: copy=%d protect=%d install=%d",
			protectedCopyAt,
			protectAt,
			installAt,
		)
	}
	if strings.Contains(script, `"$INSTDIR\bin\hypomux-engine.exe" install-service`) {
		t.Fatal("machine service still runs from the user-selected application directory")
	}
	if strings.Contains(script, `RMDir /r $INSTDIR`) || strings.Contains(script, `RMDir /r "$INSTDIR"`) {
		t.Fatal("uninstaller still recursively removes the user-selected application directory")
	}

	protection, err := os.ReadFile("build/windows/nsis/protect-core-directory.ps1")
	if err != nil {
		t.Fatal(err)
	}
	protectionScript := string(protection)
	for _, required := range []string{
		`CommonApplicationData`,
		`BuiltinAdministratorsSid`,
		`LocalSystemSid`,
		`BuiltinUsersSid`,
		`SetAccessRuleProtection($true, $false)`,
		`[System.IO.DirectoryInfo]::new`,
		`[System.IO.FileInfo]::new`,
		`[System.IO.FileSystemAclExtensions]::SetAccessControl`,
		`ReparsePoint`,
	} {
		if !strings.Contains(protectionScript, required) {
			t.Fatalf("protected Core preparation is missing %q", required)
		}
	}
	if strings.Contains(protectionScript, `Set-Acl -LiteralPath`) {
		t.Fatal("protected Core preparation must not depend on the Microsoft.PowerShell.Security module")
	}
}

func TestInstallerClearsInheritedPowerShellModulePath(t *testing.T) {
	data, err := os.ReadFile("build/windows/nsis/project.nsi")
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	if !strings.Contains(script, `System::Call 'kernel32::SetEnvironmentVariable(t "PSModulePath", p 0)'`) {
		t.Fatal("installer does not clear the inherited PowerShell module path")
	}
	if count := strings.Count(script, `!insertmacro HypoMuxClearInheritedPSModulePath`); count != 2 {
		t.Fatalf("installer and uninstaller must both clear the inherited PowerShell module path, got %d calls", count)
	}
}

func TestInstallerCoreShutdownBarrierIsPathScopedAndBounded(t *testing.T) {
	data, err := os.ReadFile("build/windows/nsis/stop-core-for-upgrade.ps1")
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	for _, required := range []string{
		`[System.IO.Path]::GetFullPath($_.Path).Equals(`,
		`[System.StringComparison]::OrdinalIgnoreCase`,
		`Stop-Process -Id $process.Id -Force`,
		`[System.IO.FileShare]::None`,
		`[DateTime]::UtcNow -lt $deadline`,
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("Core shutdown barrier is missing safety guard %q", required)
		}
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
	for _, required := range []string{
		`INSTALLER_PATH: desktop/bin/hypomux-amd64-installer.exe`,
		`cp artifacts/hypomux-amd64-installer.exe "artifacts/HypoMux_Setup_${version}.exe"`,
	} {
		if !strings.Contains(workflow, required) {
			t.Fatalf("release workflow installer casing is inconsistent: missing %q", required)
		}
	}
	taskData, err := os.ReadFile("build/windows/Taskfile.yml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(taskData), `--input "{{.BIN_DIR}}/{{.APP_NAME}}-{{.ARCH}}-installer.exe"`) {
		t.Fatal("local installer signing task does not target the packaged NSIS artifact")
	}
}

func TestReleasePublishesOneSignedInstallerAndManifestToBothMirrors(t *testing.T) {
	data, err := os.ReadFile("../.github/workflows/build.yml")
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(data)
	for _, required := range []string{
		`installer_sha256="$(sha256sum "${installer_path}" | awk '{print $1}')"`,
		`schema_version: 1`,
		`urls: [$cnb_url, $github_url]`,
		`artifacts/latest.json`,
		`docker://docker.cnb.cool/looc/git-cnb:1.2.0`,
		`secrets.CNB_TOKEN`,
		`release asset-upload -t ${{ github.ref_name }}`,
	} {
		if !strings.Contains(workflow, required) {
			t.Fatalf("dual-source release workflow is missing %q", required)
		}
	}
	if strings.Count(workflow, `cp artifacts/hypomux-amd64-installer.exe`) != 1 {
		t.Fatal("release workflow must stage exactly one signed installer for both mirrors")
	}
}

func TestVersionMetadataIsConsistent(t *testing.T) {
	const version = "2.5.7"
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
	runtimeSource := string(runtimeData)
	if !strings.Contains(runtimeSource, `System.IsDesktop()`) {
		t.Fatal("frontend runtime detection does not use the Wails v3 API")
	}
	if !strings.Contains(runtimeSource, `runtimeWindow.chrome?.webview?.postMessage`) {
		t.Fatal("desktop detection still depends only on the asynchronously injected Wails environment")
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
	backgroundSource := string(backgroundData)
	if strings.Contains(backgroundSource, `const { localBackgroundUrl, ...persistable }`) {
		t.Fatal("frontend still strips the custom background before durable persistence")
	}
	for _, required := range []string{
		`let persistedLocalBackgroundURL: string | undefined`,
		`let appearanceSaveQueue: Promise<void> = Promise.resolve()`,
		`localBackgroundURL === persistedLocalBackgroundURL`,
		`await appServices.appearance.save(JSON.stringify(settingsToSave))`,
		`persistedLocalBackgroundURL = localBackgroundURL`,
	} {
		if !strings.Contains(backgroundSource, required) {
			t.Fatalf("appearance persistence protocol is missing %q", required)
		}
	}
	if strings.Contains(backgroundSource, `localBackgroundUrl.length > 100000`) {
		t.Fatal("frontend still guesses background persistence from Data URL size")
	}
}

func TestHomeEngineStatePersistsAcrossPageNavigation(t *testing.T) {
	appData, err := os.ReadFile("frontend/src/App.tsx")
	if err != nil {
		t.Fatal(err)
	}
	shellData, err := os.ReadFile("frontend/src/components/shell/AppShell.tsx")
	if err != nil {
		t.Fatal(err)
	}
	appSource := string(appData)
	shellSource := string(shellData)
	for _, required := range []string{
		`persistentPage="home"`,
		`persistentChildren={<HomePage onNavigate={navigate} />}`,
		`hidden={page !== persistentPage}`,
		`page !== persistentPage ? (`,
	} {
		if !strings.Contains(appSource+shellSource, required) {
			t.Fatalf("persistent home-page state wiring is missing %q", required)
		}
	}
	if strings.Contains(appSource, `: <HomePage onNavigate={navigate} />}`) {
		t.Fatal("HomePage is still conditionally mounted and will lose an in-flight engine transition")
	}
}

func TestFrontendFreshInstallAppearanceDefaults(t *testing.T) {
	mainData, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	mainSource := string(mainData)
	if !strings.Contains(mainSource, `BackgroundType:   application.BackgroundTypeTranslucent`) {
		t.Fatal("window does not support the translucent composition path used by optional Mica")
	}
	if strings.Contains(mainSource, `BackgroundType:   application.BackgroundTypeTransparent`) {
		t.Fatal("Mica window still uses the transparent path that skips backdrop initialisation")
	}
	if !strings.Contains(mainSource, `BackdropType:                      application.None`) {
		t.Fatal("fresh installs still request a native backdrop before appearance settings load")
	}

	presetData, err := os.ReadFile("frontend/src/theme/appearance.presets.ts")
	if err != nil {
		t.Fatal(err)
	}
	preset := string(presetData)
	for _, required := range []string{
		`schemaVersion: 2`,
		`presetId: "windows-mica"`,
		`mode: "system"`,
		`material: "mica"`,
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
	if !strings.Contains(tokens, `[data-background-source="local"][data-panel-material="blur"] .glass-surface`) ||
		!strings.Contains(tokens, `[data-background-source="local"][data-panel-material="blur"] .network-adapter`) {
		t.Fatal("card frosting is not scoped to custom backgrounds")
	}

	settingsPageData, err := os.ReadFile("frontend/src/pages/SettingsPage.tsx")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(settingsPageData), `close_to_tray: false`) {
		t.Fatal("settings page fallback does not exit directly on close")
	}
	settingsPage := string(settingsPageData)
	for _, required := range []string{
		`backgroundSource: "system",`,
		`material: "mica",`,
		`presetId: "windows-mica",`,
		`disabled={appearance.backgroundSource !== "local"}`,
		`disabled={appearance.backgroundSource !== "local" || appearance.panelMaterial !== "blur"}`,
	} {
		if !strings.Contains(settingsPage, required) {
			t.Fatalf("appearance controls are missing %q", required)
		}
	}

	storeData, err := os.ReadFile("frontend/src/theme/appearance.store.tsx")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(storeData), `return { ...value, schemaVersion: 2 }`) {
		t.Fatal("legacy appearance settings are not migrated without overwriting the selected preset")
	}
}

func TestWindowsTaskManagerUsesProductName(t *testing.T) {
	infoData, err := os.ReadFile("build/windows/info.json")
	if err != nil {
		t.Fatal(err)
	}
	mainData, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	for path, source := range map[string]string{
		"build/windows/info.json": string(infoData),
		"main.go":                 string(mainData),
	} {
		if !strings.Contains(source, `Description": "HypoMux"`) &&
			!strings.Contains(source, `Description: "HypoMux"`) {
			t.Fatalf("%s does not expose HypoMux as the Windows application description", path)
		}
		if strings.Contains(source, "Multi-link network aggregation desktop client") {
			t.Fatalf("%s still exposes the tagline as the Task Manager application name", path)
		}
	}
	for _, language := range []string{`"0000"`, `"0409"`} {
		if !strings.Contains(string(infoData), language) {
			t.Fatalf("Windows version strings are missing language fallback %s", language)
		}
	}
	if strings.Count(string(infoData), `"FileVersion": "2.5.7"`) != 2 {
		t.Fatal("Windows neutral and en-US version tables must both expose FileVersion")
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
		`.hm-card::before`,
		`.hm-card::after`,
		`--hm-card-light-strength: 0.72`,
		`var(--hm-card-glow-border) 34%`,
		`drop-shadow(0 0 20px var(--hm-card-glow-far))`,
		`@media (hover: hover) and (pointer: fine)`,
	} {
		if !strings.Contains(css, required) {
			t.Fatalf("card hover treatment is missing %q", required)
		}
	}
	for _, removed := range []string{
		`.network-adapter:hover {`,
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
