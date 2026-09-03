# -*- coding: UTF-8 -*-
Unicode true

####
## Please note: Template replacements don't work in this file. They are provided with default defines like
## mentioned underneath.
## If the keyword is not defined, "wails_tools.nsh" will populate them.
## If they are defined here, "wails_tools.nsh" will not touch them. This allows you to use this project.nsi manually
## from outside of Wails for debugging and development of the installer.
##
## For development first make a wails nsis build to populate the "wails_tools.nsh":
## > wails build --target windows/amd64 --nsis
## Then you can call makensis on this file with specifying the path to your binary:
## For a AMD64 only installer:
## > makensis -DARG_WAILS_AMD64_BINARY=..\..\bin\app.exe
## For a ARM64 only installer:
## > makensis -DARG_WAILS_ARM64_BINARY=..\..\bin\app.exe
## For a installer with both architectures:
## > makensis -DARG_WAILS_AMD64_BINARY=..\..\bin\app-amd64.exe -DARG_WAILS_ARM64_BINARY=..\..\bin\app-arm64.exe
####
## The following information is taken from the wails_tools.nsh file, but they can be overwritten here.
####
## !define INFO_PROJECTNAME    "my-project" # Default "HypoMux"
## !define INFO_COMPANYNAME    "My Company" # Default "HypoMux"
## !define INFO_PRODUCTNAME    "My Product Name" # Default "HypoMux"
## !define INFO_PRODUCTVERSION "1.0.0"     # Default "2.5.9"
## !define INFO_COPYRIGHT      "(c) Now, My Company" # Default "© 2026, My Company"
###
## !define PRODUCT_EXECUTABLE  "Application.exe"      # Default "${INFO_PROJECTNAME}.exe"
## !define UNINST_KEY_NAME     "UninstKeyInRegistry"  # Default "${INFO_COMPANYNAME}${INFO_PRODUCTNAME}"
####
## !define REQUEST_EXECUTION_LEVEL "admin"            # Default "admin"  see also https://nsis.sourceforge.io/Docs/Chapter4.html
## !define WAILS_INSTALL_SCOPE     "user"             # Default "machine" - set to "user" for per-user install ($LOCALAPPDATA) without UAC prompt
####
## Include the wails tools
####
!include "LogicLib.nsh"
; Keep the uninstall identity stable even when company and product names are
; identical. The generated default would otherwise become HypoMuxHypoMux.
!define UNINST_KEY_NAME "HypoMux"
!include "wails_tools.nsh"

!define HYPOMUX_CORE_SERVICE "HypoMuxCore"
; wails.setShellContext selects the common shell folders for machine installs,
; so $APPDATA resolves to FOLDERID_ProgramData in the code paths below.
!define HYPOMUX_PROTECTED_CORE_ROOT "$APPDATA\HypoMux\Core"
!define HYPOMUX_PROTECTED_CORE_BIN "${HYPOMUX_PROTECTED_CORE_ROOT}\bin"
!define HYPOMUX_CORE_POLICY_KEY "Software\HypoMux\CoreServicePolicy"
!define HYPOMUX_NESTED_UNINST_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\HypoMuxHypoMux"
!define HYPOMUX_LEGACY_INNO_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\{7637d353-b9c0-4145-bc81-7a474e534d07}_is1"

# The version information for this two must consist of 4 parts
VIProductVersion "${INFO_PRODUCTVERSION}.0"
VIFileVersion    "${INFO_PRODUCTVERSION}.0"

VIAddVersionKey "CompanyName"     "${INFO_COMPANYNAME}"
VIAddVersionKey "FileDescription" "${INFO_PRODUCTNAME} Installer"
VIAddVersionKey "ProductVersion"  "${INFO_PRODUCTVERSION}"
VIAddVersionKey "FileVersion"     "${INFO_PRODUCTVERSION}"
VIAddVersionKey "LegalCopyright"  "${INFO_COPYRIGHT}"
VIAddVersionKey "ProductName"     "${INFO_PRODUCTNAME}"

# Enable HiDPI support. https://nsis.sourceforge.io/Reference/ManifestDPIAware
ManifestDPIAware true
# WinVer.nsh relies on this declaration on Windows 8.1 and later. Windows 11
# uses the Windows 10 supportedOS identifier as well.
ManifestSupportedOS Win10

!include "MUI.nsh"

!if "${WAILS_INSTALL_SCOPE}" == "user"
    !define MUI_LANGDLL_REGISTRY_ROOT HKCU
!else
    !define MUI_LANGDLL_REGISTRY_ROOT HKLM
!endif
!define MUI_LANGDLL_REGISTRY_KEY "${UNINST_KEY}"
!define MUI_LANGDLL_REGISTRY_VALUENAME "InstallerLanguage"
!define MUI_LANGDLL_WINDOWTITLE "选择安装语言 / Select Setup Language"
!define MUI_LANGDLL_INFO "请选择安装程序使用的语言。 / Please select the setup language."

!define MUI_ICON "..\icon.ico"
!define MUI_UNICON "..\icon.ico"
# !define MUI_WELCOMEFINISHPAGE_BITMAP "resources\leftimage.bmp" #Include this to add a bitmap on the left side of the Welcome Page. Must be a size of 164x314
!define MUI_FINISHPAGE_NOAUTOCLOSE # Wait on the INSTFILES page so the user can take a look into the details of the installation steps
!define MUI_ABORTWARNING # This will warn the user if they exit from the installer.

!insertmacro MUI_PAGE_WELCOME # Welcome to the installer page.
# !insertmacro MUI_PAGE_LICENSE "resources\eula.txt" # Adds a EULA page to the installer
!insertmacro MUI_PAGE_DIRECTORY # In which folder install page.
!insertmacro MUI_PAGE_INSTFILES # Installing page.
!insertmacro MUI_PAGE_FINISH # Finished installation page.

!insertmacro MUI_UNPAGE_INSTFILES # Uninstalling page

!insertmacro MUI_LANGUAGE "English"
!insertmacro MUI_LANGUAGE "SimpChinese"
!insertmacro MUI_RESERVEFILE_LANGDLL

