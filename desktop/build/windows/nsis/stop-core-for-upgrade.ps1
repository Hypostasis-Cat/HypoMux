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
$deadline = [DateTime]::UtcNow.AddSeconds($TimeoutSeconds)
$lastStopFailure = ''
$lastReplaceFailure = ''

function Get-OwnedCoreProcesses {
    @(
        Get-Process -Name 'hypomux-engine' -ErrorAction SilentlyContinue |
            Where-Object {
                try {
                    $_.Path -and
                        [System.IO.Path]::GetFullPath($_.Path).Equals(
                            $targetPath,
                            [System.StringComparison]::OrdinalIgnoreCase
                        )
                }
                catch {
                    $false
                }
            }
    )
}

function Test-TargetReplaceable {
    try {
        # File.Exists also returns false on access errors. Only genuinely
        # absent files may bypass the replacement check.
        $attributes = [System.IO.File]::GetAttributes($targetPath)
        if (($attributes -band [System.IO.FileAttributes]::ReparsePoint) -ne 0) {
            throw "Refusing to modify a linked Core executable: $targetPath"
        }
        # ReadOnly is not a process lock and rebooting cannot clear it.
        # Preserve every other attribute and never loosen the file's ACL.
        if (($attributes -band [System.IO.FileAttributes]::ReadOnly) -ne 0) {
            [System.IO.File]::SetAttributes(
                $targetPath,
                ($attributes -band (-bnot [System.IO.FileAttributes]::ReadOnly))
            )
        }
        # Match the access needed by the installer without demanding that every
        # harmless reader (for example an antivirus scanner) close its handle.
        # Existing handles must still permit writes, so a running or genuinely
        # write-locked Core remains blocked.
        $shareMode = [System.IO.FileShare]::Read -bor [System.IO.FileShare]::Delete
        $stream = [System.IO.File]::Open(
            $targetPath,
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
        $script:lastReplaceFailure = '{0} (HRESULT=0x{1:X8})' -f $cause.Message, $cause.HResult
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

    if (@(Get-OwnedCoreProcesses).Count -eq 0 -and (Test-TargetReplaceable)) {
        exit 0
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

Write-Output "Core executable cannot be replaced: $targetPath"
if ($lastReplaceFailure) {
    Write-Output "Write probe failed: $lastReplaceFailure"
}
exit 11
