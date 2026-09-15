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
$beforeNetwork = @{}
$pendingPrivateKey = ''
$privateSamples = 0
$networkQueryFailed = $false
$sharingQueryFailed = $false
$hotspotOffConfirmed = $null
$configurationRestored = $true
$cleanupError = ''
$sharingRetryCount = 0
$nextSharingCheck = [DateTime]::MinValue
$privateNetworkDetail = ''
$gatewayAddress = ''

function Publish-State([string]$state, [string]$message, [bool]$verified) {
    $clients = 0
    $devices = @(); $devicesAvailable = $false
    if ($null -ne $manager -and $state -eq 'running') { $clients = [int]$manager.ClientCount }
    if ($null -ne $manager -and $state -eq 'running') {
        try {
            $devices = @($manager.GetTetheringClients() | ForEach-Object {
                @{ mac = [string]$_.MacAddress; hosts = @($_.HostNames | ForEach-Object { [string]$_.DisplayName }) }
            })
            $devicesAvailable = $true
        } catch { $devices = @() } # Optional UI detail must not stop networking.
    }
    $ssid = ''; $band = 'auto'
    if ($null -ne $config) { $ssid = [string]$config.ssid; $band = [string]$config.band }
    [Console]::WriteLine((@{
        state = $state; ssid = $ssid; band = $band; clients = $clients
        shared_adapter = 'HypoMux-Tun'; sharing_verified = $verified; message = $message
        cleanup_complete = (($state -eq 'stopped' -or $state -eq 'failed') -and -not $cleanupFailed)
        hotspot_off_confirmed = $hotspotOffConfirmed
        configuration_restored = $configurationRestored
        cleanup_error = $cleanupError
        diagnostics = $sharingDetail
        gateway_address = $gatewayAddress
        devices = $devices; devices_available = $devicesAvailable
        updated_at = [DateTime]::UtcNow.ToString('o')
    } | ConvertTo-Json -Depth 5 -Compress))
}

function Await-Operation($operation, [Type]$resultType, [int]$timeoutMs = 25000) {
    $method = [System.WindowsRuntimeSystemExtensions].GetMethods() | Where-Object {
        $_.Name -eq 'AsTask' -and $_.IsGenericMethod -and $_.GetGenericArguments().Count -eq 1 -and $_.GetParameters().Count -eq 1
    } | Select-Object -First 1
    $task = $method.MakeGenericMethod($resultType).Invoke($null, @($operation))
    if (-not $task.Wait($timeoutMs)) { $operation.Cancel(); throw 'Windows operation timed out' }
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
    if ($reply.error) { $script:sharingQueryFailed = $true; throw ([string]$reply.error) }
    foreach ($connection in $reply.connections) {
        [PSCustomObject]@{ Guid = [guid]$connection.guid; Name = [string]$connection.name; Role = [int]$connection.role }
    }
}

function Get-SharingVerdict($shared, [guid]$publicID) {
    if (@($shared).Count -eq 0) { return 'unobserved' }
    $public = @($shared | Where-Object { $_.Role -eq 0 })
    $private = @($shared | Where-Object { $_.Role -eq 1 })
    if ($public.Count -eq 1 -and $public[0].Guid -eq $publicID -and $private.Count -eq 1 -and $private[0].Guid -eq $script:privateID) { return 'verified' }
    if (@($shared | Where-Object { ($_.Role -eq 0 -and $_.Guid -ne $publicID) -or ($_.Role -eq 1 -and $null -ne $script:privateID -and $_.Guid -ne $script:privateID) -or $_.Role -notin @(0, 1) }).Count -gt 0) { return 'mismatch' }
    return 'transitional'
}

