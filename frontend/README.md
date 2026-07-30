# Native WPF frontend

This directory contains the production native `.NET + WPF` desktop
application.

## Projects

- `HypoMux.EngineClient` is the toolkit-independent protocol-v1 JSONL client.
- `HypoMux.EngineClient.Smoke` verifies a real Go engine handshake, status,
  health check, graceful shutdown, and process reaping.
- `HypoMux.App` is the Windows 11 Fluent shell built with WPF UI.

The application scans active Windows adapters, persists selection and routing
rules, starts and stops ordinary proxy or TUN aggregation through protocol v1,
restores the previous WinINet proxy snapshot, generates the sing-box TUN
configuration, renders adapter telemetry, runs diagnostics, and supports a
notification-area lifecycle.

The client owns the Go child process, enforces the 1 MiB protocol limit,
negotiates protocol v1, correlates concurrent requests, validates ordered
events, drains stderr, applies deadlines, and terminates the exact process
tree if graceful shutdown fails.

## Build

```powershell
dotnet build .\frontend\HypoMux.App\HypoMux.App.csproj
dotnet build .\frontend\HypoMux.EngineClient.Smoke\HypoMux.EngineClient.Smoke.csproj
```

The SDK is pinned by `global.json`; WPF UI is pinned centrally in
`frontend/Directory.Packages.props`.

For source runs, set `HYPOMUX_ENGINE_PATH` to a compiled
`hypomux-engine.exe`. Installed builds discover `{app}\bin` automatically.
