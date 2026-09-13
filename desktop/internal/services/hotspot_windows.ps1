# Embedded, fixed script. The only configuration input is one JSON line on stdin.
# Runs in the desktop user's session: WinRT controls Windows tethering without
# elevating WebView2. Never fall back to sharing a physical/default uplink.
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
[Console]::InputEncoding = New-Object System.Text.UTF8Encoding($false)
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding($false)
$manager = $null
$original = $null
$configured = $false
$attempted = $false
$failure = ''
$phase = 'initialization'
$config = $null
$cleanupFailed = $false
$sharingDetail = ''

function Publish-State([string]$state, [string]$message, [bool]$verified) {
    $clients = 0
    if ($null -ne $manager -and $state -eq 'running') { $clients = [int]$manager.ClientCount }
    $ssid = ''; $band = 'auto'
    if ($null -ne $config) { $ssid = [string]$config.ssid; $band = [string]$config.band }
    [Console]::WriteLine((@{
        state = $state; ssid = $ssid; band = $band; clients = $clients
        shared_adapter = 'HypoMux-Tun'; sharing_verified = $verified; message = $message
        cleanup_complete = (($state -eq 'stopped' -or $state -eq 'failed') -and -not $cleanupFailed)
        diagnostics = $sharingDetail
    } | ConvertTo-Json -Compress))
}

function Await-Operation($operation, [Type]$resultType) {
    $method = [System.WindowsRuntimeSystemExtensions].GetMethods() | Where-Object {
        $_.Name -eq 'AsTask' -and $_.IsGenericMethod -and $_.GetGenericArguments().Count -eq 1 -and $_.GetParameters().Count -eq 1
    } | Select-Object -First 1
    $task = $method.MakeGenericMethod($resultType).Invoke($null, @($operation))
    if (-not $task.Wait(25000)) { $operation.Cancel(); throw 'Windows operation timed out' }
    return $task.Result
}

function Await-Action($operation) {
    $method = [System.WindowsRuntimeSystemExtensions].GetMethods() | Where-Object {
        $_.Name -eq 'AsTask' -and -not $_.IsGenericMethod -and $_.GetParameters().Count -eq 1
    } | Select-Object -First 1
    $task = $method.Invoke($null, @($operation))
    if (-not $task.Wait(15000)) { $operation.Cancel(); throw 'Windows configuration timed out' }
    $task.GetAwaiter().GetResult()
}

function Shared-Connections {
    # ICS enumeration requires elevation. Request a read-only inspection from
    # Core through the desktop broker; keep WinRT in the interactive session.
    if ($script:stopSignal.IsCompleted) { throw 'Desktop requested hotspot shutdown' }
    [Console]::WriteLine('{"kind":"inspect_sharing"}')
    if (-not $script:stopSignal.Wait(15000)) { throw 'Core sharing inspection timed out' }
    $line = $script:stopSignal.Result
    if ($null -eq $line) { throw 'Desktop disconnected during sharing inspection' }
    $script:stopSignal = [HypoMuxHotspotLifetime]::ReadStop()
    $reply = $line | ConvertFrom-Json
    if ($reply.error) { throw ([string]$reply.error) }
    foreach ($connection in $reply.connections) {
        [PSCustomObject]@{ Guid = [guid]$connection.guid; Name = [string]$connection.name; Role = [int]$connection.role }
    }
}

function Test-Sharing([guid]$publicID) {
    $shared = @(Shared-Connections)
    $public = @($shared | Where-Object { $_.Role -eq 0 })
    $private = @($shared | Where-Object { $_.Role -eq 1 })
    $script:sharingDetail = 'expected=' + $publicID.ToString() + '; observed=' + (($shared | ForEach-Object { $_.Name + ' [' + $_.Guid.ToString() + '] role=' + $_.Role }) -join ', ')
    return ($public.Count -eq 1 -and $public[0].Guid -eq $publicID -and $private.Count -eq 1 -and $private[0].Guid -ne $publicID)
}

