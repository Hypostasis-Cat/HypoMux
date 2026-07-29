# HypoMux Go Engine

This directory is the migration boundary between the desktop UI and the
network engine. Phase 1 does **not** replace the production Python engine.
It provides a separately buildable process, a versioned IPC contract, and a
tested lifecycle model so the current PySide6 UI and a future WPF UI can use
the same engine API.

See [MIGRATION.md](MIGRATION.md) for the staged migration plan and the mapping
from current Qt signals to transport-independent engine events.

## Build and test

```powershell
cd engine
go mod download
go test ./...
go vet ./...
go build -trimpath -o ..\dist\hypomux-engine.exe .\cmd\hypomux-engine
```

The only non-standard dependency in phase 1 is the version-pinned
`golang.org/x/sys/windows` package. It is currently used for a read-only
process elevation check.

## Protocol v1

The `serve` command reads newline-delimited JSON requests from standard input
and writes newline-delimited JSON responses and events to standard output.
Standard output is reserved for protocol messages; diagnostics belong on
standard error.

Request:

```json
{"protocol":1,"id":"hello-1","method":"engine.hello","params":{}}
```

Success response:

```json
{"protocol":1,"id":"hello-1","result":{"protocol_version":1}}
```

Error response:

```json
{"protocol":1,"id":"bad-1","error":{"code":"method_not_found","message":"unknown method"}}
```

Event:

```json
{"protocol":1,"sequence":1,"event":"host.exiting","data":{"reason":"requested"}}
```

Phase 1 methods:

- `engine.hello`: negotiate the protocol and inspect capabilities.
- `engine.status`: read the canonical engine lifecycle state.
- `health.check`: verify that the engine process and protocol loop respond.
- `host.shutdown`: acknowledge and gracefully stop the engine host process.

Reserved lifecycle states are `stopped`, `starting`, `running`, `degraded`,
`stopping`, and `failed`. Actual proxy/TUN start and stop commands are
deliberately deferred until their configuration and rollback contracts are
fully specified.

## Compatibility policy

- Every request and message declares `protocol`.
- Existing protocol-v1 fields keep their meaning for the lifetime of v1.
- New optional response fields may be added without increasing the version.
- Breaking field or lifecycle changes require a new protocol version.
- UI clients must use `engine.hello` capabilities instead of assuming that a
  newly introduced method exists.

## Migration boundary

The UI may send commands and render events, but must not own engine cleanup,
DNS fallback, routing rollback, or connection accounting. Those behaviors
move behind this boundary in later phases. Until then, the current Python/Qt
path remains the production default.
