@echo off
setlocal
set "REPORT=%TEMP%\HypoMux-Install-Diagnostics-%RANDOM%-%RANDOM%"
mkdir "%REPORT%"
if errorlevel 1 goto failed
ver > "%REPORT%\launcher.log" 2>&1
>> "%REPORT%\launcher.log" echo SystemRoot=%SystemRoot%
>> "%REPORT%\launcher.log" echo TEMP=%TEMP%
set "HYPOMUX_DIAG_ORIGINAL_PSModulePath=%PSModulePath%"
set "PSModulePath="
"%SystemRoot%\System32\WindowsPowerShell\v1.0\powershell.exe" -NoProfile -ExecutionPolicy Bypass -File "%~dp0Collect-HypoMuxInstallDiagnostics.ps1" -ReportDir "%REPORT%" -NoPrompt > "%REPORT%\startup.log" 2>&1
set "RESULT=%ERRORLEVEL%"
>> "%REPORT%\launcher.log" echo PowerShellExit=%RESULT%
echo.
echo Diagnostic process exit: %RESULT%
echo Report folder: "%REPORT%"
echo Send the ENTIRE folder, including launcher.log and startup.log.
echo Logs may contain your Windows username and installation paths.
pause
exit /b %RESULT%
:failed
echo Cannot create report folder: "%REPORT%"
pause
exit /b 1
