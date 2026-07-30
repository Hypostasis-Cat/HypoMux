using System.Text.Json;
using System.Text.Json.Nodes;
using System.IO;
using HypoMux.App.Models;
using HypoMux.EngineClient;

namespace HypoMux.App.Services;

public static class SingBoxConfigBuilder
{
    private const string DnsLocalTag = "dns-local";
    private const string DnsFakeIpTag = "dns-fakeip";

    public static string Write(
        IReadOnlyDictionary<string, string> channels,
        NetworkAdapterOption dnsAdapter,
        DnsResolveResultDto dnsResult,
        IReadOnlyList<RoutingRuleOption> routingRules,
        bool strictRoute)
    {
        var ethernetPort = ParseLoopbackPort(channels, "nic_ethernet");
        var wifiPort = ParseLoopbackPort(channels, "nic_wifi");
        var aggregationPort = ParseLoopbackPort(channels, "aggregation");
        var upstream = BuildDnsUpstream(dnsAdapter, dnsResult);
        var executablePaths = new[]
        {
            Environment.ProcessPath,
            EngineExecutableResolver.Resolve(),
            RuntimeAssetResolver.Resolve("sing-box.exe"),
        }
            .Where(path => !string.IsNullOrWhiteSpace(path))
            .Select(path => Path.GetFullPath(path!))
            .Distinct(StringComparer.OrdinalIgnoreCase)
            .ToArray();

        var config = new JsonObject
        {
            ["log"] = new JsonObject
            {
                ["level"] = "warn",
                ["timestamp"] = true,
            },
            ["dns"] = new JsonObject
            {
                ["servers"] = new JsonArray(
                    upstream,
                    new JsonObject
                    {
                        ["type"] = "fakeip",
                        ["tag"] = DnsFakeIpTag,
                        ["inet4_range"] = "198.18.0.0/15",
                        ["inet6_range"] = "fc00::/18",
                    }),
                ["rules"] = new JsonArray(
                    new JsonObject
                    {
                        ["query_type"] = new JsonArray("A", "AAAA"),
                        ["server"] = DnsFakeIpTag,
                    }),
                ["final"] = DnsLocalTag,
                ["reverse_mapping"] = true,
            },
            ["inbounds"] = new JsonArray(
                new JsonObject
                {
                    ["type"] = "tun",
                    ["tag"] = "tun-in",
                    ["interface_name"] = "HypoMux-Tun",
                    ["address"] = new JsonArray(
                        "172.19.0.1/30",
                        "fdfe:dcba:9876::1/126"),
                    ["mtu"] = 1492,
                    ["auto_route"] = true,
                    ["strict_route"] = strictRoute,
                    ["stack"] = "system",
                }),
            ["outbounds"] = new JsonArray(
                SocksOutbound("nic_ethernet", ethernetPort),
                SocksOutbound("nic_wifi", wifiPort),
                SocksOutbound("aggregation", aggregationPort),
                new JsonObject
                {
                    ["type"] = "direct",
                    ["tag"] = "direct",
                }),
            ["route"] = new JsonObject
            {
                ["auto_detect_interface"] = true,
                ["default_domain_resolver"] = DnsLocalTag,
                ["final"] = "aggregation",
                ["rules"] = BuildRouteRules(executablePaths, routingRules),
            },
        };

        var directory = Path.Combine(
            AppSettingsStore.SettingsDirectory,
            "runtime");
        Directory.CreateDirectory(directory);
        var path = Path.Combine(directory, "sing-box.json");
        var temporary = path + ".tmp";
        File.WriteAllText(
            temporary,
            config.ToJsonString(new JsonSerializerOptions
            {
                WriteIndented = true,
            }));
        File.Move(temporary, path, overwrite: true);
        return path;
    }

