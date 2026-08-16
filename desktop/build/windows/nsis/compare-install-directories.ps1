param(
    [Parameter(Mandatory = $true)]
    [string]$PreviousPath,

    [Parameter(Mandatory = $true)]
    [string]$RequestedPath
)

$ErrorActionPreference = 'Stop'

Add-Type -TypeDefinition @'
using System;
using System.ComponentModel;
using System.Runtime.InteropServices;
using Microsoft.Win32.SafeHandles;

public static class HypoMuxDirectoryIdentity
{
    private const uint ShareReadWriteDelete = 0x00000007;
    private const uint OpenExisting = 3;
    private const uint FileFlagBackupSemantics = 0x02000000;

    [StructLayout(LayoutKind.Sequential)]
    private struct ByHandleFileInformation
    {
        public uint FileAttributes;
        public uint CreationTimeLow;
        public uint CreationTimeHigh;
        public uint LastAccessTimeLow;
        public uint LastAccessTimeHigh;
        public uint LastWriteTimeLow;
        public uint LastWriteTimeHigh;
        public uint VolumeSerialNumber;
        public uint FileSizeHigh;
        public uint FileSizeLow;
        public uint NumberOfLinks;
        public uint FileIndexHigh;
        public uint FileIndexLow;
    }

    [DllImport("kernel32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
    private static extern SafeFileHandle CreateFileW(
        string fileName,
        uint desiredAccess,
        uint shareMode,
        IntPtr securityAttributes,
        uint creationDisposition,
        uint flagsAndAttributes,
        IntPtr templateFile
    );

    [DllImport("kernel32.dll", SetLastError = true)]
    [return: MarshalAs(UnmanagedType.Bool)]
    private static extern bool GetFileInformationByHandle(
        SafeFileHandle file,
        out ByHandleFileInformation information
    );

    private static ByHandleFileInformation Inspect(string path)
    {
        using (SafeFileHandle handle = CreateFileW(
            path,
            0,
            ShareReadWriteDelete,
            IntPtr.Zero,
            OpenExisting,
            FileFlagBackupSemantics,
            IntPtr.Zero
        ))
        {
            if (handle.IsInvalid)
            {
                throw new Win32Exception(Marshal.GetLastWin32Error(), "Open directory identity");
            }
            ByHandleFileInformation information;
            if (!GetFileInformationByHandle(handle, out information))
            {
                throw new Win32Exception(Marshal.GetLastWin32Error(), "Read directory identity");
            }
            return information;
        }
    }

    public static bool Same(string first, string second)
    {
        ByHandleFileInformation left = Inspect(first);
        ByHandleFileInformation right = Inspect(second);
        return left.VolumeSerialNumber == right.VolumeSerialNumber &&
            left.FileIndexHigh == right.FileIndexHigh &&
            left.FileIndexLow == right.FileIndexLow;
    }
}
'@

function Get-NormalizedFullPath([string]$Path) {
    $full = [System.IO.Path]::GetFullPath($Path)
    $root = [System.IO.Path]::GetPathRoot($full)
    if ($full.Equals($root, [StringComparison]::OrdinalIgnoreCase)) {
        return $root
    }
    return $full.TrimEnd('\')
}

try {
    $previousFull = Get-NormalizedFullPath $PreviousPath
    $requestedFull = Get-NormalizedFullPath $RequestedPath
    if ($previousFull.Equals($requestedFull, [StringComparison]::OrdinalIgnoreCase)) {
        exit 0
    }
    if (-not (Test-Path -LiteralPath $previousFull -PathType Container) -or
        -not (Test-Path -LiteralPath $requestedFull -PathType Container)) {
        exit 1
    }
    if ([HypoMuxDirectoryIdentity]::Same($previousFull, $requestedFull)) {
        exit 0
    }
    exit 1
} catch {
    [Console]::Error.WriteLine($_.Exception.Message)
    exit 2
}
