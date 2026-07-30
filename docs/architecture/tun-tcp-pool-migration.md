# TUN multi-port TCP pool migration

Status: Phase 6 implemented; UDP ownership continues in Phase 7

## Scope

Phase 6 moves the TCP half of the TUN local SOCKS outbound pool from Python
to the shared Go transport. It does not move sing-box, Wintun/WFP lifecycle,
SOCKS5 UDP, QUIC, or the TUN DNS owner.

The production TUN path stays on `MultiPortProxyWorker` until the UDP slice is
available. The new Go pool is exposed through the protocol and verified as an
independent component first, so enabling it cannot silently discard UDP.

## Ownership

| Concern | Phase 6 owner |
| --- | --- |
| TUN device, routes, WFP and sing-box process | Python `TunManager` |
| FakeIP and TUN DNS | sing-box, using the existing verified DNS plan |
| Three loopback TCP SOCKS listeners | Go engine |
| TCP source/interface binding and failover | Go engine |
| UDP ASSOCIATE and QUIC | Go engine beginning with Phase 7 |
| UI lifecycle and rollback coordinator | current Qt UI; future WPF client uses the same DTOs |

sing-box resolves domain names before sending TUN TCP traffic to the local
pool. Consequently, `tun_tcp_pool` accepts only literal IPv4 SOCKS CONNECT
targets. A domain request is rejected instead of causing a second resolver
path. IPv6 remains a later, explicit dual-stack slice.

## Channel model

`engine.start` accepts `mode=tun_tcp_pool` and exactly the three required
entries in its `channels` array. Each channel contains a stable logical name,
a preferred loopback port, and the adapter names it may schedule:

- `nic_ethernet`: wired adapters.
- `nic_wifi`: wireless adapters.
- `aggregation`: all selected adapters.

Dynamic `nic_*` route tags continue to map to one of these three logical
channels in sing-box; the engine does not create unbounded listeners from
user-controlled tags.

Adapter names in a channel must exist in the top-level adapter collection.
Names and non-zero preferred ports are unique. Missing or extra logical
channels, empty adapter subsets, unknown adapters, and external listen
addresses are rejected before any socket is opened.

## Startup transaction

1. Validate the complete configuration and construct all channel schedulers.
2. Bind every preferred loopback TCP port.
3. If a preferred port is unavailable, retry that channel with an ephemeral
   loopback port.
4. If any listener still fails, close every listener already opened and
   return `start_failed`.
5. Publish the actual endpoint map and transition to `running` only after all
   listeners are ready.

No sing-box or route takeover may begin until the UI has received this
successful endpoint map and generated a configuration from the actual ports.

## Scheduling and telemetry

Each accepted connection retains its channel identity. Selection and the
single pre-relay failover are limited to that channel's adapter subset.
Schedulers do not share round-robin or weighted cursor state across channels.

Telemetry remains cumulative per physical adapter and gains an optional
`channel` field on active connection snapshots. The start result, status, and
canonical fixtures expose the channel endpoint map as toolkit-independent
DTOs for both Qt and WPF.

## Stop and rollback

Normal stop order for a future integrated TUN path is:

1. Stop sing-box so it cannot create new pool connections.
2. Stop the Go pool, close all listeners, cancel handshakes and relays, and
   wait with a fixed deadline.
3. Clean the TUN adapter and routes.

Startup rollback uses the reverse set of resources actually acquired. The Go
engine's own startup is atomic, so a failed pool start has no listener to
clean from the UI.

An engine host shutdown performs the same bounded pool stop. Repeated stop is
idempotent.

## Activation rule

This phase deliberately does not add a UI switch that routes live TUN traffic
through the TCP-only pool. It would either break QUIC or require two local
pools and protocol-specific route duplication. Phase 7 adds source-bound
SOCKS5 UDP to the same channel endpoints. A later orchestration slice may
select the Go TUN pool end to end only after it also preserves DNS preflight
and transactional rollback.

The ordinary proxy development flag and the production Python TUN path are
unchanged.

## Exit criteria

- Three channel listeners start atomically and return their actual ports.
- Preferred-port collision falls back only for the affected channel.
- A channel cannot select an adapter outside its declared subset.
- SOCKS CONNECT relays through a real channel endpoint and records the
  channel plus the selected physical adapter.
- Domain, IPv6, unknown channel adapter, and invalid listener configurations
  fail explicitly. The original Phase 6 TCP-only UDP rejection is superseded
  by the Phase 7 UDP contract.
- Stop closes handshake-only and active relay clients within the existing
  five-second engine deadline.
- `engine.hello`, `engine.start`, `engine.status`, and canonical fixtures
  describe `tun_tcp_pool` without Qt/Python-specific types.
- Go tests, vet and Windows build plus the complete Python regression suite
  pass.
