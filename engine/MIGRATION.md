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

## Later migration slices

1. Connection ownership, counters, and telemetry.
2. TCP proxy transport and cancellation.
3. DNS resolution, DoH fallback, caching, and leak prevention.
4. SOCKS5 UDP with source validation.
5. IPv4/IPv6 dual-stack behavior.
6. Adapter scheduling and health checks.
7. TUN/sing-box and WFP orchestration.
8. Make Go the default after shadow-mode and rollback testing.
9. Connect the same protocol to the future WPF UI.

Each slice keeps a Python fallback until its unit, integration, and Windows
network tests pass.
