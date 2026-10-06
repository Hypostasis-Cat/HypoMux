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
## INFO_PRODUCTVERSION is generated in version.nsh from desktop/VERSION.
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
!include "version.nsh"
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
VIProductVersion "${HYPOMUX_WINDOWS_VERSION}"
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
!define MUI_CUSTOMFUNCTION_ABORT RollbackSetupTransaction

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
LangString CoreProcessStopping ${LANG_ENGLISH} "Checking existing HypoMux Core files before installation..."
LangString CoreProcessStopping ${LANG_SIMPCHINESE} "正在检查安装前已有的 HypoMux Core 文件…"
LangString CoreProcessStopFailed ${LANG_ENGLISH} "HypoMux Core update check failed. See the diagnostic information below."
LangString CoreProcessStopFailed ${LANG_SIMPCHINESE} "HypoMux Core 更新检查失败。请查看下方诊断信息。"
LangString CoreCheckDetails ${LANG_ENGLISH} "Result: $0$\r$\nFile: $HypoMuxCoreCheckTarget$\r$\nReport: $HypoMuxCoreCheckLog$\r$\n$1"
LangString CoreCheckDetails ${LANG_SIMPCHINESE} "返回值：$0$\r$\n文件：$HypoMuxCoreCheckTarget$\r$\n报告：$HypoMuxCoreCheckLog$\r$\n$1"
LangString CoreCheckLogUnavailable ${LANG_ENGLISH} "Could not write report; copy this message."
LangString CoreCheckLogUnavailable ${LANG_SIMPCHINESE} "无法写入报告，请复制或截图保存此信息。"
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
!ifndef HYPOMUX_INSTALLER_OUTFILE
    !define HYPOMUX_INSTALLER_OUTFILE "..\..\..\bin\${INFO_PROJECTNAME}-${ARCH}-installer.exe"
!endif
OutFile "${HYPOMUX_INSTALLER_OUTFILE}"
InstallDir "" ; Preserve /D=; resolve the default in .onInit using the 64-bit registry.
ShowInstDetails show # This will always show the installation details.

Var HypoMuxSetupActive
Var HypoMuxSetupOperation
Var HypoMuxSetupLog
Var HypoMuxSetupHelper

Var HypoMuxPreviousInstallDir
Var HypoMuxInstallPathChanged
Var HypoMuxAutostartEnabled
Var HypoMuxCoreCheckTarget
Var HypoMuxCoreCheckLog

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
   ${If} $0 == 0
   ${OrIf} $1 == 183
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

!include "install-directory.nsh"

Function .onInit
   SetErrorLevel 1 ; Success is assigned only after transaction commit.
   !insertmacro HypoMuxClearInheritedPSModulePath
   !insertmacro MUI_LANGDLL_DISPLAY
   !insertmacro HypoMuxEnsureSingleInstaller
   Call HypoMuxCheckPlatform
   !insertmacro wails.setShellContext
   Call StageSetupPayload
   StrCpy $HypoMuxSetupOperation "recover"
   Call RunSetupTransaction
   StrCpy $HypoMuxPreviousInstallDir ""
   StrCpy $HypoMuxInstallPathChanged "0"
   StrCpy $HypoMuxAutostartEnabled "0"
   ReadRegStr $0 HKCU "Software\Microsoft\Windows\CurrentVersion\Run" "HypoMux"
   ${If} $0 != ""
       StrCpy $HypoMuxAutostartEnabled "1"
   ${EndIf}
   SetRegView 64
   !if "${WAILS_INSTALL_SCOPE}" == "user"
       ReadRegStr $HypoMuxPreviousInstallDir HKCU "${UNINST_KEY}" "InstallLocation"
   !else
       SetRegView 64
       ReadRegStr $HypoMuxPreviousInstallDir HKLM "${UNINST_KEY}" "InstallLocation"
   !endif
   ${If} $HypoMuxPreviousInstallDir == ""
       ReadRegStr $HypoMuxPreviousInstallDir SHELL_CONTEXT "${HYPOMUX_NESTED_UNINST_KEY}" "InstallLocation"
   ${EndIf}
   ${If} $HypoMuxPreviousInstallDir == ""
       ReadRegStr $HypoMuxPreviousInstallDir SHELL_CONTEXT "${HYPOMUX_LEGACY_INNO_KEY}" "InstallLocation"
   ${EndIf}
   Call HypoMuxInitializeInstallDir
