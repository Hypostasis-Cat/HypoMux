# Computes the SHA-256 of the Core binaries staged by the installer while
# makensis compiles. project.nsi embeds the resulting digests into the signed
# installer and passes them to the elevated setup helper, which refuses to
# apply a payload whose bytes no longer match (desktop/internal/setup/
# payload_windows.go). Files staged under the user's temp directory cannot be
# trusted by an elevated process without such an independent reference.
#
# The digest is taken through .NET explicitly: makensis invokes this script
# through the command processor, where Windows PowerShell may run without its
# utility modules (Get-FileHash) importable, but the crypto types are always
# loaded from the framework.
param(
    [Parameter(Mandatory = $true)][string]$BinDir,
    [Parameter(Mandatory = $true)][string]$OutFile
)

$ErrorActionPreference = "Stop"

function Get-Sha256Hex {
    param([string]$Path)
    $stream = [System.IO.File]::OpenRead($Path)
    try {
        $sha = [System.Security.Cryptography.SHA256]::Create()
        try {
            $hashBytes = $sha.ComputeHash($stream)
        } finally {
            $sha.Dispose()
        }
    } finally {
        $stream.Dispose()
    }
    return ($hashBytes | ForEach-Object { $_.ToString("x2") }) -join ""
}

$names = @("hypomux-engine.exe", "sing-box.exe", "wintun.dll")
$digests = @()
foreach ($name in $names) {
    $path = Join-Path $BinDir $name
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
        Write-Error "payload binary missing: $path"
        exit 1
    }
    $digests += "$name=$(Get-Sha256Hex -Path $path)"
}

Set-Content -LiteralPath $OutFile -Value ('!define HYPOMUX_PAYLOAD_SHA256 "' + ($digests -join " ") + '"') -Encoding ASCII
