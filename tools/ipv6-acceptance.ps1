param(
    [string]$Adapter,
    [string[]]$DNSServers = @(),
    [ValidateSet('alidns', 'dnspod', 'google')]
    [string]$DNSPolicy = 'alidns',
    [switch]$RequireUDP,
    [string]$IPv6UDP = '[2400:3200::1]:53',
    [ValidateSet('dns', 'ntp')]
    [string]$IPv6UDPProtocol = 'dns',
    [switch]$RequireNAT64,
    [string]$NAT64TCP = '223.5.5.5:443',
    [string]$NAT64UDP = '223.5.5.5:53',
    [string]$NAT64ServerName = 'dns.alidns.com',
    [string]$IPv4OnlyDomain,
    [string]$Output = '.go-cache-local/ipv6-network-acceptance.json'
)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$projectRoot = Split-Path -Parent $PSScriptRoot
$reportPath = [IO.Path]::GetFullPath($(if ([IO.Path]::IsPathRooted($Output)) { $Output } else { Join-Path $projectRoot $Output }))
$report = [ordered]@{
    schema = 1; tested_at_utc = [DateTime]::UtcNow.ToString('o')
    windows_version = [Environment]::OSVersion.VersionString; host_architecture = $env:PROCESSOR_ARCHITECTURE
    scope = 'source-bound IPv6 DoH/TCP, optional IPv6 UDP and DNS64/NAT64; no system route changes'
    network_status = 'blocked'; full_acceptance = 'pending_system_matrix'
    nat64_requested = [bool]$RequireNAT64; udp_requested = [bool]$RequireUDP; udp_protocol = $IPv6UDPProtocol; dns_policy = $DNSPolicy; reason = ''; tests = @()
    remaining_system_checks = @('public IPv6 UDP if not requested', 'dual-stack asymmetric failures', 'IPv6-only physical network', 'strict TUN/WFP with public IPv6', 'sleep and adapter renumbering', 'IPv6 PMTU and UDP large packets', 'route/filter cleanup after stop and crash')
}
$exitCode = 2
$previousConfig = $env:HYPOMUX_IPV6_ACCEPTANCE_CONFIG
try {
    $eligible = @([Net.NetworkInformation.NetworkInterface]::GetAllNetworkInterfaces() | Where-Object {
        $_.OperationalStatus -eq 'Up' -and $_.NetworkInterfaceType -ne 'Loopback' -and
        (($Adapter -and $_.Name -eq $Adapter) -or (-not $Adapter -and $_.NetworkInterfaceType -in @('Ethernet', 'Wireless80211')))
    })
    $chosen = $null; $source = $null
    foreach ($candidate in $eligible) {
        $properties = $candidate.GetIPProperties()
        $sources = @($properties.UnicastAddresses | Where-Object {
            $_.Address.AddressFamily -eq 'InterNetworkV6' -and -not $_.Address.IsIPv6LinkLocal -and
            -not $_.Address.IsIPv6Multicast -and -not [Net.IPAddress]::IsLoopback($_.Address) -and
            $_.DuplicateAddressDetectionState -eq 'Preferred' -and $_.AddressPreferredLifetime -gt 0
        } | Sort-Object { $_.Address.ToString() })
        if ($sources.Count -gt 0) { $chosen = $candidate; $source = $sources[0].Address.ToString(); break }
    }
    if ($null -eq $chosen) {
        $report.reason = 'No selected active interface has a preferred, non-link-local IPv6 source; IPv4 and Teredo do not satisfy this prerequisite.'
    } else {
        $properties = $chosen.GetIPProperties()
        $index6 = $properties.GetIPv6Properties().Index
        $servers = @($DNSServers)
        if ($servers.Count -eq 0) {
            $servers = @($properties.DnsAddresses | ForEach-Object {
                if ($_.AddressFamily -eq 'InterNetworkV6') {
                    if ($_.IsIPv6LinkLocal) { $_.ToString().Split('%')[0] + '%' + $index6 } else { $_.ToString() }
                }
            })
        }
        $report.adapter = $chosen.Name
        $report.address_family = 'IPv6 source forced, even on dual-stack links'
        if ($RequireNAT64 -and $servers.Count -eq 0) {
            $report.reason = 'NAT64 requires the selected network DNS over IPv6; provide -DNSServers from that network.'
        } elseif ($RequireNAT64 -and -not $IPv4OnlyDomain) {
            $report.reason = 'Provide -IPv4OnlyDomain: a controlled A-only HTTPS domain with a valid certificate, to verify domain synthesis as well as literal targets.'
        } else {
            $config = @{ adapter = @{ name = $chosen.Name; source_ipv6 = $source; ipv6_if_index = $index6; dns_servers = $servers }; dns_policy = $DNSPolicy; ipv6_udp = $IPv6UDP; ipv6_udp_protocol = $IPv6UDPProtocol; nat64_tcp = $NAT64TCP; nat64_udp = $NAT64UDP; nat64_server_name = $NAT64ServerName; ipv4_only_domain = $IPv4OnlyDomain }
            $env:HYPOMUX_IPV6_ACCEPTANCE_CONFIG = ConvertTo-Json -InputObject $config -Depth 6 -Compress
            $expected = @('TestPublicIPv6NetworkAcceptance')
            if ($RequireUDP) { $expected += 'TestPublicIPv6UDPNetworkAcceptance' }
            if ($RequireNAT64) { $expected += 'TestPublicNAT64NetworkAcceptance' }
            $pattern = '^(' + ($expected -join '|') + ')$'
            Push-Location (Join-Path $projectRoot 'engine')
            try { $raw = @(& go test ./internal/proxy -run $pattern -count=1 -json -timeout 90s); $goExit = $LASTEXITCODE } finally { Pop-Location }
            $events = @($raw | ForEach-Object { try { $_ | ConvertFrom-Json } catch { } })
            $report.tests = @($expected | ForEach-Object {
                $testName = $_
                $finished = @($events | Where-Object { $_.PSObject.Properties['Test'] -and $_.Test -eq $testName -and $_.Action -in @('pass', 'fail', 'skip') })
                $state = if ($finished.Count -eq 0) { 'missing' } else { $finished[-1].Action }
                @{ name = $testName; status = $state; evidence = @($events | Where-Object { $_.PSObject.Properties['Test'] -and $_.Test -eq $testName -and $_.PSObject.Properties['Output'] } | ForEach-Object { $_.Output.TrimEnd() }) }
            })
            if ($goExit -eq 0 -and @($report.tests | Where-Object { $_.status -ne 'pass' }).Count -eq 0) {
                $report.network_status = 'passed'; $exitCode = 0
                $report.reason = 'Requested public network checks passed; system matrix is still required for full acceptance.'
            } else {
                $report.network_status = 'failed'; $exitCode = 1
                $report.reason = 'A requested network test failed, skipped, or did not execute.'
                $report.command_output = $raw
            }
        }
    }
} catch {
    $report.network_status = 'failed'; $exitCode = 1; $report.reason = $_.Exception.Message
} finally {
    $env:HYPOMUX_IPV6_ACCEPTANCE_CONFIG = $previousConfig
    [IO.Directory]::CreateDirectory([IO.Path]::GetDirectoryName($reportPath)) | Out-Null
    $report | ConvertTo-Json -Depth 10 | Set-Content -LiteralPath $reportPath -Encoding utf8
    Write-Host ($report.network_status + ': ' + $report.reason)
    Write-Host ('Report: ' + $reportPath)
}
exit $exitCode