LangString WailsWin10Required ${LANG_ENGLISH} "This product is only supported on Windows 10 (Server 2016) and later."
LangString WailsWin10Required ${LANG_SIMPCHINESE} "本产品仅支持 Windows 10（Server 2016）及更高版本。"
LangString WailsArchitectureNotSupported ${LANG_ENGLISH} "This product cannot be installed on the current Windows architecture. Supported architectures: ${ARCH}."
LangString WailsArchitectureNotSupported ${LANG_SIMPCHINESE} "本产品无法安装到当前 Windows 架构。支持的架构：${ARCH}。"
LangString WailsWebViewInstall ${LANG_ENGLISH} "Installing: Microsoft Edge WebView2 Runtime"
LangString WailsWebViewInstall ${LANG_SIMPCHINESE} "正在安装：Microsoft Edge WebView2 运行时"
LangString CoreServiceInstalling ${LANG_ENGLISH} "Installing and starting HypoMux Core Service..."
LangString CoreServiceInstalling ${LANG_SIMPCHINESE} "正在安装并启动 HypoMux Core 服务……"
LangString CoreServiceInstalled ${LANG_ENGLISH} "HypoMux Core Service installed and started."
LangString CoreServiceInstalled ${LANG_SIMPCHINESE} "HypoMux Core 服务已安装并启动。"
LangString CoreServiceInstallFailed ${LANG_ENGLISH} "Failed to install HypoMux Core Service. Exit code:"
LangString CoreServiceInstallFailed ${LANG_SIMPCHINESE} "HypoMux Core 服务安装失败。退出代码："
LangString CoreDirectoryPrepareFailed ${LANG_ENGLISH} "Failed to prepare the protected HypoMux Core directory."
LangString CoreDirectoryPrepareFailed ${LANG_SIMPCHINESE} "无法准备受保护的 HypoMux Core 目录。"
LangString CoreDirectoryFinalizeFailed ${LANG_ENGLISH} "Failed to secure the HypoMux Core files."
LangString CoreDirectoryFinalizeFailed ${LANG_SIMPCHINESE} "无法保护 HypoMux Core 文件。"
LangString InstallPathInspectFailed ${LANG_ENGLISH} "Could not safely compare the previous and requested HypoMux installation directories."
LangString InstallPathInspectFailed ${LANG_SIMPCHINESE} "无法安全比较 HypoMux 的旧安装目录与新安装目录。"
LangString CoreServiceRemoving ${LANG_ENGLISH} "Stopping and removing HypoMux Core Service..."
LangString CoreServiceRemoving ${LANG_SIMPCHINESE} "正在停止并移除 HypoMux Core 服务……"
LangString CoreServiceRemoved ${LANG_ENGLISH} "HypoMux Core Service removed."
LangString CoreServiceRemoved ${LANG_SIMPCHINESE} "HypoMux Core 服务已移除。"
LangString CoreServiceRemoveFailed ${LANG_ENGLISH} "Failed to remove HypoMux Core Service. Exit code:"
LangString CoreServiceRemoveFailed ${LANG_SIMPCHINESE} "HypoMux Core 服务移除失败。退出代码："
LangString RunningAppClosing ${LANG_ENGLISH} "Closing the running HypoMux application..."
LangString RunningAppClosing ${LANG_SIMPCHINESE} "正在关闭运行中的 HypoMux…"
LangString RunningAppCloseFailed ${LANG_ENGLISH} "HypoMux is still running. Close it and click Retry to continue installation."
LangString RunningAppCloseFailed ${LANG_SIMPCHINESE} "HypoMux 仍在运行。请关闭软件后点击“重试”继续安装。"
LangString CoreServiceStopping ${LANG_ENGLISH} "Stopping HypoMux Core Service before updating files..."
LangString CoreServiceStopping ${LANG_SIMPCHINESE} "正在停止 HypoMux Core 服务以更新文件…"
LangString CoreServiceForceStopping ${LANG_ENGLISH} "The previous Core Service did not stop in time; terminating its service process..."
LangString CoreServiceForceStopping ${LANG_SIMPCHINESE} "旧版 Core 服务未能及时停止，正在结束其服务进程…"
LangString CoreServiceStopFailed ${LANG_ENGLISH} "Could not stop HypoMux Core Service. Setup cannot safely replace the application files."
LangString CoreServiceStopFailed ${LANG_SIMPCHINESE} "无法停止 HypoMux Core 服务，安装程序不能安全替换应用文件。"
LangString CoreProcessStopping ${LANG_ENGLISH} "Stopping remaining HypoMux Core processes before updating files..."
LangString CoreProcessStopping ${LANG_SIMPCHINESE} "正在结束残留的 HypoMux Core 进程以更新文件…"
LangString CoreProcessStopFailed ${LANG_ENGLISH} "The old HypoMux Core file cannot be updated yet. Close HypoMux or wait a moment, then retry."
LangString CoreProcessStopFailed ${LANG_SIMPCHINESE} "旧版 HypoMux Core 文件暂时无法更新。请关闭 HypoMux 或稍候片刻，然后重试。"
LangString InstallerAlreadyRunning ${LANG_ENGLISH} "HypoMux Setup is already running. Finish or close it before starting another installer."
LangString InstallerAlreadyRunning ${LANG_SIMPCHINESE} "HypoMux 安装程序已在运行，请先完成或关闭它。"
LangString LegacyInstallRemoving ${LANG_ENGLISH} "Removing the previous HypoMux installation before migrating files..."
LangString LegacyInstallRemoving ${LANG_SIMPCHINESE} "正在移除旧版 HypoMux 并迁移安装目录…"
LangString LegacyInstallRemoveFailed ${LANG_ENGLISH} "The previous HypoMux installation could not be removed safely."
LangString LegacyInstallRemoveFailed ${LANG_SIMPCHINESE} "无法安全移除旧版 HypoMux，安装已停止。"
LangString LegacyNetworkRecovering ${LANG_ENGLISH} "Recovering network state left by HypoMux v2.2.0..."
LangString LegacyNetworkRecovering ${LANG_SIMPCHINESE} "正在恢复 HypoMux v2.2.0 的网络状态…"
LangString LegacyNetworkRecoverFailed ${LANG_ENGLISH} "Could not safely recover the network state left by HypoMux v2.2.0."
LangString LegacyNetworkRecoverFailed ${LANG_SIMPCHINESE} "无法安全恢复 HypoMux v2.2.0 的网络状态，安装已停止。"
LangString WailsNetworkRecoverFailed ${LANG_ENGLISH} "Could not safely recover the network state left by the previous HypoMux release."
LangString WailsNetworkRecoverFailed ${LANG_SIMPCHINESE} "无法安全恢复上一版 HypoMux 的网络状态，安装已停止。"

