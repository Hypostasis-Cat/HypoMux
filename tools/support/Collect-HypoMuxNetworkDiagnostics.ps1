#requires -Version 5.1
[CmdletBinding()]
param(
    [string]$OutputDirectory = [Environment]::GetFolderPath('Desktop'),
    [string]$DataDirectory,
    [switch]$LibraryOnly
)

$ErrorActionPreference = 'Stop'
$utf8 = [Text.UTF8Encoding]::new($true)
$powershellExe = Join-Path $env:SystemRoot 'System32\WindowsPowerShell\v1.0\powershell.exe'

function Protect-Text([string]$Text) {
    if ($env:USERPROFILE) {
        $Text = $Text.Replace($env:USERPROFILE.Replace('\', '\\'), '<USERPROFILE>')
        $Text = $Text.Replace($env:USERPROFILE, '<USERPROFILE>')
    }
    $Text = [regex]::Replace($Text, '(?i)(https?://[^\s"<>?]+)\?[^\s"<>]+', '$1?<REDACTED>')
    $Text = [regex]::Replace($Text, '(?i)(https?://)[^/\s"<>@]+@', '$1<REDACTED>@')
    $Text = [regex]::Replace($Text, '(?i)("(?:secret|password|token|api_key|apikey|authorization)"\s*:\s*")[^"]*', '$1<REDACTED>')
    $Text = [regex]::Replace($Text, '(?im)((?:authorization|proxy-authorization):\s*)[^\r\n]+', '$1<REDACTED>')
    return $Text
}

function Write-ReportFile([string]$Name, [string]$Text) {
    [IO.File]::WriteAllText((Join-Path $script:reportDir $Name), (Protect-Text $Text), $utf8)
}

# Windows native argv quoting, including quotes and trailing backslashes.
function Quote-Argument([string]$Value) {
    return '"' + [regex]::Replace([regex]::Replace($Value, '(\\*)"', '$1$1\"'), '(\\+)$', '$1$1') + '"'
}

function Invoke-BoundedProcess([string]$File, [string[]]$Arguments, [int]$TimeoutSeconds = 15, [Text.Encoding]$Encoding = [Text.Encoding]::UTF8) {
    $p = [Diagnostics.Process]::new()
    $p.StartInfo = [Diagnostics.ProcessStartInfo]::new()
    $p.StartInfo.FileName = $File
    $p.StartInfo.Arguments = (($Arguments | ForEach-Object { Quote-Argument $_ }) -join ' ')
    $p.StartInfo.UseShellExecute = $false
    $p.StartInfo.CreateNoWindow = $true
    $p.StartInfo.RedirectStandardOutput = $true
    $p.StartInfo.RedirectStandardError = $true
    $p.StartInfo.StandardOutputEncoding = $Encoding
    $p.StartInfo.StandardErrorEncoding = $Encoding
    $started = [DateTimeOffset]::Now
    try {
        $null = $p.Start()
        $stdout = $p.StandardOutput.ReadToEndAsync()
        $stderr = $p.StandardError.ReadToEndAsync()
        $timedOut = -not $p.WaitForExit($TimeoutSeconds * 1000)
        if ($timedOut) { $p.Kill(); $null = $p.WaitForExit(3000) }
        $out = if ($stdout.Wait(3000)) { $stdout.Result } else { '[stdout unavailable]' }
        $err = if ($stderr.Wait(3000)) { $stderr.Result } else { '[stderr unavailable]' }
        $code = if ($timedOut) { -2 } else { $p.ExitCode }
        return [pscustomobject]@{ ExitCode = $code; TimedOut = $timedOut; Stdout = $out; Stderr = $err; Started = $started.ToString('o') }
    } catch {
        return [pscustomobject]@{ ExitCode = -1; TimedOut = $false; Stdout = ''; Stderr = $_.Exception.Message; Started = $started.ToString('o') }
    } finally { $p.Dispose() }
}

function Invoke-PSProbe([string]$Code, [int]$TimeoutSeconds = 15) {
    # No native child processes inside these workers: killing a timed-out worker is sufficient.
    $prefix = '[Console]::OutputEncoding = [Text.UTF8Encoding]::new($false); $ErrorActionPreference = "Stop"; $ProgressPreference = "SilentlyContinue"; '
    $encoded = [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($prefix + $Code))
    Invoke-BoundedProcess $powershellExe @('-NoProfile', '-NonInteractive', '-OutputFormat', 'Text', '-EncodedCommand', $encoded) $TimeoutSeconds
}

function Save-Probe([string]$Name, $Result) {
    Write-ReportFile ($Name + '.txt') ("Started: $($Result.Started)`r`nExitCode: $($Result.ExitCode)`r`nTimedOut: $($Result.TimedOut)`r`n`r`n$($Result.Stdout)`r`n$($Result.Stderr)")
}

function Get-Finding($Normal, $Fixed, $Tcp) {
    if ($Normal.ExitCode -eq 0) { return '默认路径的百度 HTTPS 测试成功；不代表所有应用、所有出口均正常。' }
    if ($Normal.ExitCode -eq -1 -or $Fixed.ExitCode -eq -1) { return 'HTTPS 检查工具无法运行，结果不足以判断网络故障。' }
    if ($Fixed.ExitCode -eq 0) { return '普通 HTTPS 失败、固定真实 IP 成功：优先检查 DNS、FakeIP 和相关分流；这不是根因定论。' }
    if ($Tcp.ExitCode -eq 0 -and $Tcp.Stdout -match 'TCP_OK') { return '普通及固定 IP 的 HTTPS 均失败，但公网 TCP 建连成功：检查 TUN/出口转发及 TLS 数据路径，不能仅归因于 DNS。' }
    return 'HTTPS 与公网 TCP 未形成成功对照：检查出口和路由，并结合各探测原始记录；单一目标失败不代表全网不可用。'
}

if ($LibraryOnly) { return }
if (-not $OutputDirectory) { $OutputDirectory = $env:TEMP }
$script:reportDir = Join-Path $OutputDirectory ('HypoMux-Network-' + (Get-Date -Format 'yyyyMMdd-HHmmss') + '-' + [guid]::NewGuid().ToString('N').Substring(0, 6))
try { [IO.Directory]::CreateDirectory($script:reportDir) | Out-Null }
catch {
    $script:reportDir = Join-Path $env:TEMP ([IO.Path]::GetFileName($script:reportDir))
    [IO.Directory]::CreateDirectory($script:reportDir) | Out-Null
}
$script:reportDir = [IO.Path]::GetFullPath($script:reportDir)
$summary = [Collections.Generic.List[string]]::new()
$summary.Add('HypoMux 一键网络排查 v1')
$summary.Add('开始时间：' + [DateTimeOffset]::Now.ToString('o'))
$summary.Add('本次只采集当前状态；请保持 TUN 开启且故障可复现。未自动切换网卡、停止代理、清空 DNS 或重置网络。')
$summary.Add('报告含网卡/IP、路由、进程名、访问测试目标及应用日志。已尽力隐藏用户目录、URL 参数和常见密钥；发送前可检查。不会自动上传。')
Write-Host '请保持 TUN 开启。正在自动排查，通常需要 2–5 分钟。' -ForegroundColor Cyan
Write-Host ('报告目录：' + $script:reportDir)

try {
    Write-Host '[1/5] 收集系统、网卡、路由和代理设置...'
    $inventory = Invoke-PSProbe @'
$errors = [Collections.Generic.List[string]]::new()
function Read-Safely([scriptblock]$Action) { try { & $Action } catch { $errors.Add($_.Exception.Message) } }
$adapters = @(Read-Safely { Get-NetAdapter -IncludeHidden | Select-Object Name, InterfaceDescription, ifIndex, Status, InterfaceGuid, HardwareInterface, LinkSpeed })
$addresses = @(Read-Safely { Get-NetIPAddress | Select-Object InterfaceIndex, InterfaceAlias, IPAddress, AddressFamily, PrefixLength, AddressState })
$dns = @(Read-Safely { Get-DnsClientServerAddress | Select-Object InterfaceIndex, InterfaceAlias, AddressFamily, ServerAddresses })
$routes = @(Read-Safely { Get-NetRoute | Select-Object InterfaceIndex, InterfaceAlias, DestinationPrefix, NextHop, RouteMetric, State })
$interfaces = @(Read-Safely { Get-NetIPInterface | Select-Object InterfaceIndex, InterfaceAlias, AddressFamily, ConnectionState, NlMtu, InterfaceMetric })
[pscustomobject]@{ Adapters=$adapters; Addresses=$addresses; DNS=$dns; Routes=$routes; Interfaces=$interfaces; Errors=@($errors.ToArray()) } | ConvertTo-Json -Depth 8
'@ 30
    Save-Probe '01-network' $inventory
    $network = $null
    if ($inventory.ExitCode -eq 0) { try { $network = $inventory.Stdout | ConvertFrom-Json } catch {} }
    if (-not $network -or @($network.Errors).Count -gt 0) {
        $summary.Add('部分网卡/CIM 信息读取失败，已补充 ipconfig 和 route 原始输出；自动源地址对照可能不完整。')
        $oem = [Text.Encoding]::GetEncoding([Globalization.CultureInfo]::CurrentCulture.TextInfo.OEMCodePage)
        Save-Probe '01-fallback-ipconfig' (Invoke-BoundedProcess "$env:SystemRoot\System32\ipconfig.exe" @('/all') 15 $oem)
        Save-Probe '01-fallback-route' (Invoke-BoundedProcess "$env:SystemRoot\System32\route.exe" @('print') 15 $oem)
    }
    $system = Invoke-PSProbe @'
$names = '^(HypoMux|hypomux-engine|sing-box|clash.*|mihomo.*|v2ray.*|xray.*|hysteria.*|tailscale.*|wireguard.*|WeChat|Weixin|curl)$'
$processes = @(Get-Process | Where-Object { $_.ProcessName -match $names } | Select-Object ProcessName, Id, Path)
$services = @(Get-Service -Name HypoMuxCore,BFE,MpsSvc,Dnscache -ErrorAction SilentlyContinue | Select-Object Name, Status)
$proxy = Get-ItemProperty 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Internet Settings' -ErrorAction SilentlyContinue | Select-Object ProxyEnable, ProxyServer, AutoConfigURL
$listeners = @(Get-NetTCPConnection -State Listen -ErrorAction SilentlyContinue | Where-Object { $_.OwningProcess -in $processes.Id } | Select-Object LocalAddress, LocalPort, OwningProcess)
[pscustomobject]@{ OS=[Environment]::OSVersion.VersionString; PS=$PSVersionTable.PSVersion.ToString(); Processes=$processes; Services=$services; Proxy=$proxy; Listeners=$listeners } | ConvertTo-Json -Depth 6
'@ 25
    Save-Probe '02-system-processes-proxy' $system
    if ($system.ExitCode -eq 0) {
        try {
            $sys = $system.Stdout | ConvertFrom-Json
            $summary.Add('相关运行进程：' + (($sys.Processes | ForEach-Object { $_.ProcessName }) -join ', '))
            $summary.Add('进程存在不等于 TUN 接管成功；需结合网卡、路由和实际连接结果。')
        } catch {}
    }

    Write-Host '[2/5] 检查系统 DNS、网卡 DNS 和公共 DNS...'
    $dnsServers = @('') + @($network.DNS | ForEach-Object { $_.ServerAddresses } | Where-Object { $_ -match '^\d+\.\d+\.\d+\.\d+$' } | Select-Object -Unique -First 4) + @('223.5.5.5')
    $dnsIndex = 0
    foreach ($server in ($dnsServers | Select-Object -Unique)) {
        $dnsIndex++
        $serverOption = if ($server) { " -Server '$server'" } else { '' }
        $dnsResult = Invoke-PSProbe ("Resolve-DnsName www.baidu.com -Type A -DnsOnly -NoHostsFile -QuickTimeout" + $serverOption + ' | Select-Object Name,Type,IPAddress,NameHost | ConvertTo-Json') 10
        Save-Probe ("dns-$dnsIndex") $dnsResult
        $label = if ($server) { $server } else { '系统默认' }
        $summary.Add("DNS [$label]：退出码 $($dnsResult.ExitCode)，详见 dns-$dnsIndex.txt。指定 DNS 的请求也可能被 TUN 接管。")
    }

    Write-Host '[3/5] 检查 HTTPS、固定 IP、公网 TCP 和 ICMP...'
    $curlCommand = Get-Command curl.exe -ErrorAction SilentlyContinue
    $curlExe = if ($curlCommand) { $curlCommand.Source } else { 'curl.exe' }
    # -q must come first: do not execute options inherited from the user's .curlrc.
    $curlBase = @('-q', '--noproxy', '*', '-I', '-v', '--connect-timeout', '8', '--max-time', '12', '--write-out', '\nDIAG http=%{http_code} remote=%{remote_ip} local=%{local_ip} connect=%{time_connect} tls=%{time_appconnect} total=%{time_total}\n')
    $normal = Invoke-BoundedProcess $curlExe ($curlBase + @('https://www.baidu.com')) 16
    Save-Probe 'https-baidu-default' $normal
    $fixedArgs = @('--resolve', 'www.baidu.com:443:183.2.172.177', 'https://www.baidu.com')
    $fixed = Invoke-BoundedProcess $curlExe ($curlBase + $fixedArgs) 16
    Save-Probe 'https-baidu-fixed-ip' $fixed
    $other = Invoke-BoundedProcess $curlExe ($curlBase + @('https://www.qq.com')) 16
    Save-Probe 'https-qq-default' $other
    $tcp = Invoke-PSProbe @'
$client = [Net.Sockets.TcpClient]::new()
try {
    $task = $client.ConnectAsync('1.12.12.12',443)
    if (-not $task.Wait(8000)) { throw 'TCP timeout' }
    if (-not $client.Connected) { throw 'TCP not connected' }
    'TCP_OK local=' + $client.Client.LocalEndPoint + ' remote=' + $client.Client.RemoteEndPoint
} finally { $client.Dispose() }
'@ 12
    Save-Probe 'tcp-public-443' $tcp
    $summary.Add((Get-Finding $normal $fixed $tcp))
    $summary.Add("HTTPS 退出码：百度默认=$($normal.ExitCode)，百度固定 IP=$($fixed.ExitCode)，腾讯默认=$($other.ExitCode)。0 表示 curl 完成，并不保证 HTTP 状态为 200，请看原始记录。")
    $summary.Add('固定 IP 183.2.172.177 来自本次故障的成功对照，并非永久有效的百度地址。固定 IP 不保证绕过核心域名嗅探、重新解析或分流。')
    foreach ($target in @('223.5.5.5', '1.12.12.12')) {
        $ping = Invoke-PSProbe ("
`$p = [Net.NetworkInformation.Ping]::new()
try { 1..4 | ForEach-Object { `$r = `$p.Send('$target', 1000); [pscustomobject]@{ Target='$target'; Status=`$r.Status.ToString(); Milliseconds=`$r.RoundtripTime } } | ConvertTo-Json } finally { `$p.Dispose() }
") 10
        Save-Probe ('icmp-' + $target) $ping
    }
    $summary.Add('ICMP 丢包仅表示这些探测未收到回应，不等于实际业务丢包；TCP 建连成功也不等于 HTTPS 握手成功。')

    Write-Host '[4/5] 自动对比物理网卡的源地址...'
    $physical = @($network.Adapters | Where-Object { $_.HardwareInterface -eq $true -and $_.Status -eq 'Up' } | Select-Object -First 4)
    foreach ($adapter in $physical) {
        $address = $network.Addresses | Where-Object { $_.InterfaceIndex -eq $adapter.ifIndex -and $_.IPAddress -match '^\d+\.\d+\.\d+\.\d+$' -and $_.IPAddress -notmatch '^(169\.254\.|127\.)' } | Select-Object -First 1
        if (-not $address) { continue }
        $bound = Invoke-BoundedProcess $curlExe ($curlBase + @('--interface', $address.IPAddress) + $fixedArgs) 16
        Save-Probe ('https-source-if' + $adapter.ifIndex) $bound
        $summary.Add("网卡源地址对照 [$($adapter.Name) / $($address.IPAddress)]：curl 退出码 $($bound.ExitCode)。")
    }
    $summary.Add('源地址绑定不是禁用其他网卡，也不保证绕过 TUN/WFP；单网卡启停对照未自动执行。最多检查 4 张启用的物理网卡。')

    Write-Host '[5/5] 收集当前应用配置和最近日志，生成诊断包...'
    $roots = @($DataDirectory, $env:HYPOMUX_DATA_DIR, (Join-Path $env:USERPROFILE '.hypomux'), (Join-Path $env:APPDATA 'HypoMux')) | Where-Object { $_ } | Select-Object -Unique
    $rootIndex = 0
    $logCount = 0
    foreach ($root in $roots) {
        if (-not (Test-Path -LiteralPath $root -PathType Container)) { continue }
        $rootIndex++
        $summary.Add("应用数据目录 [$rootIndex]：$root")
        $settingsPath = Join-Path $root 'settings.json'
        if (Test-Path -LiteralPath $settingsPath) {
            try {
                if ((Get-Item -LiteralPath $settingsPath).Length -gt 8MB) { throw 'settings.json exceeds 8 MiB' }
                $settings = Get-Content -LiteralPath $settingsPath -Raw -Encoding UTF8 | ConvertFrom-Json
                # Allow-list avoids copying AI credentials, subscriptions and unrelated personal settings.
                $safe = $settings | Select-Object mode,strategy,strict_route,tun_stack,wfp_compatibility_state,force_tun_connectivity_bypass,dns_policy,dns_egress_mode,dns_adapter_id,dns_server,dns_servers,doh_servers,selected_adapter_ids,routing_match_order,routing_rules
                Write-ReportFile ("app-$rootIndex-settings.json") ($safe | ConvertTo-Json -Depth 30)
            } catch { $summary.Add('设置读取失败：' + $_.Exception.Message) }
        }
        $runtime = Join-Path $root 'runtime'
        if (Test-Path -LiteralPath $runtime) {
            foreach ($file in @(Get-ChildItem -LiteralPath $runtime -Filter '*.json' -File | Where-Object { $_.Name -like '*sing-box*' } | Sort-Object LastWriteTime -Descending | Select-Object -First 3)) {
                try {
                    if ($file.Length -gt 4MB) { throw 'runtime config exceeds 4 MiB' }
                    $config = Get-Content -LiteralPath $file.FullName -Raw -Encoding UTF8 | ConvertFrom-Json
                    # Exclude controller secrets and any outbound credentials entirely.
                    $safeConfig = [ordered]@{
                        SourceLastWriteTime = $file.LastWriteTime.ToString('o')
                        dns = $config.dns
                        inbounds = @($config.inbounds | Select-Object type,tag,interface_name,address,mtu,auto_route,strict_route,stack,dns_mode,dns_address,route_exclude_address)
                        outbounds = @($config.outbounds | Select-Object type,tag,server,server_port,bind_interface,inet4_bind_address,inet6_bind_address,detour)
                        route = $config.route
                    }
                    Write-ReportFile ("app-$rootIndex-" + $file.Name) ($safeConfig | ConvertTo-Json -Depth 50)
                } catch { $summary.Add('运行配置读取失败：' + $_.Exception.Message) }
            }
        }
        $logs = Join-Path $root 'logs'
        if (Test-Path -LiteralPath $logs) {
            foreach ($file in @(Get-ChildItem -LiteralPath $logs -File | Where-Object { $_.Extension -in '.log','.txt' } | Sort-Object LastWriteTime -Descending | Select-Object -First 3)) {
                try {
                    # A log can grow while collecting. Bound reads to the last 2 MiB and allow sharing.
                    $stream = [IO.File]::Open($file.FullName, [IO.FileMode]::Open, [IO.FileAccess]::Read, [IO.FileShare]::ReadWrite)
                    try {
                        $size = [int][Math]::Min($stream.Length, 2MB)
                        $null = $stream.Seek(-$size, [IO.SeekOrigin]::End)
                        $bytes = New-Object byte[] $size
                        $count = 0
                        while ($count -lt $size) { $read = $stream.Read($bytes, $count, $size - $count); if ($read -eq 0) { break }; $count += $read }
                        Write-ReportFile ("app-$rootIndex-" + $file.Name + '.tail.txt') ([Text.Encoding]::UTF8.GetString($bytes, 0, $count))
                        $logCount++
                    } finally { $stream.Dispose() }
                } catch { $summary.Add('日志读取失败：' + $_.Exception.Message) }
            }
        }
    }
    $summary.Add("已收集 $logCount 份日志尾部（每个数据目录最近 3 份，每份最多 2 MiB）。配置文件可能是历史文件，不自动视为当前核心加载的配置。")
    if ($logCount -eq 0) { $summary.Add('未找到应用日志；可使用 -DataDirectory 指定自定义数据目录。请另附应用导出的运行日志。') }
} catch {
    $summary.Add('采集中断，已保留此前结果：' + $_.Exception.Message + ' / ' + $_.ScriptStackTrace)
} finally {
    $summary.Add('结束时间：' + [DateTimeOffset]::Now.ToString('o'))
    Write-ReportFile 'SUMMARY.txt' ($summary -join "`r`n`r`n")
}

try {
    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $zip = $script:reportDir + '.zip'
    [IO.Compression.ZipFile]::CreateFromDirectory($script:reportDir, $zip)
    Write-Host "`n完成。请把这个 ZIP 发给排查人员：" -ForegroundColor Green
    Write-Host $zip -ForegroundColor Yellow
    Write-Host '中文摘要在报告目录的 SUMMARY.txt。请发送整个 ZIP，不必逐项截图。'
} catch {
    Write-Host ('压缩失败，但报告已保留，请发送整个目录：' + $script:reportDir) -ForegroundColor Yellow
    Write-Host $_.Exception.Message
    exit 1
}
