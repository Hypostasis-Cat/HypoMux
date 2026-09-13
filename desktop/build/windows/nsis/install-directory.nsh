; InstallDirRegKey reads the default (32-bit) view before .onInit and ignores
; SetRegView. Our uninstaller writes InstallLocation in the 64-bit view.
; .onInit reads that same view into HypoMuxPreviousInstallDir before this call.
Function HypoMuxInitializeInstallDir
    ; With InstallDir empty and no InstallDirRegKey, only /D= can prefill this.
    ; Run once in .onInit so returning to the directory page keeps user edits.
    ${If} $INSTDIR != ""
        Return
    ${EndIf}
    ${If} $HypoMuxPreviousInstallDir != ""
        StrCpy $INSTDIR "$HypoMuxPreviousInstallDir"
        Return
    ${EndIf}
    !if "${WAILS_INSTALL_SCOPE}" == "user"
        StrCpy $INSTDIR "$LOCALAPPDATA\Programs\${INFO_PRODUCTNAME}"
    !else
        StrCpy $INSTDIR "$PROGRAMFILES64\${INFO_PRODUCTNAME}"
    !endif
FunctionEnd