    private static JsonArray BuildRouteRules(
        IReadOnlyList<string> executablePaths,
        IReadOnlyList<RoutingRuleOption> routingRules)
    {
        var result = new JsonArray(
            new JsonObject
            {
                ["action"] = "sniff",
                ["timeout"] = "300ms",
            },
            new JsonObject
            {
                ["process_path"] = JsonSerializer.SerializeToNode(
                    executablePaths),
                ["outbound"] = "direct",
            },
            new JsonObject
            {
                ["process_name"] = new JsonArray(
                    "HypoMux.exe",
                    "hypomux-engine.exe",
                    "sing-box.exe"),
                ["outbound"] = "direct",
            },
            new JsonObject
            {
                ["port"] = new JsonArray(53),
                ["action"] = "hijack-dns",
            },
            new JsonObject
            {
                ["protocol"] = new JsonArray("dns"),
                ["action"] = "hijack-dns",
            });

        var allowedOutbounds = new HashSet<string>(
            ["direct", "nic_ethernet", "nic_wifi", "aggregation"],
            StringComparer.Ordinal);
        foreach (var rule in routingRules)
        {
            var value = rule.Value.Trim();
            var outbound = rule.Outbound.Trim().ToLowerInvariant();
            if (value.Length == 0 || !allowedOutbounds.Contains(outbound))
            {
                continue;
            }

            JsonObject? routeRule = rule.MatchType.Trim().ToLowerInvariant() switch
            {
                "process" or "process_name" => new JsonObject
                {
                    ["process_name"] = new JsonArray(value),
                    ["outbound"] = outbound,
                },
                "domain" => new JsonObject
                {
                    ["domain"] = new JsonArray(value),
                    ["domain_suffix"] = new JsonArray($".{value.TrimStart('.')}"),
                    ["outbound"] = outbound,
                },
                "ip" or "ip_cidr" => new JsonObject
                {
                    ["ip_cidr"] = new JsonArray(value),
                    ["outbound"] = outbound,
                },
                _ => null,
            };
            if (routeRule is not null)
            {
                result.Add(routeRule);
            }
        }

        result.Add(new JsonObject
        {
            ["action"] = "resolve",
            ["server"] = DnsLocalTag,
            ["strategy"] = "prefer_ipv4",
        });
        return result;
    }

    private static JsonObject BuildDnsUpstream(
        NetworkAdapterOption adapter,
        DnsResolveResultDto result)
    {
        var endpoint = ParseEndpoint(
            result.Server,
            string.Equals(result.Transport, "doh", StringComparison.OrdinalIgnoreCase)
                ? 443
                : 53);
        if (string.Equals(
                result.Transport,
                "doh",
                StringComparison.OrdinalIgnoreCase))
        {
            var separator = result.Server.IndexOf('@');
            if (separator <= 0)
            {
                throw new InvalidOperationException(
                    $"Go engine returned an invalid DoH endpoint: {result.Server}");
            }

            var host = result.Server[..separator];
            endpoint = ParseEndpoint(result.Server[(separator + 1)..], 443);
            return new JsonObject
            {
                ["type"] = "https",
                ["tag"] = DnsLocalTag,
                ["server"] = endpoint.Host,
                ["server_port"] = endpoint.Port,
                ["path"] = "/dns-query",
                ["tls"] = new JsonObject
                {
                    ["enabled"] = true,
                    ["server_name"] = host,
                },
                ["bind_interface"] = adapter.Name,
                ["inet4_bind_address"] = adapter.SourceIp,
            };
        }

        return new JsonObject
        {
            ["type"] = "udp",
            ["tag"] = DnsLocalTag,
            ["server"] = endpoint.Host,
            ["server_port"] = endpoint.Port,
            ["bind_interface"] = adapter.Name,
            ["inet4_bind_address"] = adapter.SourceIp,
        };
    }

    private static JsonObject SocksOutbound(string tag, int port) =>
        new()
        {
            ["type"] = "socks",
            ["tag"] = tag,
            ["server"] = "127.0.0.1",
            ["server_port"] = port,
            ["version"] = "5",
        };

    private static int ParseLoopbackPort(
        IReadOnlyDictionary<string, string> channels,
        string name)
    {
        if (!channels.TryGetValue(name, out var value))
        {
            throw new InvalidOperationException(
                $"Go engine did not return the {name} channel.");
        }

        var endpoint = ParseEndpoint(value, 0);
        if (endpoint.Host is not ("127.0.0.1" or "localhost")
            || endpoint.Port <= 0)
        {
            throw new InvalidOperationException(
                $"Go engine returned an unsafe {name} endpoint.");
        }

        return endpoint.Port;
    }

    private static (string Host, int Port) ParseEndpoint(
        string value,
        int defaultPort)
    {
        var separator = value.LastIndexOf(':');
        if (separator <= 0)
        {
            return (value, defaultPort);
        }

        return int.TryParse(value[(separator + 1)..], out var port)
            && port is > 0 and <= 65535
            ? (value[..separator], port)
            : throw new InvalidOperationException(
                $"Invalid endpoint returned by the Go engine: {value}");
    }
}