!define WAILS_WIN10_REQUIRED "$(WailsWin10Required)"
!define WAILS_ARCHITECTURE_NOT_SUPPORTED "$(WailsArchitectureNotSupported)"
!define WAILS_INSTALL_WEBVIEW_DETAILPRINT "$(WailsWebViewInstall)"

## The following two statements can be used to sign the installer and the uninstaller. The path to the binaries are provided in %1
#!uninstfinalize 'signtool --file "%1"'
#!finalize 'signtool --file "%1"'

Name "${INFO_PRODUCTNAME}"
OutFile "..\..\..\bin\${INFO_PROJECTNAME}-${ARCH}-installer.exe" # Name of the installer's file.
!if "${WAILS_INSTALL_SCOPE}" == "user"
    InstallDir "$LOCALAPPDATA\Programs\${INFO_PRODUCTNAME}"
    InstallDirRegKey HKCU "${UNINST_KEY}" "InstallLocation"
!else
    InstallDir "$PROGRAMFILES64\${INFO_PRODUCTNAME}"
    InstallDirRegKey HKLM "${UNINST_KEY}" "InstallLocation"
!endif
ShowInstDetails show # This will always show the installation details.

Var HypoMuxFreshInstall
Var HypoMuxPreviousInstallDir
Var HypoMuxInstallPathChanged
Var HypoMuxAutostartEnabled

!macro HypoMuxClearInheritedPSModulePath
   ; PowerShell 7 normally substitutes a Windows PowerShell-only module path
   ; when it launches powershell.exe directly. NSIS breaks that direct parent
   ; chain, so remove the inherited value in this process and let every Windows
   ; PowerShell 5.1 child reconstruct its own default PSModulePath.
   System::Call 'kernel32::SetEnvironmentVariable(t "PSModulePath", p 0)'
!macroend

!macro HypoMuxEnsureSingleInstaller
   ; A machine-wide mutex prevents two installers from racing while one of
   ; them stops and replaces the Core service files.
   System::Call 'kernel32::CreateMutex(p 0, i 0, t "Global\HypoMux-Installer-4C1461C5-0555-4F4C-9D47-6619C5167414") p .r0 ?e'
   Pop $1
   ${If} $1 == 183
       IfSilent hypoMuxSingleInstanceAbort 0
       MessageBox MB_OK|MB_ICONEXCLAMATION "$(InstallerAlreadyRunning)"
       hypoMuxSingleInstanceAbort:
       SetErrorLevel 66
       Abort
   ${EndIf}
!macroend

Function HypoMuxCheckPlatform
   ; WinVer.nsh is the primary check. Some modified Windows installations and
   ; future builds can still expose a compatibility version, so fall back to
   ; the protected CurrentVersion registry data before rejecting the system.
   ${If} ${AtLeastWin10}
       Goto hypoMuxPlatformArchitecture
   ${EndIf}

   SetRegView 64
   ClearErrors
   ReadRegDWORD $0 HKLM "SOFTWARE\Microsoft\Windows NT\CurrentVersion" "CurrentMajorVersionNumber"
   IfErrors hypoMuxPlatformCheckBuild
   IntCmp $0 10 hypoMuxPlatformArchitecture hypoMuxPlatformUnsupportedWindows hypoMuxPlatformArchitecture

hypoMuxPlatformCheckBuild:
   ClearErrors
   ReadRegStr $0 HKLM "SOFTWARE\Microsoft\Windows NT\CurrentVersion" "CurrentBuildNumber"
   IfErrors hypoMuxPlatformUnsupportedWindows
   ; Windows 10 starts at build 10240; Server 2016 is newer (14393).
   IntCmp $0 10240 hypoMuxPlatformArchitecture hypoMuxPlatformUnsupportedWindows hypoMuxPlatformArchitecture

hypoMuxPlatformUnsupportedWindows:
   IfSilent hypoMuxPlatformSilentWindows hypoMuxPlatformVisibleWindows
hypoMuxPlatformSilentWindows:
   SetErrorLevel 64
   Abort
hypoMuxPlatformVisibleWindows:
   MessageBox MB_OK "${WAILS_WIN10_REQUIRED}"
   Quit

hypoMuxPlatformArchitecture:
   !ifdef SUPPORTS_AMD64
       ${If} ${IsNativeAMD64}
           Return
       ${EndIf}
   !endif
   !ifdef SUPPORTS_ARM64
       ${If} ${IsNativeARM64}
           Return
       ${EndIf}
   !endif

   IfSilent hypoMuxPlatformSilentArchitecture hypoMuxPlatformVisibleArchitecture
hypoMuxPlatformSilentArchitecture:
   SetErrorLevel 65
   Abort
hypoMuxPlatformVisibleArchitecture:
   MessageBox MB_OK "${WAILS_ARCHITECTURE_NOT_SUPPORTED}"
   Quit
FunctionEnd

Function .onInit
   !insertmacro HypoMuxClearInheritedPSModulePath
   !insertmacro MUI_LANGDLL_DISPLAY
   !insertmacro HypoMuxEnsureSingleInstaller
   Call HypoMuxCheckPlatform
   StrCpy $HypoMuxFreshInstall "1"
   StrCpy $HypoMuxPreviousInstallDir ""
   StrCpy $HypoMuxInstallPathChanged "0"
   StrCpy $HypoMuxAutostartEnabled "0"
   ReadRegStr $0 HKCU "Software\Microsoft\Windows\CurrentVersion\Run" "HypoMux"
   ${If} $0 != ""
       StrCpy $HypoMuxAutostartEnabled "1"
   ${EndIf}
   !if "${WAILS_INSTALL_SCOPE}" == "user"
       ReadRegStr $HypoMuxPreviousInstallDir HKCU "${UNINST_KEY}" "InstallLocation"
   !else
       SetRegView 64
       ReadRegStr $HypoMuxPreviousInstallDir HKLM "${UNINST_KEY}" "InstallLocation"
       ${If} $HypoMuxPreviousInstallDir != ""
           StrCpy $HypoMuxFreshInstall "0"
       ${EndIf}
       nsExec::Exec '"$SYSDIR\sc.exe" query "${HYPOMUX_CORE_SERVICE}"'
       Pop $1
       ${If} $1 == 0
           StrCpy $HypoMuxFreshInstall "0"
       ${EndIf}
   !endif
