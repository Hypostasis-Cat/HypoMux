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
[`protocol/v1`](../protocol/v1/README.md). Go contract tests and the C# real
process smoke client validate the same contract.

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

## Production boundary

The WPF UI sends commands and renders events. Go exclusively owns proxy
listeners, source-bound DNS, connection scheduling, telemetry, sing-box
containment, and TUN cleanup. There is no alternate Python network backend.

Installed builds discover the signed engine at
`<runtime>\bin\hypomux-engine.exe`. Source builds can point the WPF client or
the C# smoke client at a local executable:

```powershell
go -C engine build -trimpath -o ..\hypomux-engine.exe .\cmd\hypomux-engine
$env:HYPOMUX_ENGINE_PATH = "$PWD\hypomux-engine.exe"
dotnet run --project .\frontend\HypoMux.App\HypoMux.App.csproj
```

The historical migration decisions remain under `docs/architecture`; the
current production layout is summarized in
[`frontend/README.md`](../frontend/README.md).
