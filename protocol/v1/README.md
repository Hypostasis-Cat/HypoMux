# HypoMux engine protocol v1

This directory is the language-neutral contract shared by the Go engine and
the production Wails desktop client.

- `manifest.json` records the advertised methods, events, lifecycle states,
  error codes, and operational semantics.
- `fixtures/messages.json` contains canonical wire-message examples.

The fixture `message` objects are complete JSONL payloads without the trailing
newline. Fixture metadata such as `name`, `kind`, and `method` is not sent on
the wire.

Protocol v1 is additive. Existing fields keep their name, JSON type, and
meaning for the lifetime of v1. New optional fields, methods, events, and
error codes may be added. Removing a field, changing its type or meaning, or
changing lifecycle ordering requires a newly negotiated protocol version.

Timestamps are UTC RFC 3339 values. Durations are integer milliseconds and
byte counters are non-negative integers. Clients must use `engine.hello`
capabilities instead of assuming every v1 engine implements every method.

`engine.hello.modes` currently advertises `proxy` and `tun_tcp_pool`. The
latter is a named-channel local SOCKS pool. Clients must inspect the additive
`engine.hello.mode_features` map before assuming a mode supports a particular
transport. The current TUN pool reports `tcp_connect` and `udp_associate`.
Both proxy modes report `ipv6_egress` when additive adapter fields
`source_ipv6` and `ipv6_if_index` are supported. Live TUN activation remains
gated on development orchestration. Both modes report `adaptive_health` when
their schedulers share continuous adapter health, and ordinary `proxy` also
reports `domain_quarantine`. TUN does not claim domain awareness because
sing-box resolves its destinations before forwarding literal IP targets.

`managed_tun_lifecycle` indicates the two-phase TUN transaction. Clients first
prepare `tun_tcp_pool`, generate configuration from the returned endpoints,
then call `tun.activate`. The host validates configuration before network
mutation, owns the exact sing-box process tree, and guarantees that
`engine.stop` deactivates TUN before stopping the pool. `tun.status` is
read-only and `tun.deactivate` is safe to retry.

`dns.resolve` and `dns.status` are available while either `proxy` or
`tun_tcp_pool` is running. In TUN mode they are the authoritative
selected-adapter DNS preflight used to build the sing-box upstream plan; the
channel listeners still accept only already-resolved IP destinations.
