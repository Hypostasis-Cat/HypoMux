# HypoMux Go Engine

This directory is the migration boundary between the desktop UI and the
network engine. The executable provides a versioned IPC contract, owns the
production source-bound ICMP diagnostic, and contains the staged SOCKS5/HTTP
TCP proxy with source-bound DNS/DoH plus the staged TUN multi-port TCP/UDP
pool. Routing and TUN lifecycle ownership remain incremental.

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

The only non-standard dependency is the version-pinned
`golang.org/x/sys/windows` package. It is used for Windows process identity
and IP Helper API access.

Run the production-compatible one-shot diagnostic:

```powershell
.\dist\hypomux-engine.exe diagnose --src-ip 192.168.1.100 --target-ip 223.5.5.5
```

The command emits one JSON object with the same status, loss, latency, jitter,
and counter fields consumed by the desktop UI.

## Protocol v1

The `serve` command reads newline-delimited JSON requests from standard input
and writes newline-delimited JSON responses and events to standard output.
Standard output is reserved for protocol messages; diagnostics belong on
standard error.

The language-neutral method manifest and canonical wire examples live in
[`protocol/v1`](../protocol/v1/README.md). Go and Python tests validate this
same contract; the future C# client will consume it as well.

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

Protocol-v1 methods:

- `engine.hello`: negotiate the protocol and inspect capabilities.
- `engine.status`: read the canonical engine lifecycle state.
- `engine.start`: start either the ordinary SOCKS5/HTTP TCP proxy or the
  named-channel TUN TCP/UDP pool with explicit adapters, source IPv4 addresses,
  ports, and scheduling weights.
- `engine.stop`: close listeners and cancel all accepted and relayed
  connections with a bounded shutdown.
- `engine.telemetry`: read cumulative per-adapter bytes, active connection
  counts, optional connection details, and DNS counters.
- `dns.resolve`: resolve an A or AAAA record through a running engine and a
  selected adapter for diagnostics.
- `dns.status`: inspect DNS policy, upstreams, cache, in-flight work, and
  counters without starting a query.
- `health.check`: verify that the engine process and protocol loop respond.
- `diagnostic.run`: run the same source-bound ICMP diagnostic exposed by the
  one-shot `diagnose` command.
- `host.shutdown`: acknowledge and gracefully stop the engine host process.

Reserved lifecycle states are `stopped`, `starting`, `running`, `degraded`,
`stopping`, and `failed`. `engine.hello.modes` advertises `proxy` and
`tun_tcp_pool`. `engine.hello.mode_features` reports the exact transports
available in each mode. The TUN pool owns literal-IPv4 SOCKS CONNECT and
source-validated UDP ASSOCIATE; live TUN activation, WFP, and routing remain
unavailable until their orchestration contracts are implemented.

Ordinary proxy domain targets are resolved by the Go engine before the
adapter-bound TCP dial. `auto` races the built-in DoH endpoints and uses only
source-bound traditional DNS if DoH is unavailable; explicit providers remain
strict. No ordinary proxy DNS path uses the Windows system resolver. TUN DNS
remains owned by sing-box until its later orchestration phase.

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
move behind this boundary in migration slices. The current Python/Qt path
remains the production default.

## Development connection

Build the host:

```powershell
cd engine
go build -trimpath -o ..\dist\hypomux-engine.exe .\cmd\hypomux-engine
cd ..
```

Then launch the source application with the hidden development connection:

```powershell
$env:HYPOMUX_GO_ENGINE_DEV = "1"
$env:HYPOMUX_ENGINE_PATH = "$PWD\dist\hypomux-engine.exe"
..\venv\Scripts\python.exe main.py
```

The PySide6 UI performs hello and health supervision through the
toolkit-independent `engine_client` package. To route ordinary proxy mode
through Go while retaining the Python fallback:

```powershell
$env:HYPOMUX_GO_PROXY_DEV = "1"
$env:HYPOMUX_ENGINE_PATH = "$PWD\dist\hypomux-engine.exe"
python main.py
```

This flag also starts the persistent host. TUN mode continues to use the
Python `MultiPortProxyWorker` and `TunManager` by default. To route the live
TUN TCP/UDP outbound pool through Go while keeping sing-box DNS/TUN/WFP
ownership and the pre-acquisition Python fallback:

```powershell
$env:HYPOMUX_GO_TUN_DEV = "1"
$env:HYPOMUX_ENGINE_PATH = "$PWD\dist\hypomux-engine.exe"
python main.py
```

The TUN switch is accepted only when protocol negotiation reports
`tun_tcp_pool` with both `tcp_connect` and `udp_associate`.

Unset the variables to return to the normal production path:

```powershell
Remove-Item Env:HYPOMUX_GO_ENGINE_DEV -ErrorAction SilentlyContinue
Remove-Item Env:HYPOMUX_GO_PROXY_DEV -ErrorAction SilentlyContinue
Remove-Item Env:HYPOMUX_GO_TUN_DEV -ErrorAction SilentlyContinue
Remove-Item Env:HYPOMUX_ENGINE_PATH -ErrorAction SilentlyContinue
```
