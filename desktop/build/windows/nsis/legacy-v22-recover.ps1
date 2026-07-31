[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$InstallRoot,

    [Parameter(Mandatory = $true)]
    [string]$DataRoot
)

$ErrorActionPreference = 'Stop'

function Stop-LegacyOwnedProcesses {
    $ownedRoot = [System.IO.Path]::GetFullPath($InstallRoot).TrimEnd('\') + '\'
    $processes = Get-CimInstance Win32_Process -Filter "Name = 'sing-box.exe' OR Name = 'diagnostic.exe'" -ErrorAction SilentlyContinue
    foreach ($process in $processes) {
        $executablePath = [string]$process.ExecutablePath
        if ($executablePath.StartsWith($ownedRoot, [System.StringComparison]::OrdinalIgnoreCase)) {
            Invoke-CimMethod -InputObject $process -MethodName Terminate -ErrorAction SilentlyContinue | Out-Null
        }
    }
}

function Remove-LegacyTunState {
    Get-NetRoute -AddressFamily IPv4 -ErrorAction SilentlyContinue |
        Where-Object InterfaceAlias -EQ 'HypoMux-Tun' |
        Remove-NetRoute -Confirm:$false -ErrorAction SilentlyContinue
    Get-NetRoute -AddressFamily IPv6 -ErrorAction SilentlyContinue |
        Where-Object InterfaceAlias -EQ 'HypoMux-Tun' |
        Remove-NetRoute -Confirm:$false -ErrorAction SilentlyContinue

    $devices = Get-PnpDevice -Class Net -ErrorAction SilentlyContinue |
        Where-Object FriendlyName -EQ 'HypoMux-Tun'
    foreach ($device in $devices) {
        Disable-PnpDevice -InstanceId $device.InstanceId -Confirm:$false -ErrorAction SilentlyContinue | Out-Null
        & "$env:SystemRoot\System32\pnputil.exe" /remove-device $device.InstanceId 2>&1 | Out-Null
    }
}

function Clear-LegacyOwnedProxy {
    $socksPort = 10800
    $httpPort = 10801
    $configPath = Join-Path $DataRoot 'config.json'
    if (Test-Path -LiteralPath $configPath -PathType Leaf) {
        try {
            $config = Get-Content -LiteralPath $configPath -Raw -Encoding UTF8 | ConvertFrom-Json
            if ([int]$config.socks_port -ge 1 -and [int]$config.socks_port -le 65534) {
                $socksPort = [int]$config.socks_port
            }
            if ([int]$config.http_port -ge 1 -and [int]$config.http_port -le 65534) {
                $httpPort = [int]$config.http_port
            }
        }
        catch {
            Write-Warning "Could not read the legacy HypoMux port configuration: $($_.Exception.Message)"
        }
    }

    $expected = "http=127.0.0.1:$httpPort;https=127.0.0.1:$httpPort;socks=127.0.0.1:$socksPort"
    $internetSettings = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Internet Settings'
    $current = Get-ItemProperty -LiteralPath $internetSettings -ErrorAction SilentlyContinue
    if ($null -eq $current -or [string]$current.ProxyServer -ne $expected) {
        return
    }

    Set-ItemProperty -LiteralPath $internetSettings -Name ProxyEnable -Value 0
    Set-ItemProperty -LiteralPath $internetSettings -Name ProxyServer -Value ''
    Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;
public static class HypoMuxLegacyWinInet {
    [DllImport("wininet.dll", SetLastError = true)]
    public static extern bool InternetSetOption(IntPtr internet, int option, IntPtr buffer, int length);
}
'@
    [HypoMuxLegacyWinInet]::InternetSetOption([IntPtr]::Zero, 39, [IntPtr]::Zero, 0) | Out-Null
    [HypoMuxLegacyWinInet]::InternetSetOption([IntPtr]::Zero, 37, [IntPtr]::Zero, 0) | Out-Null
}

try {
    Stop-LegacyOwnedProcesses
    Remove-LegacyTunState
    Clear-LegacyOwnedProxy
}
catch {
    Write-Error "Legacy HypoMux network recovery failed: $($_.Exception.Message)"
    exit 1
}