function Test-Sharing([guid]$publicID) {
    $script:sharingQueryFailed = $false
    try { $shared = @(Shared-Connections) }
    catch {
        # Only a consumed error reply is retryable. Pipe/timeout failures lose
        # protocol synchronization and must terminate the worker.
        if (-not $script:sharingQueryFailed) { throw }
        $script:sharingDetail = 'sharing_query_error=' + $_.Exception.Message
        return 'query_error'
    }
    $script:sharingDetail = 'expected=' + $publicID.ToString() + '; observed=' + (($shared | ForEach-Object { $_.Name + ' [' + $_.Guid.ToString() + '] role=' + $_.Role }) -join ', ')
    $script:sharingDetail += '; WinRT=' + [string]$manager.TetheringOperationalState + '; hotspot_adapter=' + [string]$script:privateID + '; gateway=' + $script:gatewayAddress + '; readiness=' + $script:privateNetworkDetail
    return Get-SharingVerdict $shared $publicID
}

function Read-WirelessNetwork {
    $adapters = @(Get-NetAdapter -IncludeHidden -ErrorAction Stop | Where-Object {
        $_.InterfaceDescription -like '*Wi-Fi Direct*' -or $_.InterfaceType -eq 71 -or $_.NdisPhysicalMedium -in @(1, 9)
    })
    $uplinks = @(Get-NetRoute -AddressFamily IPv4 -ErrorAction Stop | Where-Object { $_.DestinationPrefix -eq '0.0.0.0/0' } | ForEach-Object { $_.InterfaceIndex })
    # Query all addresses once: querying an addressless interface with a CIM
    # filter may raise "no matching objects", which is normal before startup.
    $addresses = @(Get-NetIPAddress -AddressFamily IPv4 -ErrorAction Stop)
    foreach ($adapter in $adapters) {
        $valid = @($addresses | Where-Object {
            $_.InterfaceIndex -eq $adapter.ifIndex -and $_.AddressState -eq 'Preferred' -and
            $_.IPAddress -ne '0.0.0.0' -and $_.IPAddress -notlike '169.254.*' -and $_.IPAddress -notlike '127.*'
        } | ForEach-Object { [string]$_.IPAddress } | Sort-Object -Unique)
        [PSCustomObject]@{ Guid = [guid]$adapter.InterfaceGuid; Status = [string]$adapter.Status; Addresses = $valid; Uplink = ($adapter.ifIndex -in $uplinks) }
    }
}

function Save-NetworkBaseline {
    $snapshot = @{}
    foreach ($entry in @(Read-WirelessNetwork)) { $snapshot[$entry.Guid.ToString()] = $entry }
    $script:beforeNetwork = $snapshot
}

function Test-PrivateNetwork {
    $script:gatewayAddress = ''
    $script:networkQueryFailed = $false
    $script:privateNetworkDetail = 'WinRT is not On'
    if ([string]$manager.TetheringOperationalState -ne 'On') {
        $script:pendingPrivateKey = ''; $script:privateSamples = 0
        return $false
    }
    try { $snapshot = @(Read-WirelessNetwork) }
    catch {
        $script:networkQueryFailed = $true
        $script:privateNetworkDetail = 'network_query_error=' + $_.Exception.Message
        $script:pendingPrivateKey = ''; $script:privateSamples = 0
        return $false
    }
    $ready = @($snapshot | Where-Object {
        $before = $script:beforeNetwork[$_.Guid.ToString()]
        $_.Status -eq 'Up' -and -not $_.Uplink -and $_.Addresses.Count -gt 0 -and
        (($null -ne $script:privateID -and $_.Guid -eq $script:privateID) -or
         ($null -eq $script:privateID -and ($null -eq $before -or
          (-not $before.Uplink -and ($before.Status -ne 'Up' -or $before.Addresses.Count -eq 0)))))
    })
    $script:privateNetworkDetail = 'ready=' + (($ready | ForEach-Object { $_.Guid.ToString() + ':' + ($_.Addresses -join ',') }) -join ';')
    if ($ready.Count -ne 1) { $script:pendingPrivateKey = ''; $script:privateSamples = 0; return $false }
    $key = $ready[0].Guid.ToString() + ':' + ($ready[0].Addresses -join ',')
    if ($null -eq $script:privateID) {
        if ($key -eq $script:pendingPrivateKey) { $script:privateSamples++ }
        else { $script:pendingPrivateKey = $key; $script:privateSamples = 1 }
        $script:privateNetworkDetail += '; stable_samples=' + $script:privateSamples
        if ($script:privateSamples -lt 3) { return $false }
        $script:privateID = $ready[0].Guid
    }
    $script:gatewayAddress = $ready[0].Addresses[0]
    return $true
}

