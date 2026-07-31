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

!insertmacro MUI_LANGUAGE "English" # Set the Language of the installer

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
   !insertmacro wails.checkArchitecture
FunctionEnd

Section
    !insertmacro wails.setShellContext

    !insertmacro wails.webview2runtime

    ; Close the previous UI and stop Core before replacing binaries. If a prior
    ; session ended abruptly, invoke both narrow recovery entry points while
    ; the old executables are still present.
    nsExec::Exec '"$SYSDIR\taskkill.exe" /IM "${PRODUCT_EXECUTABLE}" /T /F'
    Pop $0
    !if "${WAILS_INSTALL_SCOPE}" != "user"
        nsExec::Exec '"$SYSDIR\sc.exe" stop "${HYPOMUX_CORE_SERVICE}"'
        Pop $0
    !endif
    Sleep 1200
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
        nsExec::ExecToLog '"$INSTDIR\bin\hypomux-engine.exe" install-service'
        Pop $0
        ${If} $0 != 0
            Abort "Failed to install HypoMux Core Service (exit code $0)."
        ${EndIf}
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
            nsExec::ExecToLog '"$INSTDIR\bin\hypomux-engine.exe" remove-service'
            Pop $0
            ${If} $0 != 0
                Abort "Failed to remove HypoMux Core Service (exit code $0)."
            ${EndIf}
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
