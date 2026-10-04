param(
    [Parameter(Mandatory = $true)][string]$Adapter,
    [switch]$RequireUDP,
    [string]$NTPDomain = 'ntp.tuna.tsinghua.edu.cn',
    [ValidateSet(0, 1280)][int]$TUNMTU = 0,
    [string]$HTTPSDomain = 'www.qq.com',
    [string]$PayloadPath = '/',
    [ValidateRange(0, 16777216)][int]$PayloadBytes = 0,
    [string]$Python = 'python',
    [string]$OutputDirectory = '.go-cache-local/ipv6-system-acceptance'
)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$projectRoot = Split-Path -Parent $PSScriptRoot
$previousInput = $env:HYPOMUX_IPV6_TUN_PREPARE_INPUT
$exitCode = 2
try {
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = New-Object Security.Principal.WindowsPrincipal($identity)
    if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
        throw 'Run this explicit system test in an elevated PowerShell; it starts strict TUN and terminates only its own sidecar.'
    }
    if (@(Get-NetAdapter | Where-Object Name -eq 'HypoMux-Tun').Count -gt 0) {
        throw 'An existing HypoMux-Tun must be stopped before this isolated test.'
    }
    $nic = @([Net.NetworkInformation.NetworkInterface]::GetAllNetworkInterfaces() | Where-Object {
        $_.Name -eq $Adapter -and $_.OperationalStatus -eq 'Up' -and $_.NetworkInterfaceType -in @('Ethernet', 'Wireless80211')
    })
    if ($nic.Count -ne 1) { throw 'Select one active physical Ethernet/Wi-Fi adapter.' }
    $properties = $nic[0].GetIPProperties()
    $sources = @($properties.UnicastAddresses | Where-Object {
        $_.Address.AddressFamily -eq 'InterNetworkV6' -and -not $_.Address.IsIPv6LinkLocal -and
        -not $_.Address.IsIPv6Multicast -and -not [Net.IPAddress]::IsLoopback($_.Address) -and
        $_.DuplicateAddressDetectionState -eq 'Preferred' -and $_.AddressPreferredLifetime -gt 0
    } | Sort-Object { $_.Address.ToString() })
    if ($sources.Count -eq 0) { throw 'The selected adapter has no preferred usable IPv6 source.' }
    $index6 = $properties.GetIPv6Properties().Index
    $output = [IO.Path]::GetFullPath($(if ([IO.Path]::IsPathRooted($OutputDirectory)) { $OutputDirectory } else { Join-Path $projectRoot $OutputDirectory }))
    [IO.Directory]::CreateDirectory($output) | Out-Null
    $runtime = Join-Path $projectRoot 'desktop/bin/sing-box.exe'
    if (-not (Test-Path -LiteralPath $runtime)) { $runtime = Join-Path $projectRoot 'bin/sing-box.exe' }
    if (-not (Test-Path -LiteralPath $runtime)) { throw 'Prepare the bundled sing-box runtime first.' }
    $pythonCommand = Get-Command $Python -ErrorAction Stop
    & $pythonCommand.Source -c 'import sys; assert sys.version_info >= (3, 10), "Python 3.10 or newer required"'
    if ($LASTEXITCODE -ne 0) { throw 'Python runtime validation failed.' }
    $exitCode = 1
    $engine = Join-Path $output 'ipv6-hypomux-engine.exe'
    & go -C (Join-Path $projectRoot 'engine') build -trimpath -o $engine ./cmd/hypomux-engine
    if ($LASTEXITCODE -ne 0) { throw 'Build Core failed.' }
    & go build -trimpath -o (Join-Path $output 'ipv6-system-client.exe') (Join-Path $PSScriptRoot 'qualification/ipv6-system-client.go')
    if ($LASTEXITCODE -ne 0) { throw 'Build independent client failed.' }
    Copy-Item -LiteralPath $runtime -Destination (Join-Path $output 'sing-box.exe')
    $endpoints = @{}
    $listeners = @()
    try {
        foreach ($name in @('nic_ethernet', 'nic_wifi', 'aggregation')) {
            $listener = New-Object Net.Sockets.TcpListener([Net.IPAddress]::Loopback, 0)
            $listeners += $listener
            $listener.Start()
            $endpoints[$name] = '127.0.0.1:' + $listener.LocalEndpoint.Port
        }
    } finally { foreach ($listener in $listeners) { $listener.Stop() } }
    $servers = @($properties.DnsAddresses | ForEach-Object {
        if ($_.AddressFamily -eq 'InterNetworkV6' -and $_.IsIPv6LinkLocal) { $_.ToString().Split('%')[0] + '%' + $index6 } else { $_.ToString() }
    } | Select-Object -Unique)
    $inputPath = Join-Path $output 'preparation.json'
    $prepared = @{adapter = @{name = $Adapter; source_ipv6 = $sources[0].Address.ToString(); ipv6_if_index = $index6; dns_servers = $servers}; endpoints = $endpoints; core = $engine; output = (Join-Path $output 'config'); udp_domain = $(if ($RequireUDP) { $NTPDomain } else { '' }); tun_mtu = $TUNMTU; https_domain = $HTTPSDomain; payload_path = $PayloadPath; payload_bytes = $PayloadBytes} | ConvertTo-Json -Depth 6
    [IO.File]::WriteAllText($inputPath, $prepared, (New-Object Text.UTF8Encoding($false)))
    $env:HYPOMUX_IPV6_TUN_PREPARE_INPUT = $inputPath
    & go -C (Join-Path $projectRoot 'desktop') test ./internal/services -run '^TestPrepareIPv6SystemAcceptanceConfig$' -count=1 -timeout 90s
    if ($LASTEXITCODE -ne 0) { throw 'Production configuration preparation failed.' }
    $manifest = Get-Content -LiteralPath (Join-Path $output 'config/activation.json') -Raw | ConvertFrom-Json
    & (Join-Path $output 'sing-box.exe') check -c $manifest.config_path
    if ($LASTEXITCODE -ne 0) { throw 'Bundled runtime rejected the production configuration.' }
    & $pythonCommand.Source (Join-Path $PSScriptRoot 'qualification/ipv6-system-acceptance.py') --input $inputPath
    $exitCode = $LASTEXITCODE
    Write-Host ('Report: ' + (Join-Path $output 'ipv6-system-tun-acceptance.json'))
} catch {
    Write-Host $_.Exception.Message
} finally {
    $env:HYPOMUX_IPV6_TUN_PREPARE_INPUT = $previousInput
}
exit $exitCode