FunctionEnd

Function un.onInit
   !insertmacro HypoMuxClearInheritedPSModulePath
   !insertmacro MUI_UNGETLANGUAGE
   !insertmacro HypoMuxEnsureSingleInstaller
FunctionEnd

Function RemoveLegacyAutostartTask
    ; v2.2.0 used a highest-privilege logon task. Remove the exact
    ; application-owned task on every install/upgrade instead of relying on
    ; the legacy uninstaller registration still being intact.
    nsExec::ExecToStack '"$SYSDIR\schtasks.exe" /Delete /TN "\HypoMuxAutoStart" /F'
    Pop $0
    Pop $1
FunctionEnd

Function un.RemoveLegacyAutostartTask
    nsExec::ExecToStack '"$SYSDIR\schtasks.exe" /Delete /TN "\HypoMuxAutoStart" /F'
    Pop $0
    Pop $1
FunctionEnd

Function RestoreAutostart
    ${If} $HypoMuxAutostartEnabled == "1"
        ; A legacy uninstaller may have removed this value, and a changed
        ; install directory makes the previous command invalid. Preserve the
        ; preference while always committing the newly installed executable.
        WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Run" "HypoMux" '$\"$INSTDIR\${PRODUCT_EXECUTABLE}$\" --silent'
    ${EndIf}
FunctionEnd

Function CloseRunningHypoMux
closeRetry:
    SetDetailsPrint textonly
    DetailPrint "$(RunningAppClosing)"
    SetDetailsPrint both
    nsExec::ExecToStack '"$SYSDIR\WindowsPowerShell\v1.0\powershell.exe" -NoProfile -NonInteractive -ExecutionPolicy Bypass -Command "Get-Process -Name ${INFO_PROJECTNAME} -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue; Wait-Process -Name ${INFO_PROJECTNAME} -Timeout 15 -ErrorAction SilentlyContinue; if (Get-Process -Name ${INFO_PROJECTNAME} -ErrorAction SilentlyContinue) { exit 1 }"'
    Pop $0
    Pop $1
    ${If} $0 == 0
        Return
    ${EndIf}
    DetailPrint "$1"
    MessageBox MB_RETRYCANCEL|MB_ICONEXCLAMATION "$(RunningAppCloseFailed)" IDRETRY closeRetry
    Abort "$(RunningAppCloseFailed)"
FunctionEnd

Function StopCoreServiceForUpgrade
    SetDetailsPrint textonly
    DetailPrint "$(CoreServiceStopping)"
    SetDetailsPrint both
    ; Disable restart before requesting a stop. A previous Core can have
    ; recovery actions, and install-service restores Automatic start after
    ; the new executable has been written successfully.
    nsExec::Exec '"$SYSDIR\sc.exe" query "${HYPOMUX_CORE_SERVICE}"'
    Pop $0
    ${If} $0 != 0
        Return
    ${EndIf}
    nsExec::Exec '"$SYSDIR\sc.exe" config "${HYPOMUX_CORE_SERVICE}" start= disabled'
    Pop $0
    ${If} $0 != 0
        Abort "$(CoreServiceStopFailed)"
    ${EndIf}

    ; sc.exe only submits the stop request. Waiting is kept in a separate
    ; bounded process so a deadlocked legacy service cannot freeze Setup.
    nsExec::Exec '"$SYSDIR\sc.exe" stop "${HYPOMUX_CORE_SERVICE}"'
    Pop $0
    nsExec::ExecToStack '"$SYSDIR\WindowsPowerShell\v1.0\powershell.exe" -NoProfile -NonInteractive -ExecutionPolicy Bypass -Command "try { if (Get-Service -Name ${HYPOMUX_CORE_SERVICE} -ErrorAction SilentlyContinue) { (Get-Service -Name ${HYPOMUX_CORE_SERVICE}).WaitForStatus([System.ServiceProcess.ServiceControllerStatus]::Stopped, [TimeSpan]::FromSeconds(20)) }; exit 0 } catch { exit 2 }"'
    Pop $0
    Pop $1
    ${If} $0 == 0
        Return
    ${EndIf}
    DetailPrint "$(CoreServiceForceStopping)"
    ; Old releases could deadlock in synchronous ConnectNamedPipe. Terminate
    ; only the process hosting HypoMuxCore; automatic restart is already off.
    nsExec::Exec '"$SYSDIR\taskkill.exe" /F /FI "SERVICES eq ${HYPOMUX_CORE_SERVICE}"'
    Pop $0
    Sleep 1000
    nsExec::ExecToStack '"$SYSDIR\WindowsPowerShell\v1.0\powershell.exe" -NoProfile -NonInteractive -ExecutionPolicy Bypass -Command "if ((Get-CimInstance Win32_Service | Where-Object Name -EQ ${HYPOMUX_CORE_SERVICE}).ProcessId -ne 0) { exit 1 }"'
    Pop $0
    Pop $1
    ${If} $0 != 0
        DetailPrint "$1"
        Abort "$(CoreServiceStopFailed)"
    ${EndIf}
FunctionEnd

Function DetermineInstallPathChange
    StrCpy $HypoMuxInstallPathChanged "0"
    ${If} $HypoMuxPreviousInstallDir == ""
        Return
    ${EndIf}
    GetFullPathName $2 "$HypoMuxPreviousInstallDir"
    GetFullPathName $3 "$INSTDIR"
    ${If} $2 == $3
        Return
    ${EndIf}
    ClearErrors
    CreateDirectory "$INSTDIR"
    IfErrors 0 +2
        Abort "$(InstallPathInspectFailed)"
    InitPluginsDir
    SetOutPath "$PLUGINSDIR"
    File /oname=compare-install-directories.ps1 "compare-install-directories.ps1"
    nsExec::ExecToStack '"$SYSDIR\WindowsPowerShell\v1.0\powershell.exe" -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "$PLUGINSDIR\compare-install-directories.ps1" -PreviousPath "$HypoMuxPreviousInstallDir" -RequestedPath "$INSTDIR"'
    Pop $0
    Pop $1
    ${If} $0 == 0
        Return
    ${EndIf}
    ${If} $0 == 1
        StrCpy $HypoMuxInstallPathChanged "1"
        Return
    ${EndIf}
    DetailPrint "$1"
    Abort "$(InstallPathInspectFailed)"
