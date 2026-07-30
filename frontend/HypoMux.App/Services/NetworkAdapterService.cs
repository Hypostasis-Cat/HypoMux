using System.Net;
using System.Net.NetworkInformation;
using System.Net.Sockets;
using HypoMux.App.Models;

namespace HypoMux.App.Services;

public static class NetworkAdapterService
{
    public static IReadOnlyList<NetworkAdapterOption> GetActiveAdapters()
    {
        var result = new List<NetworkAdapterOption>();
        foreach (var adapter in NetworkInterface.GetAllNetworkInterfaces())
        {
            if (adapter.OperationalStatus != OperationalStatus.Up
                || adapter.NetworkInterfaceType == NetworkInterfaceType.Loopback
                || adapter.NetworkInterfaceType == NetworkInterfaceType.Tunnel)
            {
                continue;
            }

            IPInterfaceProperties properties;
            IPv4InterfaceProperties? ipv4Properties;
            IPv6InterfaceProperties? ipv6Properties;
            try
            {
                properties = adapter.GetIPProperties();
                ipv4Properties = properties.GetIPv4Properties();
                ipv6Properties = properties.GetIPv6Properties();
            }
            catch (NetworkInformationException)
            {
                continue;
            }

            var ipv4 = properties.UnicastAddresses
                .Select(item => item.Address)
                .FirstOrDefault(IsUsableIpv4);
            if (ipv4 is null)
            {
                continue;
            }

            var ipv6 = properties.UnicastAddresses
                .Select(item => item.Address)
                .FirstOrDefault(IsUsableIpv6);
            var dnsServers = properties.DnsAddresses
                .Where(address => address.AddressFamily == AddressFamily.InterNetwork)
                .Select(address => address.ToString())
                .Distinct(StringComparer.Ordinal)
                .ToArray();

            result.Add(new NetworkAdapterOption
            {
                Id = adapter.Id,
                Name = adapter.Name,
                SourceIp = ipv4.ToString(),
                IfIndex = ipv4Properties?.Index ?? 0,
                SourceIpv6 = ipv6?.ToString() ?? string.Empty,
                Ipv6IfIndex = ipv6Properties?.Index ?? ipv4Properties?.Index ?? 0,
                IsWireless =
                    adapter.NetworkInterfaceType == NetworkInterfaceType.Wireless80211,
                DnsServers = dnsServers,
            });
        }

        return result
            .OrderBy(item => item.IsWireless)
            .ThenBy(item => item.Name, StringComparer.CurrentCultureIgnoreCase)
            .ToArray();
    }

    private static bool IsUsableIpv4(IPAddress address) =>
        address.AddressFamily == AddressFamily.InterNetwork
        && !IPAddress.IsLoopback(address)
        && !address.Equals(IPAddress.Any)
        && !address.ToString().StartsWith("169.254.", StringComparison.Ordinal);

    private static bool IsUsableIpv6(IPAddress address) =>
        address.AddressFamily == AddressFamily.InterNetworkV6
        && !address.IsIPv6LinkLocal
        && !address.IsIPv6Multicast
        && !address.Equals(IPAddress.IPv6Any)
        && !address.Equals(IPAddress.IPv6Loopback);
}
