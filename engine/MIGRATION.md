# Engine migration plan

HypoMux will migrate incrementally. A release must never replace both the UI
and the network engine in one step, and the existing Python engine remains the
fallback until the Go implementation passes the same real-network scenarios.

## Phase 1: establish the boundary

Goal: make the future engine independently buildable and give every UI a
small, versioned contract that does not depend on Qt.

Deliverables:

- Independent `engine/go.mod` with an exact Go toolchain and dependency
  version.
- A Windows `hypomux-engine.exe` process with no CGO dependency.
- Protocol-v1 handshake, capability discovery, status, health check, and
  graceful host shutdown over newline-delimited JSON.
- A canonical, tested lifecycle state machine.
- Windows elevation detection through `golang.org/x/sys/windows`.
- A separate GitHub Actions validation workflow.
- No changes to the current PySide6 startup, TUN, routing, signing, or
  installer behavior.

Exit criteria:

- `go test ./...`, `go vet ./...`, and a Windows release build pass.
- A real child process answers `engine.hello` and `engine.status`, then exits
  with code 0 after `host.shutdown`.
- Unknown methods and unsupported protocol versions return structured errors
  without crashing the host.
- Existing Python tests still pass.

## Event contract reserved for phase 2

The current Qt signals will be represented by transport-independent events.
These names are documented now but are not advertised as capabilities until
their Go producers exist.

| Current Python/Qt signal | Future protocol event |
| --- | --- |
| `started_ok` | `engine.state_changed` with `state=running` |
| `stopped` | `engine.state_changed` with `state=stopped` |
| `error_signal` | `engine.error`, followed by an appropriate state change |
| `log_signal` | `log.record` |
| `traffic_signal` | `telemetry.snapshot` |
| `connectivity_signal` | `network.connectivity_changed` |
| `dns_compatibility_required` | `dns.fallback_required` |

Every event will carry a monotonically increasing sequence number. Telemetry
may be dropped or coalesced by a slow UI; lifecycle and error events may not.

## Phase 2: connect without switching defaults

Goal: prove the real UI-to-host process boundary without moving any production
network behavior.

Deliverables:

- A toolkit-independent `engine_client` Python package for protocol v1.
- Request deadlines, structured local/remote errors, capability negotiation,
  bounded JSONL messages, stderr draining, and event sequence validation.
- Graceful `host.shutdown` followed by bounded terminate/kill fallback.
- A thin `ui/engine_bridge.py` adapter that only translates callbacks into Qt
  signals.
- An explicit `HYPOMUX_GO_ENGINE_DEV=1` development flag. The host is never
  started by default and the flag is not stored in user configuration.
- Fake-host tests for crash, timeout, malformed output, stderr pressure, and
  forced shutdown.
- A real compiled child-process test for hello, health, status, events, and
  clean exit in the Go engine workflow.

Exit criteria:

- With no development flag, application startup and acceleration behavior are
  unchanged and no Go process is created.
- With the flag enabled, the UI completes protocol negotiation without
  blocking the Qt thread.
- A failed or unresponsive host cannot hang the UI or survive application
  shutdown.
- The client and Go engine do not import PySide6; only the adapter depends on
  Qt.
- `engine.start` and `engine.stop` remain undefined until configuration
  validation and rollback semantics are specified.
- The Go executable is not included in the production installer during this
  phase.

## Phase 3: production diagnostic ownership

Goal: replace the standalone Rust diagnostic with a signed Go engine while
preserving the existing UI result contract.

Deliverables:

- Source-bound IPv4 ICMP probing through Windows `IcmpSendEcho2Ex`.
- Shared diagnostic implementation for the one-shot `diagnose` command and
  protocol-v1 `diagnostic.run`.
- Python diagnostic runner switched to `hypomux-engine.exe diagnose`.
- Go engine built, signed, and installed under `{app}\bin`.
- Upgrade cleanup for the legacy root-level `diagnostic.exe`.
- Rust/Cargo sources and the checked-in Rust executable removed.

Exit criteria:

- Diagnostic status thresholds and JSON fields remain compatible.
- Go unit tests, Python runner tests, real-process tests, and build
  configuration tests pass.
- The production build and both SignPath modes sign `hypomux-engine.exe`.
- A repository search finds no live Rust/Cargo build path.

## Phase 4: staged TCP proxy ownership

Goal: move ordinary proxy-mode TCP connections into Go without changing the
production default or the TUN pipeline.

Deliverables:

- Loopback-only SOCKS5 CONNECT and HTTP/HTTPS CONNECT listeners.
- Source IPv4 and Windows `IP_UNICAST_IF` binding for every upstream socket.
- Round-robin and smooth weighted scheduling with one pre-relay failover.
- A connection registry with live cancellation, cumulative bytes, active
  counts, and optional connection snapshots.
- `engine.start`, `engine.stop`, and `engine.telemetry` protocol methods plus
  lifecycle events.
- A Qt-compatible development worker selected by
  `HYPOMUX_GO_PROXY_DEV=1`; Python remains the automatic fallback.

Exit criteria:

- SOCKS5 and HTTP CONNECT relay through a real compiled child process.
- Listener startup is atomic and shutdown closes handshake-only and active
  relay clients within a fixed deadline.
- Per-adapter byte and connection telemetry reaches the existing UI contract.
- With no development flag, production proxy and TUN behavior are unchanged.

## Later migration slices

1. DNS resolution, DoH fallback, caching, and leak prevention.
2. Move the TUN multi-port TCP outbound pool onto the shared Go transport.
3. SOCKS5 UDP with source validation.
4. IPv4/IPv6 dual-stack behavior.
5. Continuous adapter health and domain-aware scheduling.
6. TUN/sing-box and WFP orchestration.
7. Make the Go engine the default for all network behavior after shadow-mode
   and rollback testing.
8. Connect the same protocol to the future WPF UI.

Each slice keeps a Python fallback until its unit, integration, and Windows
network tests pass.
