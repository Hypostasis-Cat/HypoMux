# Dual-stack physical egress migration

Status: implemented behind the Phase 9 development capability gate

## Scope

Phase 9 makes the Go proxy and named TUN pool capable of source-bound IPv6
egress. It does not move DNS, sing-box, TUN, route, or WFP ownership.

The local SOCKS/HTTP listeners and SOCKS UDP ASSOCIATE relay remain IPv4
loopback endpoints. Only the physical upstream socket family changes. This
preserves the existing Qt and sing-box transaction while allowing literal
IPv6 TCP/UDP targets and IPv6-only ordinary proxy destinations.

The Go engine executable is also added to the sing-box direct-process
exclusion by exact path and process name. Its physical upstream sockets must
never be recaptured by the TUN and routed back into their own SOCKS pool.

## Adapter contract

The protocol-v1 adapter DTO gains two additive optional fields:

- `source_ipv6`: preferred non-link-local unicast IPv6 source address
- `ipv6_if_index`: authoritative Windows IPv6 interface index

`source_ip` remains the required IPv4 source and keeps its existing meaning.
DNS planning and source-bound DNS continue to use it in this phase.

An adapter without `source_ipv6` remains fully usable for IPv4. It is excluded
from an IPv6 attempt without being marked unhealthy. IPv6 never falls back to
an unbound socket or to an adapter that lacks an explicit IPv6 source.

## TCP rules

- TUN channels continue to reject domains because sing-box owns TUN DNS.
- TUN channels accept literal IPv4 and literal IPv6 SOCKS CONNECT targets.
- Ordinary SOCKS/HTTP accepts both literal families.
- Ordinary domain resolution keeps A preference and tries AAAA only after A
  fails and an IPv6-capable selected adapter exists.
- The selected target family determines `tcp4` or `tcp6`, source address, and
  interface option before connect.

## UDP rules

- SOCKS UDP domains and fragmentation remain rejected.
- Literal IPv4 and IPv6 destinations are accepted.
- A flow key includes the bracketed target address and port, so one physical
  source-bound socket remains stable for the destination five-tuple.
- IPv4 uses `udp4`, `source_ip`, and `IP_UNICAST_IF`.
- IPv6 uses `udp6`, `source_ipv6`, and `IPV6_UNICAST_IF`.
- Replies use the matching SOCKS5 UDP IPv4 or IPv6 address header.
- The IPv4 loopback relay continues to enforce the TCP peer and first-client
  UDP port lock.

## Windows binding

IPv4 retains the verified network-byte-order `IP_UNICAST_IF` behavior.
IPv6 uses `IPV6_UNICAST_IF` with the IPv6 interface index in host byte order
and an explicit IPv6 `LocalAddr`.

Global and unique-local IPv6 sources are accepted. Unspecified, multicast,
loopback-external misuse, IPv4-mapped, and link-local sources are rejected by
configuration. Loopback is accepted so the real integration suite can prove
dual-stack relay locally.

## Capability and fallback

`engine.hello.mode_features` adds `ipv6_egress` to `proxy` and
`tun_tcp_pool`. The Qt Go TUN selector requires this feature together with
TCP CONNECT and UDP ASSOCIATE. An older engine therefore falls back to the
existing Python pool before acquiring any Go resource.

The adapter scanner adds IPv6 metadata without changing the requirement that
an adapter have a usable IPv4 address. This keeps diagnostics, DNS, WFP, and
existing UI selection semantics stable.

## Exit criteria

- IPv4 behavior and wire fields remain backward compatible.
- Config validation normalizes optional IPv6 sources and rejects unsafe ones.
- TCP and UDP use family-matched source addresses and interface options.
- Incompatible adapters are skipped without poisoning their IPv4 health.
- TUN domains and UDP domains/fragments remain rejected.
- The Go engine process is excluded from TUN recapture.
- SOCKS UDP IPv6 request/reply headers round-trip.
- Real IPv6 loopback TCP and UDP integration passes when IPv6 is available.
- Python scanner/DTO tests, protocol fixtures, Go tests/vet/build, and the
  existing real child-process integration pass.