FunctionEnd

Function un.onInit
   !insertmacro HypoMuxClearInheritedPSModulePath
   !insertmacro MUI_UNGETLANGUAGE
   !insertmacro HypoMuxEnsureSingleInstaller
   !insertmacro wails.setShellContext
   Call un.StageSetupPayload
   nsExec::ExecToStack /TIMEOUT=180000 '"$HypoMuxSetupHelper" --setup-transaction recover --scope ${WAILS_INSTALL_SCOPE}'
   Pop $0
   Pop $1
   ${If} $0 != 0
       IfSilent unRecoveryFailed
       MessageBox MB_OK|MB_ICONSTOP "Incomplete setup recovery must finish before uninstalling.$\r$\n$1"
       unRecoveryFailed:
       SetErrorLevel 75
       Abort
   ${EndIf}
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
    ; Do not make a fresh install depend on a script for closing an old UI.
    ; A stale registry entry alone does not establish that an old app exists.
    IfFileExists "$INSTDIR\${PRODUCT_EXECUTABLE}" closeRetry
    ${If} $HypoMuxPreviousInstallDir != ""
        IfFileExists "$HypoMuxPreviousInstallDir\${PRODUCT_EXECUTABLE}" closeRetry
    ${EndIf}
    IfFileExists "$PROGRAMFILES64\HypoMux\${PRODUCT_EXECUTABLE}" closeRetry
    IfFileExists "$PROGRAMFILES64\HypoMux\HypoMux\${PRODUCT_EXECUTABLE}" closeRetry
    Return
closeRetry:
    SetDetailsPrint textonly
    DetailPrint "$(RunningAppClosing)"
    SetDetailsPrint both
    nsExec::ExecToStack /TIMEOUT=20000 '"$SYSDIR\WindowsPowerShell\v1.0\powershell.exe" -NoProfile -NonInteractive -ExecutionPolicy Bypass -Command "Get-Process -Name ${INFO_PROJECTNAME} -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue; Wait-Process -Name ${INFO_PROJECTNAME} -Timeout 15 -ErrorAction SilentlyContinue; if (Get-Process -Name ${INFO_PROJECTNAME} -ErrorAction SilentlyContinue) { exit 1 }; exit 0"'
    Pop $0
    Pop $1
    ${If} $0 == 0
        Return
    ${EndIf}
    DetailPrint "Result: $0; $1"
    IfSilent closeAbort 0
    MessageBox MB_RETRYCANCEL|MB_ICONEXCLAMATION "$(RunningAppCloseFailed)$\r$\nResult: $0$\r$\n$1" IDRETRY closeRetry
    closeAbort:
    SetErrorLevel 68
    Call RollbackSetupTransaction
    Abort "$(RunningAppCloseFailed)"
FunctionEnd

