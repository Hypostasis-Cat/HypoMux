param(
    [Parameter(Mandatory = $true)]
    [string]$CoreRoot,

    [Parameter(Mandatory = $true)]
    [ValidateSet('Prepare', 'Finalize')]
    [string]$Phase
)

$ErrorActionPreference = 'Stop'

$programData = [System.IO.Path]::GetFullPath(
    [Environment]::GetFolderPath([Environment+SpecialFolder]::CommonApplicationData)
).TrimEnd('\')
$expectedRoot = [System.IO.Path]::GetFullPath(
    (Join-Path $programData 'HypoMux\Core')
).TrimEnd('\')
$requestedRoot = [System.IO.Path]::GetFullPath($CoreRoot).TrimEnd('\')
if (-not $requestedRoot.Equals($expectedRoot, [StringComparison]::OrdinalIgnoreCase)) {
    throw "Core root must be the protected ProgramData path: $expectedRoot"
}

$administrators = [System.Security.Principal.SecurityIdentifier]::new(
    [System.Security.Principal.WellKnownSidType]::BuiltinAdministratorsSid,
    $null
)
$localSystem = [System.Security.Principal.SecurityIdentifier]::new(
    [System.Security.Principal.WellKnownSidType]::LocalSystemSid,
    $null
)
$users = [System.Security.Principal.SecurityIdentifier]::new(
    [System.Security.Principal.WellKnownSidType]::BuiltinUsersSid,
    $null
)

function Assert-NotReparsePoint([string]$Path) {
    if (-not (Test-Path -LiteralPath $Path)) {
        return
    }
    $item = Get-Item -LiteralPath $Path -Force
    if (($item.Attributes -band [System.IO.FileAttributes]::ReparsePoint) -ne 0) {
        throw "Protected Core path must not be a reparse point: $Path"
    }
}

function New-ProtectedDirectoryAcl {
    $acl = [System.Security.AccessControl.DirectorySecurity]::new()
    $acl.SetOwner($administrators)
    $acl.SetAccessRuleProtection($true, $false)
    $inheritance = [System.Security.AccessControl.InheritanceFlags]::ContainerInherit -bor
        [System.Security.AccessControl.InheritanceFlags]::ObjectInherit
    $propagation = [System.Security.AccessControl.PropagationFlags]::None
    $allow = [System.Security.AccessControl.AccessControlType]::Allow
    $acl.AddAccessRule([System.Security.AccessControl.FileSystemAccessRule]::new(
        $localSystem,
        [System.Security.AccessControl.FileSystemRights]::FullControl,
        $inheritance,
        $propagation,
        $allow
    ))
    $acl.AddAccessRule([System.Security.AccessControl.FileSystemAccessRule]::new(
        $administrators,
        [System.Security.AccessControl.FileSystemRights]::FullControl,
        $inheritance,
        $propagation,
        $allow
    ))
    $acl.AddAccessRule([System.Security.AccessControl.FileSystemAccessRule]::new(
        $users,
        [System.Security.AccessControl.FileSystemRights]::ReadAndExecute,
        $inheritance,
        $propagation,
        $allow
    ))
    return $acl
}

function New-ProtectedFileAcl {
    $acl = [System.Security.AccessControl.FileSecurity]::new()
    $acl.SetOwner($administrators)
    $acl.SetAccessRuleProtection($true, $false)
    $allow = [System.Security.AccessControl.AccessControlType]::Allow
    $acl.AddAccessRule([System.Security.AccessControl.FileSystemAccessRule]::new(
        $localSystem,
        [System.Security.AccessControl.FileSystemRights]::FullControl,
        $allow
    ))
    $acl.AddAccessRule([System.Security.AccessControl.FileSystemAccessRule]::new(
        $administrators,
        [System.Security.AccessControl.FileSystemRights]::FullControl,
        $allow
    ))
    $acl.AddAccessRule([System.Security.AccessControl.FileSystemAccessRule]::new(
        $users,
        [System.Security.AccessControl.FileSystemRights]::ReadAndExecute,
        $allow
    ))
    return $acl
}

$hypomuxRoot = Join-Path $programData 'HypoMux'
$binRoot = Join-Path $requestedRoot 'bin'
$directories = @($hypomuxRoot, $requestedRoot, $binRoot)

if ($Phase -eq 'Prepare') {
    foreach ($directory in $directories) {
        Assert-NotReparsePoint $directory
        if (-not (Test-Path -LiteralPath $directory -PathType Container)) {
            New-Item -ItemType Directory -Path $directory | Out-Null
        }
        Assert-NotReparsePoint $directory
        Set-Acl -LiteralPath $directory -AclObject (New-ProtectedDirectoryAcl)
    }

    foreach ($name in @('hypomux-engine.exe', 'sing-box.exe', 'wintun.dll', 'libcronet.dll')) {
        $path = Join-Path $binRoot $name
        Assert-NotReparsePoint $path
        if (Test-Path -LiteralPath $path) {
            Remove-Item -LiteralPath $path -Force
        }
    }
    exit 0
}

foreach ($directory in $directories) {
    Assert-NotReparsePoint $directory
    Set-Acl -LiteralPath $directory -AclObject (New-ProtectedDirectoryAcl)
}

foreach ($name in @('hypomux-engine.exe', 'sing-box.exe', 'wintun.dll')) {
    $path = Join-Path $binRoot $name
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
        throw "Required protected Core payload is missing: $path"
    }
    Assert-NotReparsePoint $path
    Set-Acl -LiteralPath $path -AclObject (New-ProtectedFileAcl)
}

$optionalCronet = Join-Path $binRoot 'libcronet.dll'
if (Test-Path -LiteralPath $optionalCronet -PathType Leaf) {
    Assert-NotReparsePoint $optionalCronet
    Set-Acl -LiteralPath $optionalCronet -AclObject (New-ProtectedFileAcl)
}