FunctionEnd

Function StopCoreProcessesForUpgrade
    SetDetailsPrint textonly
    DetailPrint "$(CoreProcessStopping)"
    SetDetailsPrint both
    ; Recovery commands can briefly launch Core outside the service. Use the
    ; exact installation path as the ownership boundary, then require an
    ; exclusive open before NSIS attempts to replace the executable.
    InitPluginsDir
    SetOutPath "$PLUGINSDIR"
    File /oname=stop-core-for-upgrade.ps1 "stop-core-for-upgrade.ps1"

    stopCoreProcessesRetry:

    ; If the user changed the directory on the installer page, the previous
    ; registered Core still has to be unlocked before its exact owned files
    ; can be removed after the new installation commits.
    ${If} $HypoMuxInstallPathChanged == "1"
        nsExec::ExecToStack '"$SYSDIR\WindowsPowerShell\v1.0\powershell.exe" -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "$PLUGINSDIR\stop-core-for-upgrade.ps1" -EnginePath "$HypoMuxPreviousInstallDir\bin\hypomux-engine.exe"'
        Pop $0
        Pop $1
        ${If} $0 != 0
            DetailPrint "$1"
            IfSilent stopCoreProcessesAbort 0
            MessageBox MB_RETRYCANCEL|MB_ICONEXCLAMATION "$(CoreProcessStopFailed)" IDRETRY stopCoreProcessesRetry
            Goto stopCoreProcessesAbort
        ${EndIf}
    ${EndIf}

    nsExec::ExecToStack '"$SYSDIR\WindowsPowerShell\v1.0\powershell.exe" -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "$PLUGINSDIR\stop-core-for-upgrade.ps1" -EnginePath "$INSTDIR\bin\hypomux-engine.exe"'
    Pop $0
    Pop $1
    ${If} $0 != 0
        DetailPrint "$1"
        IfSilent stopCoreProcessesAbort 0
        MessageBox MB_RETRYCANCEL|MB_ICONEXCLAMATION "$(CoreProcessStopFailed)" IDRETRY stopCoreProcessesRetry
        Goto stopCoreProcessesAbort
    ${EndIf}
    Return

    stopCoreProcessesAbort:
    Abort "$(CoreProcessStopFailed)"
FunctionEnd

Function PrepareProtectedCoreDirectory
    InitPluginsDir
    SetOutPath "$PLUGINSDIR"
    File /oname=protect-core-directory.ps1 "protect-core-directory.ps1"
    nsExec::ExecToStack '"$SYSDIR\WindowsPowerShell\v1.0\powershell.exe" -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "$PLUGINSDIR\protect-core-directory.ps1" -CoreRoot "${HYPOMUX_PROTECTED_CORE_ROOT}" -Phase Prepare'
    Pop $0
    Pop $1
    ${If} $0 != 0
        DetailPrint "$1"
        Abort "$(CoreDirectoryPrepareFailed)"
    ${EndIf}
FunctionEnd

Function FinalizeProtectedCoreDirectory
    nsExec::ExecToStack '"$SYSDIR\WindowsPowerShell\v1.0\powershell.exe" -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "$PLUGINSDIR\protect-core-directory.ps1" -CoreRoot "${HYPOMUX_PROTECTED_CORE_ROOT}" -Phase Finalize'
    Pop $0
    Pop $1
    ${If} $0 != 0
        DetailPrint "$1"
        Call RollbackFreshMachineInstall
        Abort "$(CoreDirectoryFinalizeFailed)"
    ${EndIf}
FunctionEnd

Function RollbackFreshMachineInstall
    ${If} $HypoMuxFreshInstall != "1"
        Return
    ${EndIf}
    IfFileExists "${HYPOMUX_PROTECTED_CORE_BIN}\hypomux-engine.exe" 0 rollbackWithAppCore
        nsExec::ExecToLog '"${HYPOMUX_PROTECTED_CORE_BIN}\hypomux-engine.exe" remove-service'
        Goto rollbackServiceRemoved
    rollbackWithAppCore:
    IfFileExists "$INSTDIR\bin\hypomux-engine.exe" 0 rollbackServiceRemoved
        nsExec::ExecToLog '"$INSTDIR\bin\hypomux-engine.exe" remove-service'
    rollbackServiceRemoved:
    Delete "${HYPOMUX_PROTECTED_CORE_BIN}\libcronet.dll"
    Delete "${HYPOMUX_PROTECTED_CORE_BIN}\wintun.dll"
    Delete "${HYPOMUX_PROTECTED_CORE_BIN}\sing-box.exe"
    Delete "${HYPOMUX_PROTECTED_CORE_BIN}\hypomux-engine.exe"
    RMDir "${HYPOMUX_PROTECTED_CORE_BIN}"
    RMDir "${HYPOMUX_PROTECTED_CORE_ROOT}"
    RMDir "$APPDATA\HypoMux"
    Delete "$APPDATA\HypoMuxCoreRuntime\tun-config-*.json"
    RMDir "$APPDATA\HypoMuxCoreRuntime"

    Delete "$INSTDIR\bin\libcronet.dll"
    Delete "$INSTDIR\bin\wintun.dll"
    Delete "$INSTDIR\bin\sing-box.exe"
    Delete "$INSTDIR\bin\hypomux-engine.exe"
    RMDir "$INSTDIR\bin"
    Delete "$INSTDIR\${PRODUCT_EXECUTABLE}"
    RMDir "$INSTDIR"
FunctionEnd

