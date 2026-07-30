# Default network backend cutover

Status: implemented in Phase 12 with an explicit Python rollback

## Decision

The persistent Go host is now the default backend for ordinary proxy and TUN
network sessions. The selection policy is process-local and is not written to
the user's application configuration:

| `HYPOMUX_NETWORK_BACKEND` | Behavior |
| --- | --- |
| unset or `auto` | Start the packaged Go host and use each Go mode when its complete capability set is available; otherwise fall back before acquiring session resources |
| `go` | Require the complete negotiated Go capability set; a missing or incompatible host fails the requested session instead of silently mixing owners |
| `python` or `legacy` | Do not start the Go host; use the previous Python workers and `TunManager` |

The historical `HYPOMUX_GO_ENGINE_DEV`, `HYPOMUX_GO_PROXY_DEV`, and
`HYPOMUX_GO_TUN_DEV` variables remain accepted for old scripts. Because Go is
now enabled by default, they are equivalent to `auto` and do not turn a
per-mode script into strict global selection. An explicit
`HYPOMUX_NETWORK_BACKEND` value is authoritative.

## Packaged host discovery

`HYPOMUX_ENGINE_PATH` remains the highest-priority source-build override.
Without it, the UI searches in this order:

1. `<runtime>\bin\hypomux-engine.exe` — signed installer layout;
2. `<runtime>\hypomux-engine.exe`;
3. `<runtime>\dist\hypomux-engine.exe`;
4. `<runtime>\engine\hypomux-engine.exe`.

The first location closes the production gap left by the migration-only
resolver, which did not search the installer's `bin` directory.

## Atomic selection boundary

The host handshake itself does not acquire proxy, TUN, WFP, route, or adapter
resources. Each session independently requires its full negotiated mode
contract:

- ordinary proxy: `engine.start`, `engine.stop`, `engine.telemetry`, proxy mode
  features, IPv6, adaptive health, and domain quarantine;
- TUN: the TCP/UDP pool feature set plus managed lifecycle and all three
  `tun.*` methods.

In `auto`, missing capability negotiation selects the complete Python session
before either implementation starts listeners. After a Go session starts,
errors stay inside the Go rollback transaction; the UI never switches owners
mid-session.

In strict `go`, the same pre-acquisition check returns a visible error. This
mode is intended for release qualification because it cannot hide an
incomplete package behind the compatibility backend.

## Startup and process safety

The default startup path no longer executes an image-wide
`taskkill /IM sing-box.exe`. Go-owned sing-box processes are contained by an
exact Windows Job object, while startup cleanup is scoped to:

- the adapter whose friendly name is exactly `HypoMux-Tun` and whose instance
  identifies Wintun;
- IPv4 and IPv6 default routes on that exact interface.

The legacy image-wide cleanup remains reachable only through the explicit
Python rollback path. This preserves emergency compatibility without allowing
the default backend to terminate another application's sing-box process.

## Compatibility and removal boundary

This phase changes the production preference, not the protocol. Existing
protocol-v1 fixtures and WPF DTOs remain valid.

The Python proxy pool and `TunManager` remain packaged as a bounded rollback
for the unified Windows physical test pass. They can be removed only after:

1. strict `go` passes ordinary proxy and TUN startup/stop/restart;
2. selected-adapter TCP, UDP, IPv4, IPv6, DNS, WFP strict-route, sleep/resume,
   crash, upgrade, and uninstall recovery pass on a real Windows system;
3. the installed and signed `{app}\bin\hypomux-engine.exe` path is verified;
4. no rollback leaves a route, Wintun adapter, sing-box process, or engine host.
