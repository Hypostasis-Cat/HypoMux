using System.ComponentModel;
using System.IO;
using System.Runtime.InteropServices;
using System.Text.Json;
using Microsoft.Win32;

namespace HypoMux.App.Services;

public static class SystemProxyService
{
    private const string InternetSettings =
        @"Software\Microsoft\Windows\CurrentVersion\Internet Settings";
    private const int InternetOptionSettingsChanged = 39;
    private const int InternetOptionRefresh = 37;
    private static string OwnershipMarker =>
        Path.Combine(AppSettingsStore.SettingsDirectory, "proxy-owned");

    public static void Enable(int httpPort, int socksPort)
    {
        using var key = Registry.CurrentUser.OpenSubKey(
            InternetSettings,
            writable: true)
            ?? throw new InvalidOperationException(
                "Windows Internet Settings registry key is unavailable.");
        var valueNames = key.GetValueNames().ToHashSet(
            StringComparer.OrdinalIgnoreCase);
        var snapshot = new ProxySnapshot(
            valueNames.Contains("ProxyEnable"),
            key.GetValue("ProxyEnable") as int?,
            valueNames.Contains("ProxyServer"),
            key.GetValue("ProxyServer") as string,
            valueNames.Contains("ProxyOverride"),
            key.GetValue("ProxyOverride") as string);
        Directory.CreateDirectory(AppSettingsStore.SettingsDirectory);
        File.WriteAllText(
            OwnershipMarker,
            JsonSerializer.Serialize(snapshot));
        key.SetValue("ProxyEnable", 1, RegistryValueKind.DWord);
        key.SetValue(
            "ProxyServer",
            $"http=127.0.0.1:{httpPort};https=127.0.0.1:{httpPort};"
            + $"socks=127.0.0.1:{socksPort}",
            RegistryValueKind.String);
        key.SetValue(
            "ProxyOverride",
            "<local>;localhost;127.*;10.*;172.16.*;172.17.*;172.18.*;"
            + "172.19.*;172.2*;172.30.*;172.31.*;192.168.*",
            RegistryValueKind.String);
        NotifyWindows();
    }

    public static void Disable()
    {
        if (!File.Exists(OwnershipMarker))
        {
            return;
        }

        using var key = Registry.CurrentUser.OpenSubKey(
            InternetSettings,
            writable: true);
        if (key is null)
        {
            TryDeleteOwnershipMarker();
            return;
        }

        ProxySnapshot? snapshot = null;
        try
        {
            snapshot = JsonSerializer.Deserialize<ProxySnapshot>(
                File.ReadAllText(OwnershipMarker));
        }
        catch (IOException)
        {
        }
        catch (JsonException)
        {
        }

        if (snapshot is null)
        {
            key.SetValue("ProxyEnable", 0, RegistryValueKind.DWord);
            key.SetValue("ProxyServer", string.Empty, RegistryValueKind.String);
        }
        else
        {
            RestoreValue(
                key,
                "ProxyEnable",
                snapshot.HasProxyEnable,
                snapshot.ProxyEnable ?? 0,
                RegistryValueKind.DWord);
            RestoreValue(
                key,
                "ProxyServer",
                snapshot.HasProxyServer,
                snapshot.ProxyServer ?? string.Empty,
                RegistryValueKind.String);
            RestoreValue(
                key,
                "ProxyOverride",
                snapshot.HasProxyOverride,
                snapshot.ProxyOverride ?? string.Empty,
                RegistryValueKind.String);
        }

        NotifyWindows();
        TryDeleteOwnershipMarker();
    }

    public static void RestoreIfOwned()
    {
        if (!File.Exists(OwnershipMarker))
        {
            return;
        }

        Disable();
    }

    private static void TryDeleteOwnershipMarker()
    {
        try
        {
            File.Delete(OwnershipMarker);
        }
        catch (IOException)
        {
        }
    }

    private static void RestoreValue(
        RegistryKey key,
        string name,
        bool existed,
        object value,
        RegistryValueKind kind)
    {
        if (existed)
        {
            key.SetValue(name, value, kind);
        }
        else
        {
            key.DeleteValue(name, throwOnMissingValue: false);
        }
    }

    private static void NotifyWindows()
    {
        if (!InternetSetOption(
                IntPtr.Zero,
                InternetOptionSettingsChanged,
                IntPtr.Zero,
                0)
            || !InternetSetOption(
                IntPtr.Zero,
                InternetOptionRefresh,
                IntPtr.Zero,
                0))
        {
            throw new Win32Exception(Marshal.GetLastWin32Error());
        }
    }

    [DllImport("wininet.dll", SetLastError = true)]
    [return: MarshalAs(UnmanagedType.Bool)]
    private static extern bool InternetSetOption(
        IntPtr internet,
        int option,
        IntPtr buffer,
        int bufferLength);

    private sealed record ProxySnapshot(
        bool HasProxyEnable,
        int? ProxyEnable,
        bool HasProxyServer,
        string? ProxyServer,
        bool HasProxyOverride,
        string? ProxyOverride);
}