Function RecoverLegacyV22Network
    !if "${WAILS_INSTALL_SCOPE}" != "user"
        SetRegView 64
        ReadRegStr $0 HKLM "${HYPOMUX_LEGACY_INNO_KEY}" "UninstallString"
        ${If} $0 == ""
            IfFileExists "$PROGRAMFILES64\HypoMux\python313.dll" 0 legacyV22RecoveryDone
        ${EndIf}

        ; v2.2.0 has no --recover-network command. Launching the old executable
        ; with that argument starts its full UI and makes nsExec wait forever.
        ; Use a dedicated, bounded recovery script instead. It only terminates
        ; backend processes owned by the old installation and only clears the
        ; WinINet proxy when it exactly matches the ports in legacy config.json.
        DetailPrint "$(LegacyNetworkRecovering)"
        InitPluginsDir
        SetOutPath "$PLUGINSDIR"
        File /oname=legacy-v22-recover.ps1 "legacy-v22-recover.ps1"
        nsExec::ExecToStack '"$SYSDIR\WindowsPowerShell\v1.0\powershell.exe" -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "$PLUGINSDIR\legacy-v22-recover.ps1" -InstallRoot "$PROGRAMFILES64\HypoMux" -DataRoot "$PROFILE\.hypomux"'
        Pop $0
        Pop $1
        ${If} $0 != 0
            DetailPrint "$1"
            Abort "$(LegacyNetworkRecoverFailed)"
        ${EndIf}
        legacyV22RecoveryDone:
    !endif
FunctionEnd

Function RecoverWailsInstallations
    ; The independent Core executable is the layout marker for every Wails
    ; release. Python v2.2.0 never shipped it, so the legacy UI can never be
    ; accidentally relaunched with an unsupported recovery argument.
    IfFileExists "$INSTDIR\bin\hypomux-engine.exe" 0 nestedWailsRecovery
        nsExec::ExecToStack '"$INSTDIR\bin\hypomux-engine.exe" recover'
        Pop $0
        Pop $1
        ${If} $0 != 0
            DetailPrint "$1"
            Abort "$(WailsNetworkRecoverFailed)"
        ${EndIf}
        IfFileExists "$INSTDIR\${PRODUCT_EXECUTABLE}" 0 nestedWailsRecovery
            nsExec::ExecToStack '"$INSTDIR\${PRODUCT_EXECUTABLE}" --recover-network'
            Pop $0
            Pop $1
            ${If} $0 != 0
                DetailPrint "$1"
                Abort "$(WailsNetworkRecoverFailed)"
            ${EndIf}

    nestedWailsRecovery:
    !if "${WAILS_INSTALL_SCOPE}" != "user"
        ; Recover the short-lived duplicated Company/Product layout shipped by
        ; earlier migration builds before its uninstaller removes that tree.
        IfFileExists "$PROGRAMFILES64\HypoMux\HypoMux\bin\hypomux-engine.exe" 0 wailsRecoveryDone
            nsExec::ExecToStack '"$PROGRAMFILES64\HypoMux\HypoMux\bin\hypomux-engine.exe" recover'
            Pop $0
            Pop $1
            ${If} $0 != 0
                DetailPrint "$1"
                Abort "$(WailsNetworkRecoverFailed)"
            ${EndIf}
            IfFileExists "$PROGRAMFILES64\HypoMux\HypoMux\${PRODUCT_EXECUTABLE}" 0 wailsRecoveryDone
                nsExec::ExecToStack '"$PROGRAMFILES64\HypoMux\HypoMux\${PRODUCT_EXECUTABLE}" --recover-network'
                Pop $0
                Pop $1
                ${If} $0 != 0
                    DetailPrint "$1"
                    Abort "$(WailsNetworkRecoverFailed)"
                ${EndIf}
        wailsRecoveryDone:
    !endif
FunctionEnd

Function RecoverPreviousWailsInstallation
    ${If} $HypoMuxInstallPathChanged != "1"
        Return
    ${EndIf}

    ; A changed destination must still recover the registered Wails build.
    ; Otherwise killing its UI can leave WinINet proxy state behind while all
    ; later recovery probes incorrectly inspect only the new, empty directory.
    IfFileExists "$HypoMuxPreviousInstallDir\bin\hypomux-engine.exe" 0 previousWailsRecoveryDone
        nsExec::ExecToStack '"$HypoMuxPreviousInstallDir\bin\hypomux-engine.exe" recover'
        Pop $0
        Pop $1
        ${If} $0 != 0
            DetailPrint "$1"
            Abort "$(WailsNetworkRecoverFailed)"
        ${EndIf}
        IfFileExists "$HypoMuxPreviousInstallDir\${PRODUCT_EXECUTABLE}" 0 previousWailsRecoveryDone
            nsExec::ExecToStack '"$HypoMuxPreviousInstallDir\${PRODUCT_EXECUTABLE}" --recover-network'
            Pop $0
            Pop $1
            ${If} $0 != 0
                DetailPrint "$1"
                Abort "$(WailsNetworkRecoverFailed)"
            ${EndIf}
    previousWailsRecoveryDone:
FunctionEnd

Function RemovePreviousWailsInstallation
    ${If} $HypoMuxInstallPathChanged != "1"
        Return
    ${EndIf}

    ; Require the Wails Core marker and delete only files owned by HypoMux.
    ; Unknown user files keep the old directory non-empty and are preserved.
    IfFileExists "$HypoMuxPreviousInstallDir\bin\hypomux-engine.exe" 0 previousWailsRemovalDone
        Delete "$HypoMuxPreviousInstallDir\bin\libcronet.dll"
        Delete "$HypoMuxPreviousInstallDir\bin\wintun.dll"
        Delete "$HypoMuxPreviousInstallDir\bin\sing-box.exe"
        Delete "$HypoMuxPreviousInstallDir\bin\hypomux-engine.exe"
        RMDir "$HypoMuxPreviousInstallDir\bin"
        Delete "$HypoMuxPreviousInstallDir\uninstall.exe"
        Delete "$HypoMuxPreviousInstallDir\${PRODUCT_EXECUTABLE}"
        RMDir "$HypoMuxPreviousInstallDir"
    previousWailsRemovalDone:
FunctionEnd

