#requires -Version 5.1
<#
.SYNOPSIS
Diagnose or prepare an installed HypoMux 2.5+ build for an upgrade.
.DESCRIPTION
Default: read-only diagnostics (apart from report files).
-Repair: close the selected HypoMux UI, stop its verified Core service and
processes, invoke signed built-in recovery, clear ReadOnly on Core EXEs only,
and probe write access without changing file contents.
Never deletes configuration/cache, resets networking, edits ACLs, disables
security software, or stops another proxy. No automatic download or install.
Run as the SAME Windows user who uses HypoMux, elevated for -Repair.
Exit codes: 0 = completed; 1 = error or unresolved file access; 2 = cancelled.
#>
[CmdletBinding()]
param(
    [string]$InstallDir,
    [ValidateSet('Menu', 'Check', 'Repair')]
    [string]$Mode = 'Menu',
    [switch]$Repair,
    [switch]$NoPause,
    [ValidatePattern('^S-1-[0-9-]+$')]
    [string]$ExpectedUserSid,
    [string]$ReportDir = (Join-Path $env:TEMP ('HypoMux-Upgrade-' + [guid]::NewGuid().ToString('N')))
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$exitCode = 0
$transcribing = $false
$currentUserSid = [Security.Principal.WindowsIdentity]::GetCurrent().User.Value
if ($ExpectedUserSid -and $ExpectedUserSid -ne $currentUserSid) {
    Write-Host '提权后账户发生变化，已拒绝执行。请使用原 Windows 用户账户，避免恢复到错误用户的代理设置。' -ForegroundColor Red
    $null = Read-Host '按回车退出'
    exit 1
}

if ($Repair) { $Mode = 'Repair' }
if ($Mode -eq 'Menu') {
    Write-Host ''
    Write-Host '========== HypoMux 升级检查与修复工具 ==========' -ForegroundColor Cyan
    Write-Host '  1. 检查环境（不停止程序、不修改网络）'
    Write-Host '  2. 修复清理（停止 HypoMux，恢复其网络设置，检查文件占用）'
    Write-Host '  0. 退出'
    Write-Host '不会删除配置或缓存，不会重置整个网络，不会关闭 Clash 或杀毒软件。'
    do { $choice = Read-Host '请输入 1、2 或 0' } while ($choice -notin @('1', '2', '0'))
    if ($choice -eq '0') { exit 2 }
    $Mode = if ($choice -eq '2') { 'Repair' } else { 'Check' }
}
$Repair = $Mode -eq 'Repair'
$principal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    Write-Host '为完整读取 Core 服务和进程路径，需要管理员权限；检查模式仍不会修改环境。'
    if ((Read-Host '是否申请管理员权限？输入 Y 继续，其他输入退出') -notmatch '^[Yy]$') { exit 2 }
    foreach ($argumentPath in @($PSCommandPath, $ReportDir, $InstallDir)) {
        if ($argumentPath -and $argumentPath.Contains('"')) { throw '路径含不支持的引号，无法安全启动。' }
    }
    $elevatedArgs = '-NoProfile -ExecutionPolicy Bypass -File "' + $PSCommandPath + '" -Mode ' + $Mode +
        ' -ReportDir "' + $ReportDir + '" -ExpectedUserSid ' + $currentUserSid
    if ($InstallDir) { $elevatedArgs += ' -InstallDir "' + $InstallDir.TrimEnd('\') + '"' }
    try {
        # A visible console is intentional: the user must read and confirm repair.
        $elevated = Start-Process -FilePath "$env:SystemRoot\System32\WindowsPowerShell\v1.0\powershell.exe" `
            -ArgumentList $elevatedArgs -Verb RunAs -PassThru -Wait
        exit $elevated.ExitCode
    } catch { Write-Host '未获得管理员权限，未执行修复。'; exit 2 }
}

function Assert-LocalPath([string]$Path) {
    $absolute = [IO.Path]::GetFullPath($Path)
    if ($absolute -notmatch '^[A-Za-z]:\\' -or $absolute.Contains('"')) {
        throw "仅支持本地绝对路径： $absolute"
    }
    # Do not follow junctions or symbolic links into an unexpected installation.
    $cursor = $absolute
    while ($cursor) {
        if (Test-Path -LiteralPath $cursor) {
            $item = Get-Item -LiteralPath $cursor -Force
            if ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) {
                throw "检测到目录链接或重解析点，请人工检查： $cursor"
            }
        }
        $parent = [IO.Path]::GetDirectoryName($cursor)
        if ($parent -eq $cursor) { break }
        $cursor = $parent
    }
    return $absolute.TrimEnd('\')
}

function Assert-SignedRecovery([string]$Path) {
    $signature = Get-AuthenticodeSignature -LiteralPath $Path
    if ($signature.Status -ne 'Valid' -or
        $null -eq $signature.SignerCertificate -or
        $signature.SignerCertificate.Subject -notmatch '(^|,\s*)O=SignPath Foundation(,|$)') {
        throw "拒绝以管理员权限运行未经验证的恢复程序： $Path ($($signature.Status)). 请使用已验证的官方签名安装包，或将报告交给维护者。"
    }
}

function Get-OwnedProcesses([string[]]$Paths) {
    foreach ($process in @(Get-Process -Name hypomux,hypomux-engine -ErrorAction SilentlyContinue)) {
        try { $processPath = $process.Path } catch { $processPath = $null }
        if (-not $processPath) {
            throw "无法核实进程路径： $($process.ProcessName) PID=$($process.Id); 不会仅凭进程名强制结束进程。"
        }
        if ($Paths -contains [IO.Path]::GetFullPath($processPath)) { $process }
    }
}

function Stop-OwnedProcess($Process, [string[]]$Paths) {
    # Recheck identity using the original process object before termination.
    try {
        if ($Process.HasExited) { return }
        if ($Paths -notcontains [IO.Path]::GetFullPath($Process.Path)) {
            throw '进程路径发生变化。'
        }
        Write-Host "正在停止已核实路径的 HypoMux 进程 PID=$($Process.Id): $($Process.Path)"
        $Process.Kill()
        if (-not $Process.WaitForExit(10000)) { throw '进程未在 10 秒内退出。' }
    } catch { throw "无法停止进程 PID=$($Process.Id): $($_.Exception.Message)" }
}

function Invoke-Recovery([string]$Path, [string]$Argument, [string]$Label) {
    Assert-SignedRecovery $Path
    $stdout = Join-Path $ReportDir ($Label + '.stdout.log')
    $stderr = Join-Path $ReportDir ($Label + '.stderr.log')
    $process = Start-Process -FilePath $Path -ArgumentList $Argument -WindowStyle Hidden -PassThru `
        -RedirectStandardOutput $stdout -RedirectStandardError $stderr
    if (-not $process.WaitForExit(30000)) {
        $process.Kill()
        throw "$Label 超时，不能确认恢复成功，请查看报告。"
    }
    $process.WaitForExit()
    $outputText = Get-Content -LiteralPath $stdout -Raw -ErrorAction SilentlyContinue
    $errorText = Get-Content -LiteralPath $stderr -Raw -ErrorAction SilentlyContinue
    if ($outputText) { Write-Host $outputText }
    if ($errorText) { Write-Host $errorText }
    # Older desktop recovery logs errors but still exits with zero.
    if ($process.ExitCode -ne 0 -or -not [string]::IsNullOrWhiteSpace($errorText)) {
        throw "$Label 返回错误（退出码=$($process.ExitCode)）；不能确认网络已恢复。"
    }
}

try {
    if (-not [Environment]::Is64BitProcess) { throw '请使用 64 位 Windows PowerShell。' }
    New-Item -ItemType Directory -Path $ReportDir -Force | Out-Null
    Start-Transcript -LiteralPath (Join-Path $ReportDir 'report.log') -Force | Out-Null
    $transcribing = $true
    Write-Host "模式： $(if ($Repair) { '修复清理' } else { '仅检查' })"
    Write-Host "当前用户： $([Security.Principal.WindowsIdentity]::GetCurrent().Name)"
    Write-Host "时间： $(Get-Date -Format o)"

    if (-not $InstallDir) {
        $candidates = @(
            foreach ($key in @(
                'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\HypoMux',
                'HKCU:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\HypoMux'
            )) {
                if (Test-Path $key) {
                    Get-ItemPropertyValue -LiteralPath $key -Name InstallLocation -ErrorAction SilentlyContinue
                }
            }
        ) | Where-Object { $_ } | Sort-Object -Unique
        if (@($candidates).Count -ne 1) {
            Write-Host '未能唯一识别安装目录。请输入现有 HypoMux 的安装文件夹，例如 C:\Program Files\HypoMux。'
            $InstallDir = (Read-Host '安装目录（直接回车取消）').Trim().Trim('"')
            if (-not $InstallDir) { throw '未选择安装目录，已停止。' }
        } else { $InstallDir = @($candidates)[0] }
    }
    $InstallDir = Assert-LocalPath $InstallDir
    $desktopExe = Join-Path $InstallDir 'hypomux.exe'
    $appCore = Join-Path $InstallDir 'bin\hypomux-engine.exe'
    $protectedCore = Join-Path ([Environment]::GetFolderPath('CommonApplicationData')) 'HypoMux\Core\bin\hypomux-engine.exe'
    $protectedCore = Assert-LocalPath $protectedCore
    foreach ($path in @($desktopExe, $appCore)) {
        if (-not (Test-Path -LiteralPath $path -PathType Leaf)) { throw "缺少预期的 Wails 安装文件： $path" }
        $null = Assert-LocalPath $path
    }
    $desktopVersion = (Get-Item -LiteralPath $desktopExe).VersionInfo
    if ($desktopVersion.ProductName -ne 'HypoMux' -or $desktopVersion.FileMajorPart -lt 2 -or
        ($desktopVersion.FileMajorPart -eq 2 -and $desktopVersion.FileMinorPart -lt 5)) {
        throw '本工具仅支持 HypoMux Wails 2.5 及以后版本，旧版需要单独处理。'
    }
    $corePaths = @($appCore, $protectedCore) | Sort-Object -Unique
    $ownedPaths = @($desktopExe) + $corePaths
    Write-Host "安装目录： $InstallDir；版本： $($desktopVersion.FileVersion)"
    foreach ($path in $ownedPaths) {
        if (Test-Path -LiteralPath $path -PathType Leaf) {
            $item = Get-Item -LiteralPath $path -Force
            Write-Host "文件： $path；属性=$($item.Attributes)；数字签名=$((Get-AuthenticodeSignature -LiteralPath $path).Status)"
        }
    }
    $serviceInfo = Get-CimInstance Win32_Service -Filter "Name='HypoMuxCore'"
    if ($serviceInfo) {
        Write-Host "服务：状态=$($serviceInfo.State)；启动类型=$($serviceInfo.StartMode)；路径=$($serviceInfo.PathName)"
        $expectedCommands = foreach ($path in $corePaths) { "$path service"; ('"' + $path + '" service') }
        if ($expectedCommands -notcontains $serviceInfo.PathName.Trim()) {
            throw 'Core 服务路径与当前安装目录不匹配，未修改服务。'
        }
    }
    $owned = @(Get-OwnedProcesses $ownedPaths)
    foreach ($process in $owned) { Write-Host "进程：PID=$($process.Id)；会话=$($process.SessionId)；路径=$($process.Path)" }

    if ($Repair) {
        $principal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
        if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
            throw '请使用运行 HypoMux 的同一个 Windows 账户，以管理员身份启动本工具。'
        }
        Assert-SignedRecovery $desktopExe
        $recoveryCore = if (Test-Path -LiteralPath $protectedCore -PathType Leaf) { $protectedCore } else { $appCore }
        Assert-SignedRecovery $recoveryCore
        $currentSession = (Get-Process -Id $PID).SessionId
        if (@($owned | Where-Object { $_.SessionId -ne 0 -and $_.SessionId -ne $currentSession }).Count) {
            throw '其他 Windows 会话中正在运行 HypoMux，请让对应用户先退出。'
        }
        Write-Host '请先取消并关闭所有 HypoMux 安装器；尽量先正常停止加速。'
        Write-Host '修复会停止本安装目录的 HypoMux 和 Core，正在进行的下载可能断开。'
        Write-Host '不会删除配置或缓存，不会重置网络、修改目录权限或关闭安全软件。'
        if ((Read-Host '确认已关闭安装器并同意修复，请输入“确认”') -cne '确认') {
            $exitCode = 2
        } else {
            foreach ($process in @(Get-OwnedProcesses @($desktopExe))) {
                $null = $process.CloseMainWindow()
                if (-not $process.WaitForExit(5000)) { Stop-OwnedProcess $process @($desktopExe) }
            }
            if ($serviceInfo) {
                $service = Get-Service -Name HypoMuxCore
                if ($service.Status -ne 'Stopped') {
                    $service.Stop()
                    $service.WaitForStatus([ServiceProcess.ServiceControllerStatus]::Stopped, [TimeSpan]::FromSeconds(25))
                }
            }
            foreach ($process in @(Get-OwnedProcesses $corePaths)) { Stop-OwnedProcess $process $corePaths }
            Invoke-Recovery $recoveryCore 'recover' 'tun-recovery'
            Invoke-Recovery $desktopExe '--recover-network' 'proxy-recovery'
            foreach ($path in $corePaths) {
                if (Test-Path -LiteralPath $path -PathType Leaf) {
                    $item = Get-Item -LiteralPath $path -Force
                    if ($item.IsReadOnly) {
                        Write-Host "仅清除 Core 文件的只读属性： $path"
                        $item.IsReadOnly = $false
                    }
                    try {
                        $stream = [IO.File]::Open($path, [IO.FileMode]::Open, [IO.FileAccess]::Write,
                            ([IO.FileShare]::Read -bor [IO.FileShare]::Delete))
                        $stream.Dispose()
                        Write-Host "通过：可以写入（未修改文件内容）： $path"
                    } catch {
                        $exitCode = 1
                        Write-Host "失败： $path; $($_.Exception.Message); HRESULT=$($_.Exception.HResult)"
                    }
                }
            }
            if ($exitCode -eq 0) { Write-Host '升级前清理检查完成。现在可以运行官方安装器；本结果不代表升级已成功。' }
        }
    } else {
        Write-Host '检查完成。未修改进程、服务、网络设置或文件属性。'
        Write-Host '需要修复时，请再次运行本工具并选择“2. 修复清理”。'
    }
} catch {
    $exitCode = 1
    Write-Host "错误： $($_.Exception.Message)"
    Write-Host '请停止操作并将报告交给维护者，不要删除 Core 文件或关闭安全软件。'
} finally {
    if ($Repair) { Write-Host '提示：修复后 Core 可能保持停止，或此前已被安装器禁用。本工具不修改启动类型，请继续完成安装；仅重启电脑不会重新启用已禁用的服务。' }
    Write-Host "报告文件夹： $ReportDir"
    if ($transcribing) { Stop-Transcript | Out-Null }
}
if (-not $NoPause) { $null = Read-Host '按回车关闭窗口' }
exit $exitCode
