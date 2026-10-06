#requires -Version 5.1
[CmdletBinding()]
param(
    [string]$InstallDir,
    [string]$ReportDir = (Join-Path $env:TEMP ('HypoMux-Install-Diagnostics-' + [guid]::NewGuid().ToString('N'))),
    [switch]$NoPrompt
)

# This collector never runs HypoMux, stops processes, changes services/ACLs,
# or modifies existing application files. Only its own report files are written.
$ErrorActionPreference = 'Stop'
[IO.Directory]::CreateDirectory($ReportDir) | Out-Null
$ReportDir = [IO.Path]::GetFullPath($ReportDir)
$report = [IO.Path]::Combine($ReportDir, 'report.log')
$utf8 = [Text.UTF8Encoding]::new($true)
function Log([string]$Message) {
    [IO.File]::AppendAllText($report, $Message + [Environment]::NewLine, $utf8)
    Write-Host $Message
}
function Inspect([string]$SectionName, [scriptblock]$Action) {
    Log "`r`n=== $SectionName ==="
    try { & $Action } catch { Log ("FAILED: " + $_.Exception.ToString() + "`r`n" + $_.ScriptStackTrace) }
}

Log 'HypoMux installation diagnostics v2 (read-only; no repair)'
Log ('Time: ' + [DateTimeOffset]::Now.ToString('o'))
Log ('OS: ' + [Environment]::OSVersion.VersionString)
Log ('PowerShell: ' + $PSVersionTable.PSVersion + '; 64-bit process: ' + [Environment]::Is64BitProcess)
Log ('LanguageMode: ' + $ExecutionContext.SessionState.LanguageMode)
Log ('User: ' + [Security.Principal.WindowsIdentity]::GetCurrent().Name)
Log ('Elevated: ' + ([Security.Principal.WindowsPrincipal]::new([Security.Principal.WindowsIdentity]::GetCurrent())).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator))
Log ('TEMP: ' + $env:TEMP)
Log ('Inherited PSModulePath: ' + $env:PSModulePath)
$originalModules = $env:PSModulePath
if ($env:HYPOMUX_DIAG_ORIGINAL_PSModulePath) { $originalModules = $env:HYPOMUX_DIAG_ORIGINAL_PSModulePath }
Log ('Launcher original PSModulePath: ' + $originalModules)

if (-not $InstallDir -and -not $NoPrompt) {
    Write-Host 'Optional: enter the FULL install folder chosen in Setup, or press Enter to skip.'
    $InstallDir = (Read-Host 'Install folder (not a menu number)').Trim().Trim('"')
}
if ($InstallDir -and $InstallDir -notmatch '^[A-Za-z]:\\') {
    Log ('Ignoring invalid install folder (must be an absolute drive path): ' + $InstallDir)
    $InstallDir = ''
}

$roots = [Collections.Generic.List[string]]::new()
if ($InstallDir) { $roots.Add($InstallDir) }
foreach ($base in @($env:ProgramW6432, $env:ProgramFiles, ${env:ProgramFiles(x86)})) {
    if ($base) { $roots.Add([IO.Path]::Combine($base, 'HypoMux')) }
}
if ($env:LOCALAPPDATA) { $roots.Add([IO.Path]::Combine($env:LOCALAPPDATA, 'Programs\HypoMux')) }