try {
    $config = [Console]::ReadLine() | ConvertFrom-Json
    if ($null -eq $config) { throw 'Configuration is missing' }
    Add-Type -AssemblyName System.Runtime.WindowsRuntime
    # Console.In.ReadLineAsync may block synchronously on .NET Framework's
    # synchronized reader. A dedicated Task detects pipe EOF without blocking
    # the watchdog when the desktop exits or crashes.
    Add-Type -TypeDefinition @'
using System;
using System.Threading.Tasks;
public static class HypoMuxHotspotLifetime {
    public static Task<string> ReadStop() { return Task.Run(() => Console.ReadLine()); }
}
'@
    $stopSignal = [HypoMuxHotspotLifetime]::ReadStop()
    $network = [Windows.Networking.Connectivity.NetworkInformation, Windows.Networking.Connectivity, ContentType=WindowsRuntime]
    $tethering = [Windows.Networking.NetworkOperators.NetworkOperatorTetheringManager, Windows.Networking.NetworkOperators, ContentType=WindowsRuntime]
    $resultType = [Windows.Networking.NetworkOperators.NetworkOperatorTetheringOperationResult, Windows.Networking.NetworkOperators, ContentType=WindowsRuntime]
    $phase = 'TUN profile discovery'
    $tun = @(Get-NetAdapter -IncludeHidden | Where-Object { $_.Name -eq 'HypoMux-Tun' -and $_.Status -eq 'Up' })
    if ($tun.Count -ne 1) { throw 'A running HypoMux-Tun adapter is required' }
    $publicID = [guid]$tun[0].InterfaceGuid
    $profiles = @($network::GetConnectionProfiles() | Where-Object {
        $null -ne $_.NetworkAdapter -and $_.NetworkAdapter.NetworkAdapterId -eq $publicID
    })
    if ($profiles.Count -ne 1) { throw 'Windows did not expose a shareable HypoMux-Tun connection profile' }
    $phase = 'tethering capability'
    $capability = $tethering::GetTetheringCapabilityFromConnectionProfile($profiles[0])
    if ([string]$capability -ne 'Enabled') { throw ('Tethering capability: ' + [string]$capability) }
    $manager = $tethering::CreateFromConnectionProfile($profiles[0])
    if ([string]$manager.TetheringOperationalState -ne 'Off') { throw 'An existing Windows hotspot is active; turn it off before starting HypoMux hotspot' }
    if (@(Shared-Connections).Count -ne 0) { throw 'Internet Connection Sharing is already in use; existing sharing was preserved' }
    $original = $manager.GetCurrentAccessPointConfiguration()
    $desired = New-Object Windows.Networking.NetworkOperators.NetworkOperatorTetheringAccessPointConfiguration
    $desired.Ssid = [string]$config.ssid
    $desired.Passphrase = [string]$config.password
    $bandType = [Windows.Networking.NetworkOperators.TetheringWiFiBand, Windows.Networking.NetworkOperators, ContentType=WindowsRuntime]
    switch ([string]$config.band) {
        'auto' { $desired.Band = $bandType::Auto }
        '2.4' { $desired.Band = $bandType::TwoPointFourGigahertz }
        '5' { $desired.Band = $bandType::FiveGigahertz }
        default { throw 'Unsupported band' }
    }
    if (-not $desired.IsBandSupported($desired.Band)) { throw 'The Wi-Fi adapter does not support the selected band' }
    if ($stopSignal.IsCompleted) { throw 'Desktop closed before hotspot startup' }
    $phase = 'access point configuration'
    $configured = $true
    Await-Action ($manager.ConfigureAccessPointAsync($desired))
    $phase = 'hotspot startup'
    $attempted = $true
    $result = Await-Operation ($manager.StartTetheringAsync()) $resultType
    if ([string]$result.Status -ne 'Success') { throw ('Windows tethering status: ' + [string]$result.Status) }
    $phase = 'shared egress verification'
    $verified = $false
    $verificationDeadline = [DateTime]::UtcNow.AddSeconds(20)
    do {
        if (Test-Sharing $publicID) { $verified = $true; break }
        if ($stopSignal.IsCompleted) { break }
        Start-Sleep -Milliseconds 500
    } while ([DateTime]::UtcNow -lt $verificationDeadline)
    if (-not $verified) { throw 'Windows 热点共享出口校验未通过，已请求关闭热点。请查看下方共享诊断。' }
    Publish-State 'running' '' $true
    $phase = 'hotspot monitoring'
    while (-not $stopSignal.Wait(3000)) {
        if ([string]$manager.TetheringOperationalState -eq 'Off') { break }
        $alive = @(Get-NetAdapter -IncludeHidden | Where-Object { $_.InterfaceGuid -eq $publicID -and $_.Status -eq 'Up' })
        if ($alive.Count -ne 1) { throw 'HypoMux TUN stopped; hotspot is being closed' }
        if (-not (Test-Sharing $publicID)) { throw 'Shared egress changed; hotspot is being closed' }
        Publish-State 'running' '' $true
    }
} catch {
    # Do not print ErrorRecord / InvocationInfo: these can contain credentials.
    $failure = $phase + ': ' + $_.Exception.Message
    if ($null -ne $config -and -not [string]::IsNullOrEmpty([string]$config.password)) {
        $failure = $failure.Replace([string]$config.password, '[redacted]')
    }
} finally {
    if ($attempted) {
        try {
            if ([string]$manager.TetheringOperationalState -ne 'Off') {
                $stopped = Await-Operation ($manager.StopTetheringAsync()) $resultType
                if ([string]$stopped.Status -ne 'Success') { throw 'Stop failed' }
            }
        } catch { $cleanupFailed = $true; $failure += ' Hotspot cleanup failed; check Windows Mobile hotspot settings.' }
    }
    if ($configured -and $null -ne $original) {
        try { Await-Action ($manager.ConfigureAccessPointAsync($original)) }
        catch { $cleanupFailed = $true; $failure += ' Original hotspot configuration could not be restored.' }
    }
    if ($failure -ne '') { Publish-State 'failed' $failure $false }
    else { Publish-State 'stopped' '' $false }
}
