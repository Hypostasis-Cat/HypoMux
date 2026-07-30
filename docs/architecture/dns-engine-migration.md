# DNS/DoH engine migration

> Historical migration record; selected-adapter DNS is now Go-owned.

Status: implemented behind the existing Go proxy development flag

## Scope

Phase 5 moves domain resolution for the staged Go ordinary proxy behind the
engine boundary. It does not move sing-box TUN DNS ownership yet. TUN keeps
its existing, verified sing-box DNS and WFP path until the TUN orchestration
phase.

The immediate defect being removed is implicit use of the operating-system
resolver when the Go proxy receives a SOCKS5 or HTTP domain target. System
resolution is not tied to the adapter selected by the scheduler and can
therefore use the wrong interface or re-enter the TUN path.

## Security and routing invariants

1. A domain connection selects an adapter before resolution.
2. DNS and the subsequent TCP connection use the same adapter source IPv4
   address and Windows interface index.
3. The Go engine never calls `net.DefaultResolver`, `net.LookupIP`, or a
   hostname-based `net.Dialer` target for proxy traffic.
4. DoH connections use literal endpoint IPs, TLS certificate verification,
   and the provider hostname as SNI.
5. Traditional DNS uses only explicit adapter or configured server IPv4
   addresses over adapter-bound UDP/TCP port 53.
6. A pre-relay adapter failover performs a new DNS lookup in the second
   adapter's cache scope before connecting.
7. FakeIP answers in `198.18.0.0/15` are rejected outside the sing-box-owned
   TUN path.
8. Engine stop cancels in-flight resolution through the same root context
   used by active proxy connections. No DNS socket or worker survives stop.

## Policy

The public setting remains `auto`, `off`, `alidns`, `dnspod`, or `google`.

| Setting | Behavior |
| --- | --- |
| `auto` | Race the built-in DoH endpoints; if all fail, use source-bound traditional DNS |
| `off` | Use source-bound traditional DNS only |
| Explicit provider | Use only that provider's DoH endpoints; request a controlled compatibility fallback after repeated failure |

No policy falls back to the Windows system resolver.

## Protocol additions

`engine.start` gains an optional `dns` object:

```json
{
  "policy": "auto",
  "legacy_servers": ["223.5.5.5"],
  "cache_ttl_ms": 180000,
  "query_timeout_ms": 4000
}
```

Each adapter gains an optional `dns_servers` IPv4 array. Existing clients that
omit all DNS fields receive the safe `auto` defaults.

Two additive capabilities are introduced:

- `dns.resolve` resolves one A or AAAA record through a running engine and a
  named adapter. It exists for diagnostics, contract tests, and the future
  WPF diagnostics page.
- `dns.status` returns policy, cache size, in-flight count, query/cache/failure
  counters, and the configured upstreams without triggering network work.

Successful resolution reports the adapter, address, record type, transport,
upstream server, cache status, and expiry. The existing
`dns.fallback_required` event is advertised only when its Go producer exists.

## Cache and concurrency

- Cache keys contain adapter identity, normalized IDNA domain, and record
  type.
- Positive answers use the smallest applicable DNS TTL capped by
  `cache_ttl_ms`; zero TTL answers are not cached.
- Concurrent identical misses share one in-flight lookup.
- Cache storage is bounded and expired entries are removed before eviction.
- Negative caching is deferred until response-code and SOA semantics are
  explicitly modeled.

## Failure and cancellation

- `dns.resolve` is read-only but performs network work; the request deadline
  is owned by the caller and engine query timeout.
- Proxy resolution inherits the accepted connection's engine context.
- Invalid names, unsupported record types, invalid adapters, malformed DNS,
  TLS errors, timeouts, and empty answers return structured failures.
- An `auto` DoH-to-legacy transition is reported in DNS telemetry. It is not a
  system-resolver fallback and does not weaken adapter binding.
- Explicit-provider failure remains a failure. After the failure threshold,
  the engine emits one `dns.fallback_required` event for a controlled UI
  restart in compatibility mode.

## Batch exit criteria

- Go unit tests cover DNS wire validation, A/AAAA parsing, FakeIP rejection,
  TTL caching, expiry, shared in-flight requests, policy selection, and
  cancellation.
- Proxy integration proves that a domain target is converted to a literal IP
  before the bound TCP dial.
- Protocol manifest and canonical fixtures cover both DNS methods, the start
  configuration, status, errors, and fallback event.
- Python contract tests consume the same fixtures.
- The Qt development bridge passes current DNS settings and adapter DNS
  servers; its Python fallback remains unchanged.
- A compiled Windows engine completes a real adapter-bound DNS lookup without
  using the system resolver.
- Go tests, vet, build, Python tests, and existing proxy/TUN DNS regressions
  pass at the batch gate.