function Test-PublicNetwork([guid]$publicID) {
    $script:networkQueryFailed = $false
    try { $alive = @(Get-NetAdapter -IncludeHidden -ErrorAction Stop | Where-Object { [guid]$_.InterfaceGuid -eq $publicID -and $_.Status -eq 'Up' }) }
    catch {
        $script:networkQueryFailed = $true
        $script:privateNetworkDetail = 'public_query_error=' + $_.Exception.Message
        return $false
    }
    return ($alive.Count -eq 1)
}

function Update-SharingCheck([guid]$publicID) {
    $verdict = Test-Sharing $publicID
    if ($verdict -eq 'mismatch') { throw 'Shared egress mismatch; hotspot is being closed' }
    if ($verdict -in @('transitional', 'query_error')) {
        $script:sharingRetryCount++
        if ($script:sharingRetryCount -ge 3) { throw '共享接口连续三次未能完成校验，正在关闭热点。' }
        $script:nextSharingCheck = [DateTime]::UtcNow.AddSeconds(3)
    } else {
        $script:sharingRetryCount = 0
        $script:nextSharingCheck = [DateTime]::UtcNow.AddSeconds(15)
    }
    return $verdict
}

function Publish-Running([string]$verdict) {
    $message = ''
    if ($verdict -eq 'unobserved') { $message = '热点已开启，但传统 ICS 接口未提供出口信息。请连接手机测试；当前尚未确认手机流量经过聚合。' }
    if ($verdict -in @('transitional', 'query_error')) { $message = '共享接口正在重新校验，当前出口尚未确认。' }
    Publish-State 'running' $message ($verdict -eq 'verified')
}

function Get-StartFailure([string]$status) {
    if ($status -eq 'WiFiDeviceOff') {
        return 'Wi-Fi 无线设备未开启（WiFiDeviceOff）。请在 Windows 快速设置中开启 Wi-Fi、关闭飞行模式；无需连接其他 Wi-Fi，然后重试。若 Wi-Fi 已开启，请检查无线网卡是否被禁用或驱动异常。'
    }
    return ('Windows tethering status: ' + $status)
}

function Read-ConnectionProfiles { $network::GetConnectionProfiles() }

function Read-HostNetworkAdapters {
    $network::GetHostNames() | ForEach-Object {
        if ($null -ne $_.IPInformation -and $null -ne $_.IPInformation.NetworkAdapter) { $_.IPInformation.NetworkAdapter }
    }
}

function Read-TunProfile([guid]$publicID) {
    try { $observed = @(Read-ConnectionProfiles) }
    catch { $script:sharingDetail = 'profile_query_error=' + $_.Exception.Message; return $null }
    $script:sharingDetail = 'profile_expected=' + $publicID + '; profile_adapters=' + (($observed | ForEach-Object {
        if ($null -ne $_.NetworkAdapter) { [string]$_.NetworkAdapter.NetworkAdapterId }
    }) -join ', ')
    $matches = @($observed | Where-Object { $null -ne $_.NetworkAdapter -and [guid]$_.NetworkAdapter.NetworkAdapterId -eq $publicID })
    if ($matches.Count -gt 1) { throw 'Windows 返回了多个 HypoMux-Tun 连接配置，无法唯一确认共享出口。' }
    if ($matches.Count -eq 1) { return $matches[0] }
    # Windows 10 may omit the TUN from the global profile list while host IP
    # information still exposes its WinRT adapter. Query that SAME adapter.
    # ProfileName may equal the upstream Wi-Fi name; only the GUID is identity.
    try { $adapters = @(Read-HostNetworkAdapters | Where-Object { [guid]$_.NetworkAdapterId -eq $publicID }) }
    catch { $script:sharingDetail += '; host_query_error=' + $_.Exception.Message; return $null }
    $script:sharingDetail += '; host_adapter_matches=' + $adapters.Count
    if ($adapters.Count -eq 0) { return $null }
    $profileType = [Windows.Networking.Connectivity.ConnectionProfile, Windows.Networking.Connectivity, ContentType=WindowsRuntime]
    try { $profile = Await-Operation ($adapters[0].GetConnectedProfileAsync()) $profileType 2000 }
    catch { $script:sharingDetail += '; connected_profile_error=' + $_.Exception.Message; return $null }
    if ($null -eq $profile) { return $null }
    if ($null -eq $profile.NetworkAdapter -or [guid]$profile.NetworkAdapter.NetworkAdapterId -ne $publicID) {
        throw 'Windows 返回的连接配置不属于 HypoMux-Tun，已拒绝共享其他网卡。'
    }
    $script:sharingDetail += '; profile_source=adapter_connected_profile'
    return $profile
}

