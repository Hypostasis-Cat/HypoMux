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
## !define INFO_PRODUCTVERSION "1.0.0"     # Default "2.1.0"
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
!include "wails_tools.nsh"

!define HYPOMUX_CORE_SERVICE "HypoMuxCore"

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
LangString CoreServiceStopFailed ${LANG_ENGLISH} "Could not stop HypoMux Core Service. Setup cannot safely replace the application files."
LangString CoreServiceStopFailed ${LANG_SIMPCHINESE} "无法停止 HypoMux Core 服务，安装程序不能安全替换应用文件。"

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
!else
    InstallDir "$PROGRAMFILES64\${INFO_COMPANYNAME}\${INFO_PRODUCTNAME}"
!endif
ShowInstDetails show # This will always show the installation details.

Function .onInit
   !insertmacro MUI_LANGDLL_DISPLAY
   !insertmacro wails.checkArchitecture
FunctionEnd

Function un.onInit
   !insertmacro MUI_UNGETLANGUAGE
FunctionEnd

Function CloseRunningHypoMux
closeRetry:
    DetailPrint "$(RunningAppClosing)"
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
    DetailPrint "$(CoreServiceStopping)"
    nsExec::ExecToStack '"$SYSDIR\WindowsPowerShell\v1.0\powershell.exe" -NoProfile -NonInteractive -ExecutionPolicy Bypass -Command "if (Get-Service -Name ${HYPOMUX_CORE_SERVICE} -ErrorAction SilentlyContinue) { Stop-Service -Name ${HYPOMUX_CORE_SERVICE} -Force -ErrorAction Stop; (Get-Service -Name ${HYPOMUX_CORE_SERVICE}).WaitForStatus([System.ServiceProcess.ServiceControllerStatus]::Stopped, [TimeSpan]::FromSeconds(20)) }"'
    Pop $0
    Pop $1
    ${If} $0 != 0
        DetailPrint "$1"
        Abort "$(CoreServiceStopFailed)"
    ${EndIf}
FunctionEnd

Section
    !insertmacro wails.setShellContext

    !insertmacro wails.webview2runtime

    ; Close the previous UI and stop Core before replacing binaries. If a prior
    ; session ended abruptly, invoke both narrow recovery entry points while
    ; the old executables are still present.
    Call CloseRunningHypoMux
    !if "${WAILS_INSTALL_SCOPE}" != "user"
        Call StopCoreServiceForUpgrade
    !endif
    IfFileExists "$INSTDIR\bin\hypomux-engine.exe" 0 +2
        nsExec::ExecToLog '"$INSTDIR\bin\hypomux-engine.exe" recover'
    IfFileExists "$INSTDIR\${PRODUCT_EXECUTABLE}" 0 +2
        nsExec::ExecToLog '"$INSTDIR\${PRODUCT_EXECUTABLE}" --recover-network'

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

    ; Machine installation elevates once and installs the isolated privileged
    ; Core. The Wails/WebView2 executable remains asInvoker.
    !if "${WAILS_INSTALL_SCOPE}" != "user"
        DetailPrint "$(CoreServiceInstalling)"
        nsExec::ExecToStack '"$INSTDIR\bin\hypomux-engine.exe" install-service'
        Pop $0
        Pop $1
        ${If} $0 != 0
            DetailPrint "$1"
            Abort "$(CoreServiceInstallFailed) $0"
        ${EndIf}
        DetailPrint "$(CoreServiceInstalled)"
    !endif

    CreateShortcut "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"
    CreateShortCut "$DESKTOP\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"

    !insertmacro wails.associateFiles
    !insertmacro wails.associateCustomProtocols

    !insertmacro wails.writeUninstaller
SectionEnd

Section "uninstall"
    !insertmacro wails.setShellContext

    ; Stop the ordinary-permission UI first, then recover the current user's
    ; proxy snapshot and the machine-owned TUN state before files disappear.
    nsExec::Exec '"$SYSDIR\taskkill.exe" /IM "${PRODUCT_EXECUTABLE}" /T /F'
    Pop $0
    !if "${WAILS_INSTALL_SCOPE}" != "user"
        IfFileExists "$INSTDIR\bin\hypomux-engine.exe" 0 serviceRemoved
            DetailPrint "$(CoreServiceRemoving)"
            nsExec::ExecToStack '"$INSTDIR\bin\hypomux-engine.exe" remove-service'
            Pop $0
            Pop $1
            ${If} $0 != 0
                DetailPrint "$1"
                Abort "$(CoreServiceRemoveFailed) $0"
            ${EndIf}
            DetailPrint "$(CoreServiceRemoved)"
        serviceRemoved:
    !endif
    IfFileExists "$INSTDIR\bin\hypomux-engine.exe" 0 +2
        nsExec::ExecToLog '"$INSTDIR\bin\hypomux-engine.exe" recover'
    IfFileExists "$INSTDIR\${PRODUCT_EXECUTABLE}" 0 +2
        nsExec::ExecToLog '"$INSTDIR\${PRODUCT_EXECUTABLE}" --recover-network'

    ; Device-local settings under %AppData%\HypoMux are deliberately retained
    ; for reinstall/rollback. Autostart, WebView data and installed files are
    ; application-owned and are removed.
    DeleteRegValue HKCU "Software\Microsoft\Windows\CurrentVersion\Run" "HypoMux"

    RMDir /r "$AppData\${PRODUCT_EXECUTABLE}" # Remove the WebView2 DataPath

    Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk"
    Delete "$DESKTOP\${INFO_PRODUCTNAME}.lnk"

    !insertmacro wails.unassociateFiles
    !insertmacro wails.unassociateCustomProtocols

    !insertmacro wails.deleteUninstaller

    RMDir /r $INSTDIR
SectionEnd
