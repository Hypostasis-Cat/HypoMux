# HypoMux Go Engine

This directory is the migration boundary between the desktop UI and the
network engine. The executable provides a versioned IPC contract, owns the
production source-bound ICMP diagnostic, the default SOCKS5/HTTP proxy with
source-bound DNS/DoH, the TUN multi-port TCP/UDP pool, and managed
sing-box/TUN/WFP/route lifecycle.

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
  named-channel TUN TCP/UDP pool with explicit adapters, required source IPv4,
  optional source IPv6, interface indices, ports, and scheduling weights.
- `engine.stop`: close listeners and cancel all accepted and relayed
  connections with a bounded shutdown.
- `engine.telemetry`: read cumulative per-adapter bytes, active connection
  counts, optional connection details, DNS counters, adaptive health state,
  and ordinary-proxy domain quarantines.
- `tun.activate`: validate and start sing-box under Go-owned process and
  network-resource containment after a TUN pool has returned its endpoints.
- `tun.status`: inspect the managed sidecar state, PID, timestamps, exit code,
  and last error.
- `tun.deactivate`: stop the exact sidecar process tree and clean only the
  HypoMux TUN routes and adapter while leaving the prepared pool available for
  the enclosing transaction.
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
literal-IPv6 SOCKS CONNECT plus source-validated dual-stack UDP ASSOCIATE.
Go owns the sing-box process and its TUN/WFP/route lifetime by default;
sing-box still implements DNS, FakeIP, Wintun, and strict routing.

Ordinary proxy domain targets are resolved by the Go engine before the
adapter-bound TCP dial. A records remain preferred, with AAAA fallback only
through adapters that advertise an explicit IPv6 source. `auto` races the
built-in DoH endpoints and uses only source-bound traditional DNS if DoH is
unavailable; explicit providers remain strict. No ordinary proxy DNS path
uses the Windows system resolver. TUN DNS remains owned by sing-box.

Adapter-local transport failures use a shared bounded backoff across ordinary
proxy and every TUN channel. Expired cooldowns become recovery candidates and
a successful connection restores the adapter immediately. Ordinary proxy
domain isolation requires repeated comparative evidence: one adapter fails
while another succeeds for the same domain. All-adapter failures are never
learned as a domain quarantine, and TUN literal-IP traffic never manufactures
domain state.

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
move behind this boundary in migration slices. Go is the default network
backend; Python remains a bounded pre-acquisition compatibility rollback until
the unified Windows qualification pass is complete.

## Runtime backend selection

Build the host:

```powershell
cd engine
go build -trimpath -o ..\dist\hypomux-engine.exe .\cmd\hypomux-engine
cd ..
```

Installed builds discover the signed host at
`<runtime>\bin\hypomux-engine.exe` and use it by default. Source builds can
point at a local executable:

```powershell
$env:HYPOMUX_ENGINE_PATH = "$PWD\dist\hypomux-engine.exe"
python main.py
```

The default `auto` policy starts the host, negotiates capabilities, and selects
Go independently for ordinary proxy and TUN sessions. A missing or incomplete
mode falls back to the complete Python implementation before listeners or TUN
resources are acquired.

Use strict Go mode during release qualification:

```powershell
$env:HYPOMUX_NETWORK_BACKEND = "go"
python main.py
```

Strict mode reports a visible pre-acquisition error instead of hiding a
missing packaged host or capability behind Python. To use the bounded
compatibility rollback:

```powershell
$env:HYPOMUX_NETWORK_BACKEND = "python"
python main.py
```

Unset the variables to return to the default `auto` policy:

```powershell
Remove-Item Env:HYPOMUX_NETWORK_BACKEND -ErrorAction SilentlyContinue
Remove-Item Env:HYPOMUX_ENGINE_PATH -ErrorAction SilentlyContinue
```

The older `HYPOMUX_GO_ENGINE_DEV`, `HYPOMUX_GO_PROXY_DEV`, and
`HYPOMUX_GO_TUN_DEV` flags remain accepted by compatibility helpers. They are
equivalent to the default `auto` behavior and do not enable strict mode.

The complete cutover and compatibility-removal rules are documented in
[default network backend cutover](../docs/architecture/default-network-backend-cutover.md).

Generate the non-destructive strict-Go packaging and residue report with:

```powershell
python -m engine_client.qualification `
  --engine ..\hypomux-engine.exe `
  --output ..\qualification\preflight.json
```

This command starts no proxy or TUN mode. Release qualification adds
`--require-elevated --require-signed` and follows the physical matrix in
[strict-Go Windows qualification](../docs/architecture/strict-go-windows-qualification.md).
