using System.Text.Json;
using System.IO;
using HypoMux.App.Models;

namespace HypoMux.App.Services;

public sealed record AppSettings(
    string Mode = "proxy",
    int SocksPort = 10800,
    int HttpPort = 10801,
    bool Weighted = false,
    bool StrictRoute = true,
    bool CloseToTray = true,
    IReadOnlyList<string>? SelectedAdapterIds = null,
    IReadOnlyDictionary<string, int>? AdapterWeights = null,
    IReadOnlyList<RoutingRuleSetting>? RoutingRules = null);

public sealed record RoutingRuleSetting(
    string MatchType,
    string Value,
    string Outbound);

public static class AppSettingsStore
{
    private static readonly JsonSerializerOptions Options = new()
    {
        WriteIndented = true,
        PropertyNamingPolicy = JsonNamingPolicy.SnakeCaseLower,
    };

    public static string SettingsDirectory =>
        Environment.GetEnvironmentVariable("HYPOMUX_DATA_DIR") is
            { Length: > 0 } configured
            ? Path.GetFullPath(
                Environment.ExpandEnvironmentVariables(configured))
            : Path.Combine(
                Environment.GetFolderPath(
                    Environment.SpecialFolder.LocalApplicationData),
                "HypoMux");

    public static AppSettings Load()
    {
        var path = Path.Combine(SettingsDirectory, "settings.json");
        try
        {
            if (File.Exists(path))
            {
                return JsonSerializer.Deserialize<AppSettings>(
                        File.ReadAllText(path),
                        Options)
                    ?? new AppSettings();
            }
        }
        catch (IOException)
        {
        }
        catch (UnauthorizedAccessException)
        {
        }
        catch (JsonException)
        {
        }

        return new AppSettings();
    }

    public static void Save(AppSettings settings)
    {
        Directory.CreateDirectory(SettingsDirectory);
        var path = Path.Combine(SettingsDirectory, "settings.json");
        var temporary = path + ".tmp";
        File.WriteAllText(
            temporary,
            JsonSerializer.Serialize(settings, Options));
        File.Move(temporary, path, overwrite: true);
    }
}