function Wait-TunProfile([guid]$publicID, [int]$timeoutMs = 10000) {
    $deadline = [DateTime]::UtcNow.AddMilliseconds($timeoutMs)
    do {
        if ($script:stopSignal.IsCompleted) { throw 'Desktop closed during TUN profile discovery' }
        $profile = $null
        if (Test-PublicNetwork $publicID) { $profile = Read-TunProfile $publicID }
        elseif (-not $script:networkQueryFailed) { throw 'HypoMux-Tun stopped during connection profile discovery' }
        else { $script:sharingDetail = $script:privateNetworkDetail }
        if ($null -ne $profile) { return $profile }
        if ($script:stopSignal.Wait(500)) { throw 'Desktop closed during TUN profile discovery' }
    } while ([DateTime]::UtcNow -lt $deadline)
    throw 'Windows 未提供 HypoMux-Tun 的连接配置，暂时无法开启聚合热点。请停止并重新启动 TUN 后重试；若仍失败，请复制共享诊断。'
}

function Set-HotspotBand($desired, [string]$band, [bool]$bandAvailable) {
    if ($band -notin @('auto', '2.4', '5')) { throw 'Unsupported band' }
    # Band and IsBandSupported were added in Windows 10 2004. Older systems
    # can still start a hotspot with the system-selected band.
    if (-not $bandAvailable) {
        if ($band -ne 'auto') { throw '当前 Windows 不支持指定热点频段，请选择自动频段或升级到 Windows 10 2004 及以上版本。' }
        return
    }
    $bandType = [Windows.Networking.NetworkOperators.TetheringWiFiBand, Windows.Networking.NetworkOperators, ContentType=WindowsRuntime]
    switch ($band) {
        'auto' { $desired.Band = $bandType::Auto }
        '2.4' { $desired.Band = $bandType::TwoPointFourGigahertz }
        '5' { $desired.Band = $bandType::FiveGigahertz }
    }
    if (-not $desired.IsBandSupported($desired.Band)) { throw 'The Wi-Fi adapter does not support the selected band' }
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

function Complete-HotspotCleanup {
    if ($attempted) {
        try {
            Stop-OwnedHotspot
            $script:hotspotOffConfirmed = $true
        } catch { $script:cleanupFailed = $true; $script:cleanupError += '热点关闭未确认；请在 Windows 设置中检查移动热点。'; $script:failure += ' ' + $cleanupError }
    }
    if ($configured -and $null -ne $original -and $hotspotOffConfirmed -eq $true) {
        try { Await-Action ($manager.ConfigureAccessPointAsync($original)); $script:configurationRestored = $true }
        catch { $script:cleanupFailed = $true; $script:cleanupError += ' 原热点配置未能恢复，请在 Windows 设置中检查名称、密码和频段。'; $script:failure += ' Original hotspot configuration could not be restored.' }
    }
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
    $profile = Wait-TunProfile $publicID
    $phase = 'tethering capability'
    $capability = $tethering::GetTetheringCapabilityFromConnectionProfile($profile)
    if ([string]$capability -ne 'Enabled') { throw ('Tethering capability: ' + [string]$capability) }
    $manager = $tethering::CreateFromConnectionProfile($profile)
    if ([string]$manager.TetheringOperationalState -ne 'Off') { throw 'An existing Windows hotspot is active; turn it off before starting HypoMux hotspot' }
    $hotspotOffConfirmed = $true
    if (@(Shared-Connections).Count -ne 0) { throw 'Internet Connection Sharing is already in use; existing sharing was preserved' }
    $original = $manager.GetCurrentAccessPointConfiguration()
    $desired = New-Object Windows.Networking.NetworkOperators.NetworkOperatorTetheringAccessPointConfiguration
    $desired.Ssid = [string]$config.ssid
    $desired.Passphrase = [string]$config.password
    $apiInformation = [Windows.Foundation.Metadata.ApiInformation, Windows.Foundation, ContentType=WindowsRuntime]
    $configurationType = 'Windows.Networking.NetworkOperators.NetworkOperatorTetheringAccessPointConfiguration'
    $bandAvailable = $apiInformation::IsPropertyPresent($configurationType, 'Band') -and $apiInformation::IsMethodPresent($configurationType, 'IsBandSupported')
    Set-HotspotBand $desired ([string]$config.band) $bandAvailable
    if ($stopSignal.IsCompleted) { throw 'Desktop closed before hotspot startup' }
    $phase = 'network baseline'
    Save-NetworkBaseline
    $phase = 'access point configuration'
    $configured = $true
    $configurationRestored = $false
    Await-Action ($manager.ConfigureAccessPointAsync($desired))
    $phase = 'hotspot startup'
    $attempted = $true
    $hotspotOffConfirmed = $false
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
    if (-not $networkReady) { $sharingDetail = $privateNetworkDetail; throw 'Windows 热点未准备好：未能唯一识别本次启动的热点无线网卡及有效 IPv4 网关，已请求关闭热点。' }
    do {
        $verdict = Update-SharingCheck $publicID
        if ($verdict -in @('verified', 'unobserved')) { break }
        if ($stopSignal.Wait(3000)) { throw 'Desktop closed during sharing verification' }
    } while ($true)
    Publish-Running $verdict
    $phase = 'hotspot monitoring'
    $networkFailures = 0
    while (-not $stopSignal.Wait(3000)) {
        if ([string]$manager.TetheringOperationalState -eq 'Off') { break }
        $publicReady = Test-PublicNetwork $publicID
        if (-not $publicReady -and -not $networkQueryFailed) { throw 'HypoMux TUN stopped; hotspot is being closed' }
        $privateReady = $false
        if ($publicReady) { $privateReady = Test-PrivateNetwork }
        if (-not $privateReady) {
            if (-not $networkQueryFailed) { throw '热点网卡或 IPv4 网关已失效，正在关闭热点。' }
            $networkFailures++
            $sharingDetail = $privateNetworkDetail
            if ($networkFailures -ge 3) { throw '连续三次无法读取热点网络状态，正在关闭热点。' }
            $verdict = 'query_error'
            $nextSharingCheck = [DateTime]::MinValue
            Publish-Running $verdict
            continue
        }
        $networkFailures = 0
        if ([DateTime]::UtcNow -ge $nextSharingCheck) { $verdict = Update-SharingCheck $publicID }
        Publish-Running $verdict
    }
} catch {
    # Do not print ErrorRecord / InvocationInfo: these can contain credentials.
    $failure = $phase + ': ' + $_.Exception.Message
    if ($null -ne $config -and -not [string]::IsNullOrEmpty([string]$config.password)) {
        $failure = $failure.Replace([string]$config.password, '[redacted]')
    }
} finally {
    Complete-HotspotCleanup
    if ($failure -ne '') { Publish-State 'failed' $failure $false }
    else { Publish-State 'stopped' '' $false }
}