Function RemoveLegacyInstallations
    !if "${WAILS_INSTALL_SCOPE}" != "user"
        SetRegView 64

        ; Affected Wails builds used CompanyName/ProductName as two path
        ; segments and registered under HypoMuxHypoMux. Remove that exact
        ; installation before the corrected root is populated.
        ReadRegStr $0 HKLM "${HYPOMUX_NESTED_UNINST_KEY}" "UninstallString"
        ReadRegStr $2 HKLM "${HYPOMUX_NESTED_UNINST_KEY}" "InstallLocation"
        ; Some machines retained the obsolete duplicate key after upgrading its
        ; payload in place. Never let that stale registration launch the current
        ; uninstaller and erase the installation we are about to update.
        ${If} $2 != ""
            GetFullPathName $2 "$2"
            GetFullPathName $3 "$INSTDIR"
            ${If} $2 == $3
                DeleteRegKey HKLM "${HYPOMUX_NESTED_UNINST_KEY}"
                Goto nestedRemoved
            ${EndIf}
        ${EndIf}
        ${If} $0 == "$INSTDIR\uninstall.exe"
            DeleteRegKey HKLM "${HYPOMUX_NESTED_UNINST_KEY}"
            Goto nestedRemoved
        ${EndIf}
        ${If} $0 == '$\"$INSTDIR\uninstall.exe$\"'
            DeleteRegKey HKLM "${HYPOMUX_NESTED_UNINST_KEY}"
            Goto nestedRemoved
        ${EndIf}
        ${If} $0 == ""
            IfFileExists "$PROGRAMFILES64\HypoMux\HypoMux\uninstall.exe" 0 nestedRemoved
            StrCpy $0 "$PROGRAMFILES64\HypoMux\HypoMux\uninstall.exe"
        ${EndIf}
        DetailPrint "$(LegacyInstallRemoving)"
        ClearErrors
        ExecWait '"$0" /S' $1
        IfErrors nestedUninstallFailed
        ${If} $1 != 0
            Goto nestedUninstallFailed
        ${EndIf}
        Goto nestedUninstallSucceeded
        nestedUninstallFailed:
        Abort "$(LegacyInstallRemoveFailed)"
        nestedUninstallSucceeded:
        DeleteRegKey HKLM "${HYPOMUX_NESTED_UNINST_KEY}"
        RMDir /r "$PROGRAMFILES64\HypoMux\HypoMux"
        nestedRemoved:

        ; v2.2.0 and earlier used this stable Inno Setup AppId. Running the
        ; registered uninstaller removes only files owned by that package;
        ; user configuration under %USERPROFILE%\.hypomux is untouched.
        ReadRegStr $0 HKLM "${HYPOMUX_LEGACY_INNO_KEY}" "UninstallString"
        ${If} $0 != ""
            DetailPrint "$(LegacyInstallRemoving)"
            ClearErrors
            ExecWait '$0 /VERYSILENT /SUPPRESSMSGBOXES /NORESTART' $1
            IfErrors legacyInnoUninstallFailed
            ${If} $1 != 0
                Goto legacyInnoUninstallFailed
            ${EndIf}
            Goto legacyInnoUninstallSucceeded
            legacyInnoUninstallFailed:
            Abort "$(LegacyInstallRemoveFailed)"
            legacyInnoUninstallSucceeded:
            DeleteRegKey HKLM "${HYPOMUX_LEGACY_INNO_KEY}"
        ${EndIf}

        ; Never mix the Python/PySide payload with the Wails application if a
        ; damaged legacy registration points to a missing uninstaller.
        IfFileExists "$PROGRAMFILES64\HypoMux\python313.dll" 0 legacyRemoved
            Abort "$(LegacyInstallRemoveFailed)"
        legacyRemoved:
    !endif
FunctionEnd

Section
    !insertmacro wails.setShellContext

    ; Compare directory identities before changing services, network state or
    ; files. String comparison is unsafe for 8.3 names, junctions and symlinks.
    Call DetermineInstallPathChange

    !insertmacro wails.webview2runtime

    Call RemoveLegacyAutostartTask

    ; Close the previous UI and stop Core before replacing binaries. Recovery
    ; is layout-aware: v2.2.0 uses its dedicated cleanup script, while current
    ; and future Wails builds use their supported recovery entry points.
    Call CloseRunningHypoMux
    !if "${WAILS_INSTALL_SCOPE}" != "user"
        Call StopCoreServiceForUpgrade
    !endif
    Call RecoverPreviousWailsInstallation
    Call RecoverLegacyV22Network
    Call RecoverWailsInstallations

    Call RemoveLegacyInstallations

    ; Final path-scoped barrier: nothing may still own the old executable
    ; when NSIS reaches the File instruction below.
    Call StopCoreProcessesForUpgrade

    !if "${WAILS_INSTALL_SCOPE}" != "user"
        ; v2.5.5 hotfix: the desktop may live on any drive, but LocalSystem
        ; must never execute its service image from that user-selected path.
        Call PrepareProtectedCoreDirectory
    !endif

    SetOutPath $INSTDIR

    !insertmacro wails.files

    ; The UI host remains unprivileged. TUN elevation is requested only for
    ; this independently launched Core through the authenticated pipe.
    SetOutPath "$INSTDIR\bin"
    File "/oname=hypomux-engine.exe" "..\..\..\bin\hypomux-engine.exe"
    File "/oname=sing-box.exe" "..\..\..\bin\sing-box.exe"
    File "/oname=wintun.dll" "..\..\..\bin\wintun.dll"
    File /nonfatal "/oname=libcronet.dll" "..\..\..\bin\libcronet.dll"
    SetOutPath $INSTDIR

    !if "${WAILS_INSTALL_SCOPE}" != "user"
        SetOutPath "${HYPOMUX_PROTECTED_CORE_BIN}"
        File "/oname=hypomux-engine.exe" "..\..\..\bin\hypomux-engine.exe"
        File "/oname=sing-box.exe" "..\..\..\bin\sing-box.exe"
        File "/oname=wintun.dll" "..\..\..\bin\wintun.dll"
        File /nonfatal "/oname=libcronet.dll" "..\..\..\bin\libcronet.dll"
        Call FinalizeProtectedCoreDirectory
        SetOutPath $INSTDIR
    !endif

    ; Machine installation elevates once and installs the isolated privileged
    ; Core. The Wails/WebView2 executable remains asInvoker.
    !if "${WAILS_INSTALL_SCOPE}" != "user"
        DetailPrint "$(CoreServiceInstalling)"
        nsExec::ExecToStack '"${HYPOMUX_PROTECTED_CORE_BIN}\hypomux-engine.exe" install-service --desktop "$INSTDIR\${PRODUCT_EXECUTABLE}"'
        Pop $0
        Pop $1
        ${If} $0 != 0
            DetailPrint "$1"
            Call RollbackFreshMachineInstall
            Abort "$(CoreServiceInstallFailed) $0"
        ${EndIf}
        DetailPrint "$(CoreServiceInstalled)"
    !endif

    CreateShortcut "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"
    CreateShortCut "$DESKTOP\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"

    !insertmacro wails.associateFiles
    !insertmacro wails.associateCustomProtocols

    !insertmacro wails.writeUninstaller
    !if "${WAILS_INSTALL_SCOPE}" == "user"
        WriteRegStr HKCU "${UNINST_KEY}" "InstallLocation" "$INSTDIR"
    !else
        WriteRegStr HKLM "${UNINST_KEY}" "InstallLocation" "$INSTDIR"
    !endif
    ; Commit the new uninstaller registration before removing the previous
    ; exact Wails payload. A late cleanup failure cannot orphan Add/Remove Apps.
    Call RemovePreviousWailsInstallation
    Call RestoreAutostart
