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

function Test-TargetUnlocked {
    if (-not [System.IO.File]::Exists($targetPath)) {
        return $true
    }

    try {
        $stream = [System.IO.File]::Open(
            $targetPath,
            [System.IO.FileMode]::Open,
            [System.IO.FileAccess]::Read,
            [System.IO.FileShare]::None
        )
        $stream.Dispose()
        return $true
    }
    catch {
        return $false
    }
}

do {
    foreach ($process in @(Get-OwnedCoreProcesses)) {
        Stop-Process -Id $process.Id -Force -ErrorAction SilentlyContinue
    }

    if (@(Get-OwnedCoreProcesses).Count -eq 0 -and (Test-TargetUnlocked)) {
        exit 0
    }

    Start-Sleep -Milliseconds 250
} while ([DateTime]::UtcNow -lt $deadline)

Write-Error "Timed out while unlocking the previous HypoMux Core executable: $targetPath"
exit 1
