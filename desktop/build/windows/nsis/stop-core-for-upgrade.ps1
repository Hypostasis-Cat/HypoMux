[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [ValidateNotNullOrEmpty()]
    [string]$EnginePath,

    [ValidateRange(1, 60)]
    [int]$TimeoutSeconds = 20
)

$ErrorActionPreference = 'Stop'
$targetPath = [System.IO.Path]::GetFullPath($EnginePath)
$coreDirectory = [System.IO.Path]::GetDirectoryName($targetPath)
$processPaths = @($targetPath, (Join-Path $coreDirectory 'sing-box.exe'))
$payloadPaths = @($processPaths) + @((Join-Path $coreDirectory 'wintun.dll'))
$deadline = [DateTime]::UtcNow.AddSeconds($TimeoutSeconds)
$lastStopFailure = ''
$lastReplaceFailure = ''

function Get-OwnedCoreProcesses {
    @(
        Get-Process -Name 'hypomux-engine','sing-box' -ErrorAction SilentlyContinue |
            Where-Object {
                try {
                    if (-not $_.Path) { return $false }
                    foreach ($ownedPath in $processPaths) {
                        if ([System.IO.Path]::GetFullPath($_.Path).Equals(
                            $ownedPath,
                            [System.StringComparison]::OrdinalIgnoreCase
                        )) { return $true }
                    }
                    $false
                }
                catch {
                    $false
                }
            }
    )
}

function Test-TargetReplaceable {
    param([string]$Path)
    try {
        # File.Exists also returns false on access errors. Only genuinely
        # absent files may bypass the replacement check.
        $attributes = [System.IO.File]::GetAttributes($Path)
        if (($attributes -band [System.IO.FileAttributes]::ReparsePoint) -ne 0) {
            throw "Refusing to modify a linked Core payload: $Path"
        }
        # ReadOnly is not a process lock and rebooting cannot clear it.
        # Preserve every other attribute and never loosen the file's ACL.
        if (($attributes -band [System.IO.FileAttributes]::ReadOnly) -ne 0) {
            [System.IO.File]::SetAttributes(
                $Path,
                ($attributes -band (-bnot [System.IO.FileAttributes]::ReadOnly))
            )
        }
        # Match the access needed by the installer without demanding that every
        # harmless reader (for example an antivirus scanner) close its handle.
        # Existing handles must still permit writes, so a running or genuinely
        # write-locked Core remains blocked.
        $shareMode = [System.IO.FileShare]::Read -bor [System.IO.FileShare]::Delete
        $stream = [System.IO.File]::Open(
            $Path,
            [System.IO.FileMode]::Open,
            [System.IO.FileAccess]::Write,
            $shareMode
        )
        $stream.Dispose()
        $script:lastReplaceFailure = ''
        return $true
    }
    catch [System.IO.FileNotFoundException] {
        return $true
    }
    catch [System.IO.DirectoryNotFoundException] {
        return $true
    }
    catch {
        $cause = $_.Exception.GetBaseException()
        $script:lastReplaceFailure = '{0}: {1} (HRESULT=0x{2:X8})' -f $Path, $cause.Message, $cause.HResult
        return $false
    }
}

do {
    foreach ($process in @(Get-OwnedCoreProcesses)) {
        try {
            Stop-Process -Id $process.Id -Force -ErrorAction Stop
            $lastStopFailure = ''
        }
        catch {
            $lastStopFailure = $_.Exception.Message
        }
    }

    if (@(Get-OwnedCoreProcesses).Count -eq 0) {
        # An old service can stop before its sing-box child exits. Check every
        # payload in this exact Core directory before NSIS replaces any files.
        $allReplaceable = $true
        foreach ($path in $payloadPaths) {
            if (-not (Test-TargetReplaceable $path)) {
                $allReplaceable = $false
                break
            }
        }
        if ($allReplaceable) { exit 0 }
    }

    Start-Sleep -Milliseconds 250
} while ([DateTime]::UtcNow -lt $deadline)

$remaining = @(Get-OwnedCoreProcesses)
if ($remaining.Count -gt 0) {
    $processIds = ($remaining | ForEach-Object Id) -join ', '
    Write-Output "Core process is still running (PID: $processIds)."
    if ($lastStopFailure) {
        Write-Output "Stop failed: $lastStopFailure"
    }
    exit 10
}

Write-Output "Core payload cannot be replaced in: $coreDirectory"
if ($lastReplaceFailure) {
    Write-Output "Write probe failed: $lastReplaceFailure"
}
exit 11
