$ErrorActionPreference = "Stop"

$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
Set-Location -LiteralPath $repoRoot

$env:PYTHONPATH = (Resolve-Path ".").Path
$session = "build\qualification\phase14-dev-9a591c7\session.json"
$failed = $false

python -m engine_client.qualification_session capture `
  --session $session `
  --scenario package `
  --result passed `
  --evidence "Fresh test-signed install contains dev-9a591c7 from commit 9a591c72a77777914d10ca4dee4350d12ccf8b29" `
  --evidence "Installed engine SHA-256 is 3b72ce18b3110d49f1d260a967a07f252ff282e16a12a2f2d2c68b453b5b06a1"
if ($LASTEXITCODE -ne 0) { $failed = $true }

python -m engine_client.qualification_session capture `
  --session $session `
  --scenario proxy `
  --result passed `
  --evidence "Physical Ethernet SOCKS5 and HTTP CONNECT reached www.msftconnecttest.com:80" `
  --evidence "Ethernet telemetry recorded 229 bytes up, 1908 bytes down, 3 health successes, 0 failures" `
  --evidence "Probe artifact: network-probe.json"
if ($LASTEXITCODE -ne 0) { $failed = $true }

python -m engine_client.qualification_session capture `
  --session $session `
  --scenario dns `
  --result passed `
  --evidence "Traditional DNS, AliDNS, and DNSPod passed both A and AAAA over physical Ethernet" `
  --evidence "Google DoH failed only because direct physical access to Google services is regionally blocked; the engine did not bypass adapter binding through Clash" `
  --evidence "Probe artifact: network-probe.json"
if ($LASTEXITCODE -ne 0) { $failed = $true }

python -m engine_client.qualification_session capture `
  --session $session `
  --scenario ipv6 `
  --result passed `
  --evidence "Forced SOCKS5 IPv6 reached a physical-DNS Cloudflare AAAA address and returned 1534 bytes" `
  --evidence "The same adapter reached the A-only Microsoft connectivity peer without weakening interface binding" `
  --evidence "Probe artifact: network-probe.json"
if ($LASTEXITCODE -ne 0) { $failed = $true }

python -m engine_client.qualification_session capture `
  --session $session `
  --scenario scheduling `
  --result passed `
  --evidence "Round-robin distributed 6 requests exactly 3 Ethernet and 3 Wi-Fi" `
  --evidence "Weighted 3:1 distributed 12 requests exactly 9 Ethernet and 3 Wi-Fi" `
  --evidence "Probe artifact: scheduling-probe.json"
if ($LASTEXITCODE -ne 0) { $failed = $true }

python -m engine_client.qualification_session capture `
  --session $session `
  --scenario adapter_churn `
  --result passed `
  --evidence "Disabling WLAN preserved traffic on Ethernet and moved Wi-Fi to cooldown with one local failure" `
  --evidence "Automatic WLAN reconnect restored Wi-Fi to healthy with consecutive failures reset to zero" `
  --evidence "Probe artifact: churn-probe.json"
if ($LASTEXITCODE -ne 0) { $failed = $true }

exit [int]$failed
