# HypoMux engine protocol v1

This directory is the language-neutral contract for the Go engine, the
current Python client, and the future C# WPF client.

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
gated on development orchestration.
