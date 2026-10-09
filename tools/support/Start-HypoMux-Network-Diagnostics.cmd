@echo off
setlocal
title HypoMux Network Diagnostics
echo Keep HypoMux TUN ON and the problem reproducible.
echo Collecting diagnostics. Please wait; do not close this window.
echo No network settings will be changed.
"%SystemRoot%\System32\WindowsPowerShell\v1.0\powershell.exe" -NoProfile -ExecutionPolicy Bypass -File "%~dp0Collect-HypoMuxNetworkDiagnostics.ps1"
set "RESULT=%ERRORLEVEL%"
echo.
if not "%RESULT%"=="0" echo Collection was incomplete. See the message above.
pause
exit /b %RESULT%