Function StopCoreServiceForUpgrade
    SetDetailsPrint textonly
    DetailPrint "$(CoreServiceStopping)"
    SetDetailsPrint both
    ; Disable restart before requesting a stop. A previous Core can have
    ; recovery actions, and install-service restores Automatic start after
    ; the new executable has been written successfully.
    nsExec::ExecToStack /TIMEOUT=10000 '"$SYSDIR\sc.exe" query "${HYPOMUX_CORE_SERVICE}"'
    Pop $0
    Pop $1
    ${If} $0 == 1060
        Return
    ${EndIf}
    ${If} $0 != 0
        DetailPrint "Result: $0; $1"
        Call RollbackSetupTransaction
    Abort "$(CoreServiceStopFailed) $0"
    ${EndIf}
    nsExec::Exec '"$SYSDIR\sc.exe" config "${HYPOMUX_CORE_SERVICE}" start= disabled'
    Pop $0
    ${If} $0 != 0
        Call RollbackSetupTransaction
    Abort "$(CoreServiceStopFailed)"
    ${EndIf}

    ; sc.exe only submits the stop request. Waiting is kept in a separate
    ; bounded process so a deadlocked legacy service cannot freeze Setup.
    nsExec::Exec '"$SYSDIR\sc.exe" stop "${HYPOMUX_CORE_SERVICE}"'
    Pop $0
    nsExec::ExecToStack /TIMEOUT=30000 '"$SYSDIR\WindowsPowerShell\v1.0\powershell.exe" -NoProfile -NonInteractive -ExecutionPolicy Bypass -Command "try { (Get-Service -Name ${HYPOMUX_CORE_SERVICE} -ErrorAction Stop).WaitForStatus([System.ServiceProcess.ServiceControllerStatus]::Stopped, [TimeSpan]::FromSeconds(20)); exit 0 } catch { [Console]::Error.WriteLine($$_.Exception.Message); exit 2 }"'
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
    ; Killing the process is asynchronous with SCM state updates. Wait for a
    ; bounded stopped state instead of one fixed sleep and a CIM snapshot.
    nsExec::ExecToStack /TIMEOUT=15000 '"$SYSDIR\WindowsPowerShell\v1.0\powershell.exe" -NoProfile -NonInteractive -ExecutionPolicy Bypass -Command "try { (Get-Service -Name ${HYPOMUX_CORE_SERVICE} -ErrorAction Stop).WaitForStatus([System.ServiceProcess.ServiceControllerStatus]::Stopped, [TimeSpan]::FromSeconds(10)); exit 0 } catch { [Console]::Error.WriteLine($$_.Exception.Message); exit 2 }"'
    Pop $0
    Pop $1
    ${If} $0 != 0
        DetailPrint "$1"
        Call RollbackSetupTransaction
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
    IfErrors 0 +3
        Call RollbackSetupTransaction
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
    Call RollbackSetupTransaction
    Abort "$(InstallPathInspectFailed)"
FunctionEnd

Function StopCoreProcessesForUpgrade
    SetDetailsPrint textonly
    DetailPrint "$(CoreProcessStopping)"
    SetDetailsPrint both
    stopCoreProcessesRetry:

    ; If the user changed the directory on the installer page, the previous
    ; registered Core still has to be unlocked before its exact owned files
    ; can be removed after the new installation commits.
    ${If} $HypoMuxInstallPathChanged == "1"
        StrCpy $HypoMuxCoreCheckTarget "$HypoMuxPreviousInstallDir\bin\hypomux-engine.exe"
        Call CheckExistingCoreForUpgrade
        ${If} $0 != 0
            Goto stopCoreProcessesFailed
        ${EndIf}
    ${EndIf}

    StrCpy $HypoMuxCoreCheckTarget "$INSTDIR\bin\hypomux-engine.exe"
    Call CheckExistingCoreForUpgrade
    ${If} $0 != 0
        Goto stopCoreProcessesFailed
    ${EndIf}
    !if "${WAILS_INSTALL_SCOPE}" != "user"
        ; The service image is separate from the desktop copy. Check it before
        ; PrepareProtectedCoreDirectory attempts to remove the old payload.
        StrCpy $HypoMuxCoreCheckTarget "${HYPOMUX_PROTECTED_CORE_BIN}\hypomux-engine.exe"
        Call CheckExistingCoreForUpgrade
        ${If} $0 != 0
            Goto stopCoreProcessesFailed
        ${EndIf}
    !endif
    Return

    stopCoreProcessesFailed:
    Call LogCoreUpgradeCheckFailure
    IfSilent stopCoreProcessesAbort 0
    MessageBox MB_RETRYCANCEL|MB_ICONEXCLAMATION "$(CoreProcessStopFailed)$\r$\n$\r$\n$(CoreCheckDetails)" IDRETRY stopCoreProcessesRetry
    stopCoreProcessesAbort:
    SetErrorLevel 67
    Call RollbackSetupTransaction
    Abort "$(CoreProcessStopFailed)"