SectionEnd

Section "uninstall"
    !insertmacro wails.setShellContext

    Call un.RemoveLegacyAutostartTask

    ; Stop the ordinary-permission UI first, then recover the current user's
    ; proxy snapshot and the machine-owned TUN state before files disappear.
    nsExec::Exec '"$SYSDIR\taskkill.exe" /IM "${PRODUCT_EXECUTABLE}" /T /F'
    Pop $0
    !if "${WAILS_INSTALL_SCOPE}" != "user"
        SetRegView 64
        DetailPrint "$(CoreServiceRemoving)"
        IfFileExists "${HYPOMUX_PROTECTED_CORE_BIN}\hypomux-engine.exe" 0 serviceRemoveWithAppCore
            StrCpy $2 "${HYPOMUX_PROTECTED_CORE_BIN}\hypomux-engine.exe"
            Goto serviceRemoveInvoke
        serviceRemoveWithAppCore:
        IfFileExists "$INSTDIR\bin\hypomux-engine.exe" 0 serviceRemoveRaw
            StrCpy $2 "$INSTDIR\bin\hypomux-engine.exe"
        serviceRemoveInvoke:
            nsExec::ExecToStack '"$2" remove-service'
            Pop $0
            Pop $1
            ${If} $0 != 0
                DetailPrint "$1"
                Abort "$(CoreServiceRemoveFailed) $0"
            ${EndIf}
            Goto serviceRemoved
        serviceRemoveRaw:
            ; If security software removed both Core copies, remove the exact
            ; application-owned service registration without depending on them.
            nsExec::Exec '"$SYSDIR\sc.exe" query "${HYPOMUX_CORE_SERVICE}"'
            Pop $0
            ${If} $0 == 0
                nsExec::Exec '"$SYSDIR\sc.exe" stop "${HYPOMUX_CORE_SERVICE}"'
                Pop $1
                nsExec::Exec '"$SYSDIR\sc.exe" delete "${HYPOMUX_CORE_SERVICE}"'
                Pop $0
                ${If} $0 != 0
                ${AndIf} $0 != 1060
                ${AndIf} $0 != 1072
                    Abort "$(CoreServiceRemoveFailed) $0"
                ${EndIf}
            ${EndIf}
            DeleteRegKey HKLM "${HYPOMUX_CORE_POLICY_KEY}"
        serviceRemoved:
        DetailPrint "$(CoreServiceRemoved)"
        Delete "${HYPOMUX_PROTECTED_CORE_BIN}\libcronet.dll"
        Delete "${HYPOMUX_PROTECTED_CORE_BIN}\wintun.dll"
        Delete "${HYPOMUX_PROTECTED_CORE_BIN}\sing-box.exe"
        Delete "${HYPOMUX_PROTECTED_CORE_BIN}\hypomux-engine.exe"
        RMDir "${HYPOMUX_PROTECTED_CORE_BIN}"
        RMDir "${HYPOMUX_PROTECTED_CORE_ROOT}"
        RMDir "$APPDATA\HypoMux"
        Delete "$APPDATA\HypoMuxCoreRuntime\tun-config-*.json"
        RMDir "$APPDATA\HypoMuxCoreRuntime"
    !endif
    IfFileExists "$INSTDIR\bin\hypomux-engine.exe" 0 +2
        nsExec::ExecToLog '"$INSTDIR\bin\hypomux-engine.exe" recover'
    IfFileExists "$INSTDIR\${PRODUCT_EXECUTABLE}" 0 +2
        nsExec::ExecToLog '"$INSTDIR\${PRODUCT_EXECUTABLE}" --recover-network'

    ; Device-local settings under %USERPROFILE%\.hypomux are deliberately retained
    ; for reinstall/rollback. Autostart, WebView data and installed files are
    ; application-owned and are removed.
    DeleteRegValue HKCU "Software\Microsoft\Windows\CurrentVersion\Run" "HypoMux"

    ; Remove the optional, user-approved NAT detection UDP reply rule.
    nsExec::ExecToLog '"$SYSDIR\netsh.exe" advfirewall firewall delete rule name="HypoMux NAT Type Detection" program="$INSTDIR\${PRODUCT_EXECUTABLE}"'

    RMDir /r "$AppData\${PRODUCT_EXECUTABLE}" # Remove the WebView2 DataPath

    Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk"
    Delete "$DESKTOP\${INFO_PRODUCTNAME}.lnk"

    !insertmacro wails.unassociateFiles
    !insertmacro wails.unassociateCustomProtocols

    !insertmacro wails.deleteUninstaller

    Delete "$INSTDIR\bin\libcronet.dll"
    Delete "$INSTDIR\bin\wintun.dll"
    Delete "$INSTDIR\bin\sing-box.exe"
    Delete "$INSTDIR\bin\hypomux-engine.exe"
    RMDir "$INSTDIR\bin"
    Delete "$INSTDIR\${PRODUCT_EXECUTABLE}"
    RMDir "$INSTDIR"
SectionEnd
