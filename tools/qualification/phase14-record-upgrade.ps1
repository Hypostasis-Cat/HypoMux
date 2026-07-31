$ErrorActionPreference = "Stop"

$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
Set-Location -LiteralPath $repoRoot

$env:PYTHONPATH = (Resolve-Path ".").Path
$session = "build\qualification\phase14-dev-9a591c7\session.json"

python -m engine_client.qualification_session capture `
  --session $session `
  --scenario upgrade `
  --result passed `
  --evidence "Upgraded the installed test-signed candidate from dev-19e672f to dev-9a591c7 without uninstalling first" `
  --evidence "The upgraded engine reports commit 9a591c72a77777914d10ca4dee4350d12ccf8b29 and SHA-256 3b72ce18b3110d49f1d260a967a07f252ff282e16a12a2f2d2c68b453b5b06a1" `
  --evidence "Independent elevated postflight found no stale HypoMux process, route, or TUN device"

exit $LASTEXITCODE
