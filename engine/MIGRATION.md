# Go engine and Wails migration status

Status: production cutover implemented.

The staged Qt/Python/WPF-to-Go-and-Wails migration is complete:

- `hypomux-engine.exe` is the only network backend.
- The desktop application uses Wails v3, Go, React, TypeScript, Vite, and
  Fluent UI React v9.
- Protocol v1 over stdio JSONL is the only UI/engine boundary.
- Go owns ordinary proxy, DNS/DoH, adapter scheduling, telemetry, diagnostics,
  the TUN channel pool, and the managed sing-box process tree.
- Python, PySide6, QFluentWidgets, Nuitka, C# WPF, and their compatibility
  fallbacks are no longer part of the source tree or installer.
- GitHub Actions tests both Go modules, generates Wails bindings, builds the
  React frontend, packages the Windows client with NSIS, and retains the
  SignPath signing path for owned executables and the installer.

The earlier phase-by-phase design records are retained under
`docs/architecture` as historical rationale. They do not describe an active
runtime fallback.
