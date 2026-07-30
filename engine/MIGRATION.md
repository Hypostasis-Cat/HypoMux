# Go engine and WPF migration status

Status: production cutover implemented.

The staged Qt/Python-to-Go migration is complete:

- `hypomux-engine.exe` is the only network backend.
- The desktop application is native C#/.NET WPF.
- Protocol v1 over stdio JSONL is the only UI/engine boundary.
- Go owns ordinary proxy, DNS/DoH, adapter scheduling, telemetry, diagnostics,
  the TUN channel pool, and the managed sing-box process tree.
- Python, PySide6, QFluentWidgets, Nuitka, and their compatibility fallback
  are no longer part of the source tree or installer.
- GitHub Actions builds the Go engine, exercises it through the real C#
  process client, publishes a self-contained `win-x64` WPF application, and
  packages the unified publish directory with Inno Setup.

The earlier phase-by-phase design records are retained under
`docs/architecture` as historical rationale. They do not describe an active
runtime fallback.
