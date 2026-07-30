# TUN orchestration migration

Status: implemented behind the Phase 8 development flag

## Scope

Phase 8 connects the Phase 6/7 Go TUN pool to the existing Qt TUN startup
transaction behind `HYPOMUX_GO_TUN_DEV=1`. It does not change the production
default, move sing-box/Wintun/WFP process ownership, or make Go responsible
for TUN DNS.

The purpose of this phase is to exercise the real mixed control plane:

- Qt coordinates the transaction.
- A Python DNS planner verifies and selects the exact sing-box upstream.
- Go owns all local SOCKS TCP/UDP data-plane endpoints.
- sing-box owns FakeIP, DNS, TUN, routes, and WFP strict routing.

The same endpoint and DNS-plan DTOs remain suitable for the future WPF
coordinator.

## Development selection

`HYPOMUX_GO_TUN_DEV=1` is an environment-only switch. It is never written to
user configuration.

The Go TUN path is selected only when the persistent engine bridge is
connected and advertises:

- `engine.start`, `engine.stop`, and `engine.telemetry`
- mode `tun_tcp_pool`
- features `tcp_connect` and `udp_associate`

If those prerequisites are absent before the transaction starts, Qt logs the
reason and uses the existing Python `MultiPortProxyWorker`. After an explicit
Go transaction has begun, failures are surfaced and rolled back rather than
silently starting a second pool.

## DNS ownership

The existing verified TUN DNS behavior is preserved:

1. Probe selected adapters concurrently for source-bound DoH and traditional
   DNS.
2. Respect explicit provider, `off`, and controlled legacy-mode overrides.
3. Select one exact upstream and physical binding.
4. Pass that plan to sing-box configuration generation.
5. Keep monitoring the selected DoH egress and request one controlled legacy
   restart after three consecutive failures.

The planner opens no SOCKS listeners and relays no application traffic.
Go TUN endpoints accept only literal IPv4 destinations resolved by sing-box.

## Startup transaction

Resources are acquired in this order:

1. Complete the existing route/WFP/TUN preflight.
2. Produce a verified sing-box DNS plan.
3. Start `mode=tun_tcp_pool` and receive all three actual Go endpoints.
4. Generate sing-box configuration using those exact ports and the verified
   DNS plan.
5. Run sing-box configuration validation.
6. Start sing-box and wait for stable TUN takeover.
7. Complete the existing bidirectional connectivity validation.

The pool emits `started_ok` only after both DNS preparation and Go listener
startup succeed. Therefore the existing main-window transition cannot start
sing-box with a partial or guessed endpoint set.

## Failure and rollback matrix

| Failure point | Acquired resources | Required rollback |
| --- | --- | --- |
| DNS preparation | planner only | cancel planner |
| Go start | host may be `failed` | issue `engine.stop` to clear state |
| Config generation/check | Go pool | stop Go pool |
| sing-box startup | Go pool and partial kernel process | stop sing-box, then Go |
| Connectivity validation | active TUN and Go pool | stop sing-box/routes, then Go |
| Go bridge/data-plane loss | active TUN | immediately run the same full rollback |

Stop requests are idempotent. The UI first removes the current worker identity
to ignore queued stale callbacks, requests sing-box shutdown and route
cleanup, then requests Go engine stop. Retired worker references remain alive
until their `finished` signal so Qt cannot destroy a running thread adapter.

## Worker compatibility surface

The Go TUN worker mirrors the subset already consumed by `MainWindow`:

- lifecycle: `start`, `stop`, `isRunning`, `wait`, `finished`
- signals: log, traffic, connectivity, DNS compatibility request,
  `started_ok`, `stopped`, and error
- snapshots: actual listen ports, selected adapters, DNS mode, and sing-box
  DNS plan

This keeps orchestration independent of whether the current pool is Python or
Go and reduces later WPF migration to replacing the signal adapter, not
rewriting network ownership.

## Exit criteria

- With no TUN development flag, startup behavior and process ownership are
  byte-for-byte unchanged at the selection boundary.
- Missing engine, capability, mode, or UDP feature falls back to Python before
  any Go resource is acquired.
- The Go worker starts only after DNS preparation and returns all actual
  channel ports.
- Config-generation, sing-box, health-check, bridge-loss, manual-stop, and
  shutdown paths all stop the Go engine and leave no listener process.
- DoH health degradation requests the existing one-shot legacy restart.
- Telemetry reaches the current Qt dashboard and real upstream bytes produce
  connectivity evidence.
- Unit tests cover selection, DTO mapping, DNS preparation, start failure,
  normal stop, telemetry, and compatibility restart signaling.
- Go tests/vet/build and the complete Python regression suite pass.
