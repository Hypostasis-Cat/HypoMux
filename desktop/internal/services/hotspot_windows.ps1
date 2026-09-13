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
$privateID = $null
$previousPrivateIDs = @()
$gatewayAddress = ''

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
        gateway_address = $gatewayAddress
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

function Get-SharingVerdict($shared, [guid]$publicID) {
    if (@($shared).Count -eq 0) { return 'unobserved' }
    $public = @($shared | Where-Object { $_.Role -eq 0 })
    $private = @($shared | Where-Object { $_.Role -eq 1 })
    if ($public.Count -eq 1 -and $public[0].Guid -eq $publicID -and $private.Count -eq 1 -and $private[0].Guid -eq $script:privateID) { return 'verified' }
    return 'mismatch'
}

function Test-Sharing([guid]$publicID) {
    $shared = @(Shared-Connections)
    $script:sharingDetail = 'expected=' + $publicID.ToString() + '; observed=' + (($shared | ForEach-Object { $_.Name + ' [' + $_.Guid.ToString() + '] role=' + $_.Role }) -join ', ')
    $script:sharingDetail += '; WinRT=' + [string]$manager.TetheringOperationalState + '; hotspot_adapter=' + [string]$script:privateID + '; gateway=' + $script:gatewayAddress
    return Get-SharingVerdict $shared $publicID
}

function Test-PrivateNetwork {
    $script:gatewayAddress = ''
    if ([string]$manager.TetheringOperationalState -ne 'On') { return $false }
    $candidates = @(Get-NetAdapter -IncludeHidden | Where-Object {
        $_.InterfaceDescription -like '*Wi-Fi Direct*' -and $_.Status -eq 'Up' -and
        (($null -ne $script:privateID -and [guid]$_.InterfaceGuid -eq $script:privateID) -or
         ($null -eq $script:privateID -and [guid]$_.InterfaceGuid -notin $script:previousPrivateIDs))
    })
    if ($candidates.Count -ne 1) { return $false }
    $addresses = @(Get-NetIPAddress -InterfaceIndex $candidates[0].ifIndex -AddressFamily IPv4 -ErrorAction SilentlyContinue | Where-Object {
        $_.AddressState -eq 'Preferred' -and $_.IPAddress -ne '0.0.0.0' -and $_.IPAddress -notlike '169.254.*' -and $_.IPAddress -notlike '127.*'
    })
    if ($addresses.Count -eq 0) { return $false }
    $script:privateID = [guid]$candidates[0].InterfaceGuid
    $script:gatewayAddress = [string]$addresses[0].IPAddress
    return $true
}

function Publish-Running([string]$verdict) {
    $message = ''
    if ($verdict -eq 'unobserved') { $message = '热点已开启，但传统 ICS 接口未提供出口信息。请连接手机测试；当前尚未确认手机流量经过聚合。' }
    Publish-State 'running' $message ($verdict -eq 'verified')
}

function Get-StartFailure([string]$status) {
    if ($status -eq 'WiFiDeviceOff') {
        return 'Wi-Fi 无线设备未开启（WiFiDeviceOff）。请在 Windows 快速设置中开启 Wi-Fi、关闭飞行模式；无需连接其他 Wi-Fi，然后重试。若 Wi-Fi 已开启，请检查无线网卡是否被禁用或驱动异常。'
    }
    return ('Windows tethering status: ' + $status)
}

function Stop-OwnedHotspot {
    # Transitional/off states can lag behind a failed Start/Stop result.
    $deadline = [DateTime]::UtcNow.AddSeconds(5)
    while ([string]$manager.TetheringOperationalState -eq 'InTransition' -and [DateTime]::UtcNow -lt $deadline) {
        Start-Sleep -Milliseconds 200
    }
    if ([string]$manager.TetheringOperationalState -eq 'Off') { return }
    try {
        $stopped = Await-Operation ($manager.StopTetheringAsync()) $resultType
        if ([string]$stopped.Status -ne 'Success') { throw ('Stop status: ' + [string]$stopped.Status) }
    } catch {
        if ([string]$manager.TetheringOperationalState -ne 'Off') { throw }
    }
    $deadline = [DateTime]::UtcNow.AddSeconds(5)
    while ([string]$manager.TetheringOperationalState -ne 'Off' -and [DateTime]::UtcNow -lt $deadline) {
        Start-Sleep -Milliseconds 200
    }
    if ([string]$manager.TetheringOperationalState -ne 'Off') { throw 'Windows has not confirmed hotspot is off' }
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
    $previousPrivateIDs = @(Get-NetAdapter -IncludeHidden | Where-Object { $_.InterfaceDescription -like '*Wi-Fi Direct*' -and $_.Status -eq 'Up' } | ForEach-Object { [guid]$_.InterfaceGuid })
    $attempted = $true
    $result = Await-Operation ($manager.StartTetheringAsync()) $resultType
    if ([string]$result.Status -ne 'Success') { throw (Get-StartFailure ([string]$result.Status)) }
    $phase = 'shared egress verification'
    $networkReady = $false
    $verificationDeadline = [DateTime]::UtcNow.AddSeconds(20)
    do {
        if (Test-PrivateNetwork) { $networkReady = $true; break }
        if ($stopSignal.IsCompleted) { break }
        Start-Sleep -Milliseconds 500
    } while ([DateTime]::UtcNow -lt $verificationDeadline)
    $verdict = Test-Sharing $publicID
    if (-not $networkReady) { throw 'Windows 热点未准备好：未检测到本次启动的 Wi-Fi Direct 网卡及有效 IPv4 网关，已请求关闭热点。' }
    if ($verdict -eq 'mismatch') { throw '检测到共享接口与本次 HypoMux 热点不匹配，已请求关闭热点。' }
    Publish-Running $verdict
    $phase = 'hotspot monitoring'
    while (-not $stopSignal.Wait(3000)) {
        if ([string]$manager.TetheringOperationalState -eq 'Off') { break }
        $alive = @(Get-NetAdapter -IncludeHidden | Where-Object { $_.InterfaceGuid -eq $publicID -and $_.Status -eq 'Up' })
        if ($alive.Count -ne 1) { throw 'HypoMux TUN stopped; hotspot is being closed' }
        if (-not (Test-PrivateNetwork)) { throw '热点网卡或 IPv4 网关已失效，正在关闭热点。' }
        $verdict = Test-Sharing $publicID
        if ($verdict -eq 'mismatch') { throw 'Shared egress changed; hotspot is being closed' }
        Publish-Running $verdict
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
            Stop-OwnedHotspot
        } catch { $cleanupFailed = $true; $failure += ' 热点关闭未确认；请在 Windows 中关闭移动热点，再返回重试。' }
    }
    if ($configured -and $null -ne $original) {
        try { Await-Action ($manager.ConfigureAccessPointAsync($original)) }
        catch { $cleanupFailed = $true; $failure += ' Original hotspot configuration could not be restored.' }
    }
    if ($failure -ne '') { Publish-State 'failed' $failure $false }
    else { Publish-State 'stopped' '' $false }
}
