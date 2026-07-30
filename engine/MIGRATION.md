# Engine migration plan

HypoMux will migrate incrementally. A release must never replace both the UI
and the network engine in one step, and the existing Python engine remains the
fallback until the Go implementation passes the same real-network scenarios.

## Frontend target and readiness rule

The target frontend is native WPF with WPF UI (`lepoco/WPF-UI`) as its only
application-wide visual library. Native WPF controls, layout, binding, and
templates remain the foundation; a second complete visual theme must not be
introduced.

WPF is not a final cleanup step. Every engine slice must expose
toolkit-independent DTOs, capability negotiation, reconstructible status,
defined cancellation/lifecycle semantics, and canonical JSON fixtures that a
C# client can consume without reading Python or Qt code.

The complete visual-stack decision, ownership boundary, stages, and exit
criteria are documented in
[WPF frontend migration](../docs/architecture/wpf-frontend-migration.md).

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

## Phase 5: source-bound DNS and DoH

Goal: remove implicit system DNS from the staged Go proxy while preserving
the selected-adapter routing and the existing Python production fallback.

Deliverables:

- Adapter-bound traditional UDP/TCP DNS and certificate-verified DoH over
  literal endpoint IPs.
- `auto`, `off`, and explicit-provider policy with no Windows system-resolver
  fallback.
- Bounded positive TTL cache and shared in-flight lookups scoped by adapter.
- Domain resolution before every adapter-bound proxy dial, including
  pre-relay failover.
- `dns.resolve`, `dns.status`, DNS telemetry, and canonical protocol fixtures.
- Qt development configuration bridge; existing Python and sing-box TUN DNS
  remain the production fallback.

Exit criteria:

- A repository search finds no hostname-based upstream dial in the Go proxy.
- Unit and integration tests cover wire parsing, policy, cache, cancellation,
  domain proxying, and protocol compatibility.
- A real compiled Windows engine proves source-bound DNS on a physical
  adapter.
- With no development flag, production proxy and TUN DNS behavior is
  unchanged.

The detailed invariants and contract are documented in
[DNS/DoH engine migration](../docs/architecture/dns-engine-migration.md).

## Phase 6: TUN multi-port TCP outbound pool

Goal: move the independently testable TCP half of the TUN local SOCKS pool
onto the shared Go transport without breaking the still-Python UDP/QUIC path.

Deliverables:

- `tun_tcp_pool` engine mode with three named, loopback-only SOCKS listeners.
- Per-channel adapter subsets and isolated round-robin/weighted schedulers.
- Atomic multi-listener startup, preferred-port fallback, and actual endpoint
  reporting.
- Literal-IPv4-only TUN CONNECT handling so sing-box remains the sole TUN DNS
  owner.
- Channel identity in connection telemetry and toolkit-independent protocol
  DTOs for the future WPF client.
- Explicitly unchanged production TUN activation until source-bound UDP is
  implemented.

Exit criteria:

- Channel isolation, collision fallback, atomic rollback, TCP relay,
  telemetry, unsupported command/address rejection, and bounded stop are
  covered by Go integration tests.
- Protocol contract fixtures describe both ordinary proxy and TUN TCP pool
  modes.
- With no development flags, current Python TUN, UDP, DNS, sing-box and WFP
  behavior is unchanged.

The detailed ownership, lifecycle, and activation rules are documented in
[TUN multi-port TCP pool migration](../docs/architecture/tun-tcp-pool-migration.md).

## Phase 7: source-bound SOCKS5 UDP and QUIC

Goal: complete the Go TUN pool data plane with persistent, source-validated
UDP flows while preserving QUIC five-tuple stability.

Deliverables:

- Loopback-only UDP ASSOCIATE relays scoped to their TCP control connection.
- First-client endpoint locking and rejection of other local UDP sources.
- Persistent per-destination physical sockets bound to the selected adapter.
- Bounded flow count, 120-second idle expiry, and deterministic teardown.
- UDP flow adapter/channel/byte telemetry.
- `mode_features` capability negotiation for TCP and UDP support.
- Explicit rejection of fragmented, domain, IPv6, malformed, and empty TUN
  datagrams until their dedicated slices.

Exit criteria:

- Real UDP echo integration proves request/reply relay and stable physical
  flow reuse.
- Tests cover source validation, channel isolation, packet validation, flow
  limits, expiry, TCP-control close, and engine shutdown.
- Production Python TUN and sing-box DNS behavior remains unchanged.

The detailed invariants are documented in
[TUN SOCKS5 UDP and QUIC migration](../docs/architecture/tun-udp-migration.md).

## Phase 8: development TUN orchestration

Goal: connect the completed Go TCP/UDP pool to the real Qt/sing-box TUN
transaction without changing the production default.

Deliverables:

- Environment-only `HYPOMUX_GO_TUN_DEV` selection with mode/feature
  negotiation and pre-acquisition Python fallback.
- A DNS-planner facade that preserves the verified sing-box DNS plan without
  opening Python SOCKS listeners.
- A Qt-compatible Go TUN worker exposing actual channel ports, DNS state,
  telemetry, connectivity evidence, and bounded lifecycle methods.
- Existing sing-box configuration, WFP compatibility restart, connectivity
  validation, and full rollback paths reused for both pool implementations.
- Explicit engine-state cleanup after partial Go startup failure.

Exit criteria:

- Tests cover selection, DNS-plan handoff, channel DTOs, stop, failure, and
  telemetry behavior.
- The Go path can be activated only when TCP and UDP TUN features are both
  advertised.
- Without the flag, Python TUN behavior is unchanged.
- No failure can leave sing-box active without a pool or the Go engine
  running after TUN rollback.

The transaction and ownership rules are documented in
[TUN orchestration migration](../docs/architecture/tun-orchestration-migration.md).

## Later migration slices

1. IPv4/IPv6 dual-stack behavior.
2. Continuous adapter health and domain-aware scheduling.
3. Move sing-box/WFP lifecycle ownership after development orchestration
   shadow testing.
4. Make the Go engine the default for all network behavior after shadow-mode
   and rollback testing.

Each slice keeps a Python fallback until its unit, integration, and Windows
network tests pass. Each slice also has to meet the frontend-readiness rule
above; WPF shell work can begin once the shared protocol fixtures are stable.