FunctionEnd

Function CheckExistingCoreForUpgrade
    ; A fresh or partially removed installation has no old file to unlock.
    ; Do this in NSIS before extracting or launching the PowerShell helper.
    ; Existing files retain the path-scoped process and write-access checks.
    StrCpy $0 "0"
    StrCpy $1 ""
    IfFileExists "$HypoMuxCoreCheckTarget" 0 coreUpgradeCheckDone
    InitPluginsDir
    SetOutPath "$PLUGINSDIR"
    File /oname=stop-core-for-upgrade.ps1 "stop-core-for-upgrade.ps1"
    nsExec::ExecToStack /TIMEOUT=30000 '"$SYSDIR\WindowsPowerShell\v1.0\powershell.exe" -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "$PLUGINSDIR\stop-core-for-upgrade.ps1" -EnginePath "$HypoMuxCoreCheckTarget"'
    Pop $0
    Pop $1
    coreUpgradeCheckDone:
FunctionEnd

Function LogCoreUpgradeCheckFailure
    ; Preserve the return value even when the child emitted no text or never
    ; started. The detail list alone is not a durable diagnostic record.
    Push $2
    ClearErrors
    GetTempFileName $HypoMuxCoreCheckLog "$TEMP"
    IfErrors coreCheckLogFailed
    FileOpen $2 "$HypoMuxCoreCheckLog" w
    IfErrors coreCheckLogFailed
    FileWriteWord $2 0xFEFF
    FileWriteUTF16LE $2 "HypoMux ${INFO_PRODUCTVERSION} Core update check$\r$\n"
    FileWriteUTF16LE $2 "Result: $0$\r$\nTarget: $HypoMuxCoreCheckTarget$\r$\n"
    FileWriteUTF16LE $2 "PowerShell: $SYSDIR\WindowsPowerShell\v1.0\powershell.exe$\r$\n"
    FileWriteUTF16LE $2 "Helper: $PLUGINSDIR\stop-core-for-upgrade.ps1$\r$\nOutput: $1$\r$\n"
    FileClose $2
    IfErrors coreCheckLogFailed
    Goto coreCheckLogDone
    coreCheckLogFailed:
    StrCpy $HypoMuxCoreCheckLog "$(CoreCheckLogUnavailable)"
    coreCheckLogDone:
    DetailPrint "$(CoreCheckDetails)"
    Pop $2
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
            Call RollbackSetupTransaction
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
            Call RollbackSetupTransaction
    Abort "$(WailsNetworkRecoverFailed)"
        ${EndIf}
        IfFileExists "$INSTDIR\${PRODUCT_EXECUTABLE}" 0 nestedWailsRecovery
            nsExec::ExecToStack '"$INSTDIR\${PRODUCT_EXECUTABLE}" --recover-network'
            Pop $0
            Pop $1
            ${If} $0 != 0
                DetailPrint "$1"
                Call RollbackSetupTransaction
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
                Call RollbackSetupTransaction
    Abort "$(WailsNetworkRecoverFailed)"
            ${EndIf}
            IfFileExists "$PROGRAMFILES64\HypoMux\HypoMux\${PRODUCT_EXECUTABLE}" 0 wailsRecoveryDone
                nsExec::ExecToStack '"$PROGRAMFILES64\HypoMux\HypoMux\${PRODUCT_EXECUTABLE}" --recover-network'
                Pop $0
                Pop $1
                ${If} $0 != 0
                    DetailPrint "$1"
                    Call RollbackSetupTransaction
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
            Call RollbackSetupTransaction
    Abort "$(WailsNetworkRecoverFailed)"
        ${EndIf}
        IfFileExists "$HypoMuxPreviousInstallDir\${PRODUCT_EXECUTABLE}" 0 previousWailsRecoveryDone
            nsExec::ExecToStack '"$HypoMuxPreviousInstallDir\${PRODUCT_EXECUTABLE}" --recover-network'
            Pop $0
            Pop $1
            ${If} $0 != 0
                DetailPrint "$1"
                Call RollbackSetupTransaction
    Abort "$(WailsNetworkRecoverFailed)"
            ${EndIf}
    previousWailsRecoveryDone:
