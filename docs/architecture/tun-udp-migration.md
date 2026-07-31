# TUN SOCKS5 UDP and QUIC migration

> Historical migration record; the Go/WPF cutover is complete.

Status: Phase 7 implementation plan

## Scope

Phase 7 adds SOCKS5 UDP ASSOCIATE to the three Go TUN pool endpoints created
in Phase 6. Its purpose is to preserve a stable physical UDP five-tuple for
QUIC and other connection-oriented UDP protocols while retaining explicit
physical-adapter binding.

This phase does not move sing-box, TUN/WFP orchestration, FakeIP/DNS,
dual-stack egress, or the UI startup transaction. The live Qt TUN switch
remains on Python until the Go pool and the existing sing-box DNS plan can be
started and rolled back as one development-only transaction.

## Association ownership

An accepted SOCKS5 UDP ASSOCIATE belongs to its TCP control connection and to
the logical channel on which that connection arrived:

- The relay UDP socket listens only on loopback.
- The TCP peer's loopback IP is the only permitted UDP client host.
- A non-zero port in the ASSOCIATE request is enforced.
- When the requested port is zero, the first valid datagram locks the
  association to that UDP source port.
- Closing the TCP control connection closes the relay socket and every
  physical flow created by that association.

These checks prevent another local process from injecting traffic into an
existing association.

## Datagram boundary

The relay accepts only complete SOCKS5 UDP datagrams:

- RSV must be zero and FRAG must be zero; fragmentation is not implemented.
- Only literal IPv4 destinations are accepted in this phase.
- Domains are rejected because sing-box remains the only TUN DNS owner.
- IPv6 is rejected until the dual-stack migration slice. Phase 9 supersedes
  this boundary with literal, source-bound IPv6 while preserving the domain
  and fragmentation rejections.
- Destination port zero and empty payloads are rejected.
- Datagram and flow counts are bounded.

Invalid datagrams are dropped without changing scheduler state or creating a
physical socket.

## Persistent physical flows

Each association maintains a map keyed by literal destination IP and port.
The first datagram for a key:

1. Selects an adapter from that channel's isolated scheduler.
2. Creates a `udp4` socket with the adapter source IPv4 and, on Windows,
   `IP_UNICAST_IF`.
3. Connects the socket to the literal destination.
4. Retains the socket for all later datagrams to that destination.

This persistent mapping keeps source IP, source port, destination, and
adapter stable for QUIC. A physical flow is never rescheduled after it has
successfully carried traffic.

Initial socket creation may try one alternate adapter before the first
payload is accepted. Established-flow send or receive failure closes that
flow; the next client datagram may create a new flow through normal channel
scheduling.

## Limits and cleanup

- At most 256 destination flows exist in one association.
- A flow idle for 120 seconds is closed.
- The sweep interval is five seconds.
- UDP payload buffers are limited to 65,535 bytes.
- Engine stop closes listeners, control connections, relay sockets, and
  physical flow sockets before waiting for worker completion.
- Repeated close and stop operations are idempotent.

## Telemetry and protocol

Each physical UDP flow appears as an active connection with:

- `protocol=socks5_udp`
- logical `channel`
- literal destination
- selected physical adapter
- cumulative payload bytes up and down

`engine.hello.mode_features.tun_tcp_pool` becomes `["tcp_connect",
"udp_associate"]` in this phase and gains `ipv6_egress` in Phase 9. Clients
must inspect this additive feature map before
assuming UDP support; the existing `modes` array alone only proves that the
mode can start.

## Activation rule

Phase 7 removes the data-plane reason that prevented Go TUN activation, but it
does not bypass the existing DNS preflight and rollback coordinator. A later
orchestration slice will introduce a Qt development switch only after it can:

1. Produce the existing verified sing-box DNS plan.
2. Start the Go pool and receive all actual ports.
3. Generate and validate sing-box configuration.
4. Start TUN takeover.
5. Roll back sing-box and Go in the correct order on every failure.

The production Python TUN path remains unchanged until that shadow path
passes real Windows network testing.

## Exit criteria

- UDP ASSOCIATE succeeds on each Phase 6 channel endpoint.
- The same client/destination pair reuses one physical UDP socket.
- Different destinations may be assigned independently but never escape the
  channel's adapter subset.
- The first accepted client UDP endpoint is locked and spoofed local sources
  are ignored.
- Domain, fragmentation, malformed headers, port zero, empty payload, and
  flow-limit overflow create no upstream flow. The Phase 7 IPv6 rejection is
  superseded by Phase 9 source-bound IPv6.
- Replies contain a correct SOCKS5 UDP IPv4 header and return to the locked
  client endpoint.
- Idle expiry, TCP-control close, engine stop, and host shutdown release all
  UDP sockets and telemetry counters.
- Go tests, repeated concurrency/lifecycle tests, vet, Windows build, real-process
  protocol checks, and the complete Python regression suite pass.