Inspect 'Installation registrations (32-bit and 64-bit views)' {
    foreach ($hive in @([Microsoft.Win32.RegistryHive]::LocalMachine, [Microsoft.Win32.RegistryHive]::CurrentUser)) {
        foreach ($view in @([Microsoft.Win32.RegistryView]::Registry64, [Microsoft.Win32.RegistryView]::Registry32)) {
            $baseKey = [Microsoft.Win32.RegistryKey]::OpenBaseKey($hive, $view)
            try {
                foreach ($name in @('HypoMux', 'HypoMuxHypoMux', '{7637d353-b9c0-4145-bc81-7a474e534d07}_is1')) {
                    $key = $baseKey.OpenSubKey('SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\' + $name)
                    if (-not $key) { continue }
                    try {
                        $location = [string]$key.GetValue('InstallLocation')
                        Log ("$hive/$view/$name : Version=" + $key.GetValue('DisplayVersion') + '; InstallLocation=' + $location)
                        if ($location -match '^[A-Za-z]:\\') { $roots.Add($location) }
                    } finally { $key.Dispose() }
                }
            } finally { $baseKey.Dispose() }
        }
    }
    Log 'Registration scan complete; missing registrations are normal on a fresh install.'
}

Inspect 'HypoMux Core service' {
    & "$env:SystemRoot\System32\sc.exe" query HypoMuxCore | ForEach-Object { Log ([string]$_) }
    Log ('sc query exit: ' + $LASTEXITCODE)
    & "$env:SystemRoot\System32\sc.exe" qc HypoMuxCore | ForEach-Object { Log ([string]$_) }
    Log ('sc qc exit: ' + $LASTEXITCODE)
}
Inspect 'HypoMux process paths (no processes are stopped)' {
    $found = @(Get-Process -Name hypomux,hypomux-engine -ErrorAction SilentlyContinue)
    Log ('Process count: ' + $found.Count)
    foreach ($p in $found) {
        try {
            $processPath = $p.Path
            if (-not $processPath) { $processPath = '(unavailable with current permissions)' }
            Log ("PID=$($p.Id); Name=$($p.ProcessName); Path=$processPath")
        }
        catch { Log ("PID=$($p.Id); path unavailable: " + $_.Exception.Message) }
    }
}

$files = [Collections.Generic.List[string]]::new()
foreach ($root in $roots) {
    $files.Add([IO.Path]::Combine($root, 'hypomux.exe'))
    $files.Add([IO.Path]::Combine($root, 'bin\hypomux-engine.exe'))
}
$files.Add([IO.Path]::Combine([Environment]::GetFolderPath('CommonApplicationData'), 'HypoMux\Core\bin\hypomux-engine.exe'))
foreach ($path in ($files | Sort-Object -Unique)) {
    Inspect ("File: $path") {
        # Unlike File.Exists, GetAttributes preserves access-denied errors.
        try { $attributes = [IO.File]::GetAttributes($path) }
        catch [IO.FileNotFoundException] { Log 'Absent (normal on a fresh install).'; return }
        catch [IO.DirectoryNotFoundException] { Log 'Absent (normal on a fresh install).'; return }
        Log ('Attributes: ' + $attributes)
        $item = Get-Item -LiteralPath $path -Force
        Log ('Length: ' + $item.Length + '; Version: ' + $item.VersionInfo.FileVersion)
        Log ('ACL: ' + (Get-Acl -LiteralPath $path).Sddl)
    }
}

# Harmless extracted-script probe: exercise the PowerShell launch and module
# loading used by NSIS, without executing the production process-killing helper.
$probe = @'
$ErrorActionPreference = 'Stop'
try {
    Write-Output ('PowerShell=' + $PSVersionTable.PSVersion + '; 64-bit=' + [Environment]::Is64BitProcess)
    Write-Output ('LanguageMode=' + $ExecutionContext.SessionState.LanguageMode)
    Write-Output ('PSModulePath=' + $env:PSModulePath)
    Get-Command Get-Process, Where-Object, Stop-Process, Start-Sleep -ErrorAction Stop | ForEach-Object {
        Write-Output ('Command=' + $_.Name + '; Module=' + $_.ModuleName)
    }
    $owned = @(Get-Process -Name hypomux-engine -ErrorAction SilentlyContinue | Where-Object { $false })
    Write-Output ('Process enumeration completed; filtered count=' + $owned.Count)
    Write-Output 'PROBE_OK (no processes stopped or application files written)'
    exit 0
} catch {
    Write-Output ($_ | Out-String)
    exit 1
}
'@
$probePath = [IO.Path]::Combine($ReportDir, 'powershell-probe.ps1')
[IO.File]::WriteAllText($probePath, $probe, $utf8)
foreach ($arch in @('System32', 'SysWOW64')) {
    foreach ($cleanModules in @($false, $true)) {
        $label = $arch + $(if ($cleanModules) { '-clean-modules' } else { '-inherited-modules' })
        Inspect ("Child PowerShell: $label") {
            $hostPath = [IO.Path]::Combine($env:SystemRoot, "$arch\WindowsPowerShell\v1.0\powershell.exe")
            if (-not [IO.File]::Exists($hostPath)) { Log 'Host not present.'; return }
            $info = [Diagnostics.ProcessStartInfo]::new()
            $info.FileName = $hostPath
            $info.Arguments = '-NoProfile -NonInteractive -ExecutionPolicy Bypass -File "' + $probePath + '"'
            $info.UseShellExecute = $false
            $info.CreateNoWindow = $true
            $info.RedirectStandardOutput = $true
            $info.RedirectStandardError = $true
            $child = [Diagnostics.Process]::new()
            $child.StartInfo = $info
            try {
                # Scope the environment change to this collector process only.
                # Avoid ProcessStartInfo.EnvironmentVariables: .NET Framework
                # can reject inherited blocks containing both PATH and Path.
                $savedModules = [Environment]::GetEnvironmentVariable('PSModulePath', 'Process')
                try {
                    $probeModules = if ($cleanModules) { $null } else { $originalModules }
                    [Environment]::SetEnvironmentVariable('PSModulePath', $probeModules, 'Process')
                    if (-not $child.Start()) { throw 'Process.Start returned false' }
                } finally {
                    [Environment]::SetEnvironmentVariable('PSModulePath', $savedModules, 'Process')
                }
                $stdout = $child.StandardOutput.ReadToEndAsync()
                $stderr = $child.StandardError.ReadToEndAsync()
                if (-not $child.WaitForExit(20000)) {
                    # Terminate only the exact diagnostic child created above.
                    $child.Kill()
                    $child.WaitForExit()
                    Log 'TIMEOUT after 20 seconds; diagnostic child terminated.'
                }
                $outText = $stdout.Result
                $errText = $stderr.Result
                [IO.File]::WriteAllText([IO.Path]::Combine($ReportDir, "$label.stdout.log"), $outText, $utf8)
                [IO.File]::WriteAllText([IO.Path]::Combine($ReportDir, "$label.stderr.log"), $errText, $utf8)
                Log ('Exit code: ' + $child.ExitCode)
                Log ('STDOUT: ' + $outText)
                Log ('STDERR: ' + $errText)
            } finally { $child.Dispose() }
        }
    }
}
# Read only the bounded diagnostic text, never payloads or rollback backups.
foreach ($base in @([Environment]::GetFolderPath('CommonApplicationData'), [Environment]::GetFolderPath('LocalApplicationData'))) {
    foreach ($name in @('setup.log', 'installer-errors.log', 'journal.json')) {
        $path = [IO.Path]::Combine($base, 'HypoMux\SetupTransaction', $name)
        Inspect ("Setup transaction: $path") {
            try { $info = Get-Item -LiteralPath $path -Force -ErrorAction Stop }
            catch [System.Management.Automation.ItemNotFoundException] { Log 'No transaction report.'; return }
            if ($info.Length -gt 4MB) { Log 'Report exceeds 4 MiB; collect this file separately.'; return }
            $encoding = if ($name -eq 'installer-errors.log') { [Text.Encoding]::Unicode } else { $utf8 }
            Log ([IO.File]::ReadAllText($path, $encoding))
        }
    }
}
Log "`r`nCollection complete. Send the entire report folder: $ReportDir"
Log 'This is diagnostic evidence only; passing probes do not prove Setup will succeed.'