FunctionEnd

Function RemovePreviousWailsInstallation
    ${If} $HypoMuxInstallPathChanged != "1"
        Return
    ${EndIf}
    ; Native cleanup validates links and directory identity before deleting
    ; exact old files. A cleanup failure cannot undo the committed new install.
    nsExec::ExecToStack /TIMEOUT=60000 '"$HypoMuxSetupHelper" --setup-transaction cleanup-previous --scope ${WAILS_INSTALL_SCOPE} --previous-dir "$HypoMuxPreviousInstallDir\." --install-dir "$INSTDIR\."'
    Pop $0
    Pop $1
    DetailPrint "$1"
FunctionEnd

Function RemoveLegacyInstallations
    ; Payload is committed first. Never run a legacy uninstaller over the new
    ; application, or recursively delete nested/custom installation folders.
    !if "${WAILS_INSTALL_SCOPE}" != "user"
        DeleteRegKey HKLM "${HYPOMUX_NESTED_UNINST_KEY}"
        DeleteRegKey HKLM "${HYPOMUX_LEGACY_INNO_KEY}"
    !else
        DeleteRegKey HKCU "${HYPOMUX_NESTED_UNINST_KEY}"
    !endif
FunctionEnd

!macro HypoMuxStagePayload PREFIX
Function ${PREFIX}StageSetupPayload
    InitPluginsDir
    SetOutPath "$PLUGINSDIR\payload"
    !insertmacro wails.files
    !if "${PREFIX}" == ""
    ; The uninstaller only needs the headless recovery entrypoint, not a
    ; duplicate archive of Core and its DLLs.
    File /oname=hypomux-engine.exe "..\..\..\bin\hypomux-engine.exe"
    File /oname=sing-box.exe "..\..\..\bin\sing-box.exe"
    File /oname=wintun.dll "..\..\..\bin\wintun.dll"
    File /oname=libcronet.dll "..\..\..\bin\libcronet.dll"
    !endif
    StrCpy $HypoMuxSetupHelper "$PLUGINSDIR\payload\${PRODUCT_EXECUTABLE}"
    !if "${WAILS_INSTALL_SCOPE}" == "user"
        StrCpy $HypoMuxSetupLog "$LOCALAPPDATA\HypoMux\SetupTransaction\installer-errors.log"
    !else
        StrCpy $HypoMuxSetupLog "$APPDATA\HypoMux\SetupTransaction\installer-errors.log"
    !endif
FunctionEnd
!macroend
!insertmacro HypoMuxStagePayload ""
!insertmacro HypoMuxStagePayload "un."

Function LogSetupFailure
    Push $2
    FileOpen $2 "$HypoMuxSetupLog" a
    IfErrors setupLogDone
    FileWriteUTF16LE $2 "Stage: $HypoMuxSetupOperation; result: $0$\r$\n$1$\r$\n"
    FileClose $2
    setupLogDone:
    Pop $2
FunctionEnd

