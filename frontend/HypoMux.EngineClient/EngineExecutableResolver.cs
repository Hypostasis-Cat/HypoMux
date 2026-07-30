namespace HypoMux.EngineClient;

public static class EngineExecutableResolver
{
    public const string EnginePathVariable = "HYPOMUX_ENGINE_PATH";

    public static string? Resolve(string? applicationDirectory = null)
    {
        var configured = Environment.GetEnvironmentVariable(EnginePathVariable);
        if (!string.IsNullOrWhiteSpace(configured))
        {
            var expanded = Environment.ExpandEnvironmentVariables(
                configured.Trim().Trim('"'));
            return File.Exists(expanded) ? Path.GetFullPath(expanded) : null;
        }

        var candidates = new List<string>();
        var root = Path.GetFullPath(applicationDirectory ?? AppContext.BaseDirectory);
        candidates.Add(Path.Combine(root, "bin", "hypomux-engine.exe"));
        candidates.Add(Path.Combine(root, "hypomux-engine.exe"));

        var current = new DirectoryInfo(root);
        for (var depth = 0; current is not null && depth < 8; depth++, current = current.Parent)
        {
            candidates.Add(Path.Combine(current.FullName, "bin", "hypomux-engine.exe"));
            candidates.Add(Path.Combine(current.FullName, "engine", "hypomux-engine.exe"));
            candidates.Add(Path.Combine(current.FullName, "dist", "hypomux-engine.exe"));
        }

        var programFiles = Environment.GetFolderPath(
            Environment.SpecialFolder.ProgramFiles);
        if (!string.IsNullOrWhiteSpace(programFiles))
        {
            candidates.Add(
                Path.Combine(
                    programFiles,
                    "HypoMux",
                    "bin",
                    "hypomux-engine.exe"));
        }

        return candidates
            .Select(Path.GetFullPath)
            .Distinct(StringComparer.OrdinalIgnoreCase)
            .FirstOrDefault(File.Exists);
    }
}
