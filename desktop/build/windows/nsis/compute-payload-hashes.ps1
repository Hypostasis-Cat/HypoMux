# Computes the SHA-256 of the Core binaries staged by the installer while
# makensis compiles. project.nsi embeds the resulting digests into the signed
# installer and passes them to the elevated setup helper, which refuses to
# apply a payload whose bytes no longer match (desktop/internal/setup/
# payload_windows.go). Files staged under the user's temp directory cannot be
# trusted by an elevated process without such an independent reference.
param(
    [Parameter(Mandatory = $true)][string]$BinDir,
    [Parameter(Mandatory = $true)][string]$OutFile
)

$ErrorActionPreference = "Stop"

$names = @("hypomux-engine.exe", "sing-box.exe", "wintun.dll")
$digests = @()
foreach ($name in $names) {
    $path = Join-Path $BinDir $name
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
        Write-Error "payload binary missing: $path"
        exit 1
    }
    $hash = (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash.ToLowerInvariant()
    $digests += "$name=$hash"
}

Set-Content -LiteralPath $OutFile -Value ('!define HYPOMUX_PAYLOAD_SHA256 "' + ($digests -join " ") + '"') -Encoding ASCII