Function RollbackSetupTransaction
    ${If} $HypoMuxSetupActive != "1"
        Return
    ${EndIf}
    Push $0
    Push $1
    Push $2
    GetErrorLevel $2
    Push $2
    Call LogSetupFailure
    ; Clear before invoking recovery to avoid recursive error callbacks.
    StrCpy $HypoMuxSetupActive "0"
    nsExec::ExecToStack /TIMEOUT=120000 '"$HypoMuxSetupHelper" --setup-transaction rollback --scope ${WAILS_INSTALL_SCOPE}'
    Pop $0
    Pop $1
    DetailPrint "$1"
    ${If} $0 != 0
        IfSilent rollbackReportDone
        MessageBox MB_OK|MB_ICONSTOP "Recovery incomplete ($0). Backups and report are retained in HypoMux\SetupTransaction under ProgramData (machine) or LocalAppData (user).$\r$\n$1"
    ${EndIf}
    rollbackReportDone:
    Pop $2
    SetErrorLevel $2
    Pop $2
    Pop $1
    Pop $0
FunctionEnd

Function RunSetupTransaction
    DetailPrint "Setup: $HypoMuxSetupOperation"
    nsExec::ExecToStack /TIMEOUT=180000 '"$HypoMuxSetupHelper" --setup-transaction $HypoMuxSetupOperation --scope ${WAILS_INSTALL_SCOPE} --install-dir "$INSTDIR\." --payload "$PLUGINSDIR\payload"'
    Pop $0
    Pop $1
    DetailPrint "$1"
    ${If} $0 != 0
        Call LogSetupFailure
        IfSilent setupFailureSilent
        MessageBox MB_OK|MB_ICONSTOP "Setup $HypoMuxSetupOperation failed ($0).$\r$\n$1"
        setupFailureSilent:
        Call RollbackSetupTransaction
        SetErrorLevel 70
        Abort
    ${EndIf}
FunctionEnd

Function .onInstFailed
    Call RollbackSetupTransaction
FunctionEnd

Function EnsureWebViewRuntime
    StrCpy $HypoMuxSetupOperation "webview-runtime"
    nsExec::ExecToStack '"$HypoMuxSetupHelper" --webview-check ${WAILS_INSTALL_SCOPE}'
    Pop $0
    Pop $1
    ${If} $0 == 0
        Return
    ${EndIf}
    SetOutPath "$PLUGINSDIR\webview2bootstrapper"
    File "MicrosoftEdgeWebview2Setup.exe"
    DetailPrint "$(WailsWebViewInstall)"
    ClearErrors
    ExecWait '"$PLUGINSDIR\webview2bootstrapper\MicrosoftEdgeWebview2Setup.exe" /silent /install' $0
    IfErrors webviewFailed
    DetailPrint "WebView2 bootstrapper exit code: $0"
    ${If} $0 != 0
    ${AndIf} $0 != 3010
        Goto webviewFailed
    ${EndIf}
    nsExec::ExecToStack '"$HypoMuxSetupHelper" --webview-check ${WAILS_INSTALL_SCOPE}'
    Pop $0
    Pop $1
    ${If} $0 == 0
        Return
    ${EndIf}
    webviewFailed:
    Call LogSetupFailure
    IfSilent webviewFailedSilent
    MessageBox MB_OK|MB_ICONSTOP "Microsoft Edge WebView2 Runtime installation failed ($0). Please install the runtime and retry.$\r$\nMicrosoft Edge WebView2 运行时安装未完成，请安装运行时后重试。"
    webviewFailedSilent:
    SetErrorLevel 71
    Abort
FunctionEnd

!macro HypoMuxWriteRegistration
    WriteUninstaller "$INSTDIR\uninstall.exe"
    IfErrors registrationFailed
    WriteRegStr SHELL_CONTEXT "${UNINST_KEY}" "Publisher" "${INFO_COMPANYNAME}"
    WriteRegStr SHELL_CONTEXT "${UNINST_KEY}" "DisplayName" "${INFO_PRODUCTNAME}"
    WriteRegStr SHELL_CONTEXT "${UNINST_KEY}" "DisplayVersion" "${INFO_PRODUCTVERSION}"
    WriteRegStr SHELL_CONTEXT "${UNINST_KEY}" "DisplayIcon" "$INSTDIR\${PRODUCT_EXECUTABLE}"
    WriteRegStr SHELL_CONTEXT "${UNINST_KEY}" "UninstallString" '$\"$INSTDIR\uninstall.exe$\"'
    WriteRegStr SHELL_CONTEXT "${UNINST_KEY}" "QuietUninstallString" '$\"$INSTDIR\uninstall.exe$\" /S'
    IfErrors registrationFailed
    ${GetSize} "$INSTDIR" "/S=0K" $0 $1 $2
    IfErrors registrationFailed
    WriteRegDWORD SHELL_CONTEXT "${UNINST_KEY}" "EstimatedSize" $0
    IfErrors registrationFailed
