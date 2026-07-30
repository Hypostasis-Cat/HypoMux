# Managed TUN lifecycle migration

Status: implemented behind the Phase 11 development capability gate

## Scope

Phase 11 moves sing-box process supervision and the resulting TUN, WFP, route,
and adapter lifetime into the persistent Go host. Qt remains the transaction
coordinator and still owns:

- administrator and foreign-route preflight;
- selected-adapter and same-gateway warnings;
- source-bound DNS-plan verification;
- sing-box configuration generation;
- startup and periodic end-to-end connectivity decisions;
- the one-shot strict-route and legacy-DNS compatibility restart policy.

sing-box remains the implementation of FakeIP, DNS, Wintun, automatic routes,
and WFP strict routing. Go now owns when that implementation may start, how it
is contained, and how all of its resources are stopped and cleaned.

The production Python path remains unchanged.

## Why activation is two phase

The named Go TUN pool may replace a preferred local port when it is already in
use. Qt cannot generate a correct sing-box configuration until the engine has
returned all three actual endpoints. A single `engine.start` request would
either guess ports or move routing configuration semantics into Go too early.

The transaction is therefore:

1. `engine.start(mode=tun_tcp_pool)` prepares the source-bound TCP/UDP pool.
2. Qt verifies DNS and writes a configuration using the returned endpoints.
3. `tun.activate` validates that exact configuration and activates sing-box.
4. Qt completes its real TUN connectivity validation.

`tun.activate` failure stops both a partial sidecar and the prepared pool
inside the Go host. Qt never falls back to a Python sidecar after Go resources
have been acquired.

## Host-owned process safety

Before any network cleanup or TUN takeover, Go runs:

`sing-box check --disable-color -c <absolute-config-path>`

A failed or timed-out check leaves the current network untouched.

On Windows, the running sidecar is assigned to a Job object configured with
`JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`. If the Go host crashes or is forcibly
terminated, Windows terminates that exact process tree. Normal and forced
shutdown target the owned PID/Job; the managed path never kills every
`sing-box.exe` process by image name.

stdout and stderr are bounded and forwarded as protocol `log.record` events.
An unexpected exit becomes `tun.state_changed(state=failed)`, removes the
TUN resources, stops the sole outbound pool, and moves the engine to `failed`.

## Scoped network cleanup

Cleanup is intentionally narrower than a generic VPN sweep:

- only default routes whose interface alias is exactly `HypoMux-Tun`;
- both `0.0.0.0/0` and `::/0`;
- only network devices whose friendly name is exactly `HypoMux-Tun` and whose
  instance ID identifies Wintun.

The command does not remove other VPN adapters and does not perform an
image-name process kill.

Configuration validation precedes startup cleanup. Cleanup runs before
activation, after every process exit, during `tun.deactivate`, during
`engine.stop`, and when the host input pipe closes.

## Stop and rollback order

The Go host always stops resources in this order:

1. close the Job/terminate the owned sing-box process tree;
2. remove the exact HypoMux TUN default routes and Wintun device;
3. close all Go TUN TCP/UDP listeners and flows;
4. transition the engine to `stopped`, or `failed` if cleanup could not be
   completed.

This prevents both dangerous intermediate states: an active default route
without an egress pool, and a running pool left behind after TUN shutdown.

## Protocol contract

Protocol v1 adds:

- `tun.activate`
- `tun.status`
- `tun.deactivate`
- `tun.state_changed`
- `log.record`
- error code `tun_failed`
- mode feature `managed_tun_lifecycle`

The feature is advertised only for `tun_tcp_pool`. Qt requires the three
methods and the feature together with the existing TCP, UDP, IPv6, and
adaptive-health features before starting any Go TUN resource.

The status DTO contains sidecar state, PID, UTC start/exit timestamps, exit
code, last error, and config path. It is toolkit-independent and can be
consumed unchanged by the future WPF client.

## Rollback boundary

`HYPOMUX_GO_TUN_DEV=1` remains the only selection switch. Missing or partial
capability negotiation falls back to the complete Python pool and
`TunManager` before resource acquisition. Once the Go pool starts, all
subsequent failures use Go rollback; no mixed-owner retry is allowed.