!macroend

Section
    !insertmacro wails.setShellContext
    Call DetermineInstallPathChange
    Call EnsureWebViewRuntime
    ; Snapshot files, registrations and service configuration BEFORE stopping
    ; anything. The helper stages and hashes every payload before mutation.
    StrCpy $HypoMuxSetupOperation "begin"
    Call RunSetupTransaction
    StrCpy $HypoMuxSetupActive "1"
    Call CloseRunningHypoMux
    !if "${WAILS_INSTALL_SCOPE}" != "user"
        Call StopCoreServiceForUpgrade
    !endif
    Call RecoverPreviousWailsInstallation
    Call RecoverLegacyV22Network
    Call RecoverWailsInstallations
    Call StopCoreProcessesForUpgrade

    StrCpy $HypoMuxSetupOperation "apply"
    Call RunSetupTransaction
    SetOutPath $INSTDIR
    !if "${WAILS_INSTALL_SCOPE}" != "user"
        StrCpy $HypoMuxSetupOperation "install-service"
        DetailPrint "$(CoreServiceInstalling)"
        nsExec::ExecToStack /TIMEOUT=60000 '"${HYPOMUX_PROTECTED_CORE_BIN}\hypomux-engine.exe" install-service --desktop "$INSTDIR\${PRODUCT_EXECUTABLE}"'
        Pop $0
        Pop $1
        ${If} $0 != 0
            DetailPrint "$1"
            Call RollbackSetupTransaction
            SetErrorLevel 72
            Abort "$(CoreServiceInstallFailed)"
        ${EndIf}
        ; Starting the service is insufficient: verify the authenticated IPC
        ; handshake and health before committing the new installation.
        StrCpy $HypoMuxSetupOperation "health-check"
        nsExec::ExecToStack /TIMEOUT=45000 '"$INSTDIR\${PRODUCT_EXECUTABLE}" --core-service-self-test'
        Pop $0
        Pop $1
        ${If} $0 != 0
            DetailPrint "$1"
            Call RollbackSetupTransaction
            SetErrorLevel 73
            Abort "Core health check failed. See HypoMux\SetupTransaction\setup.log."
        ${EndIf}
        DetailPrint "$(CoreServiceInstalled)"
    !endif

    ClearErrors
    CreateShortcut "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"
    CreateShortCut "$DESKTOP\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"
    IfErrors registrationFailed
    !insertmacro wails.associateFiles
    !insertmacro wails.associateCustomProtocols
    !insertmacro HypoMuxWriteRegistration
    !if "${WAILS_INSTALL_SCOPE}" == "user"
        WriteRegStr HKCU "${UNINST_KEY}" "InstallLocation" "$INSTDIR"
    !else
        WriteRegStr HKLM "${UNINST_KEY}" "InstallLocation" "$INSTDIR"
    !endif
    IfErrors registrationFailed
    StrCpy $HypoMuxSetupOperation "commit"
    Call RunSetupTransaction
    StrCpy $HypoMuxSetupActive "0"
    Call RemoveLegacyInstallations
    Call RemovePreviousWailsInstallation
    Call RemoveLegacyAutostartTask
    Call RestoreAutostart
    Goto installationDone
    registrationFailed:
    Call RollbackSetupTransaction
    SetErrorLevel 74
    Abort "Could not register HypoMux."
    installationDone:
    SetErrorLevel 0
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
