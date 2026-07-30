# Adaptive health scheduling migration

Status: implemented behind the Phase 10 development capability gate

## Scope and ownership

Phase 10 moves outcome-based adapter health and safe ordinary-proxy domain
isolation into the Go engine. It does not move sing-box, WFP, routes, DNS
planning, settings persistence, or the production-default switch.

The Go engine has two different visibility boundaries:

- Ordinary SOCKS5/HTTP proxy requests retain their original domain until the
  engine resolves and connects them.
- TUN requests arrive from sing-box as literal IP targets. Their domain rules
  and DNS decisions remain owned by sing-box.

Accordingly, both modes share physical-adapter health, while only ordinary
proxy mode advertises and applies domain quarantine.

## Adapter health state machine

One synchronized health table is created per running engine and injected into
the ordinary scheduler plus every named TUN-channel scheduler. A physical
link failure observed through any channel therefore affects all other
channels that reference the same adapter.

Only failures that identify the selected local path are global health
evidence:

- failure to construct the source/interface-bound dialer;
- Windows network, host, or source-address unreachable errors;
- corresponding development-platform local-path errors.

Remote connection refusal, an untyped target error, and per-domain DNS
failure do not globally cool down an adapter.

Consecutive local failures use bounded backoff:

| Consecutive failure | Cooldown |
| --- | ---: |
| 1 | 2 seconds |
| 2 | 5 seconds |
| 3 | 15 seconds |
| 4 or more | 30 seconds |

During cooldown the adapter is skipped when another eligible path exists.
After expiry it reports `probing` and becomes selectable again. Any successful
connection resets the consecutive-failure count and returns it to `healthy`.
If every adapter is cooling down, the earliest recovery candidate remains
selectable so cooldown cannot cause a self-inflicted total outage.

## Domain quarantine

Domain state is normalized to lowercase without a trailing dot and kept only
in engine memory. One failed attempt is insufficient. Evidence is recorded
only when the same request subsequently succeeds through another adapter.
Two such comparative outcomes quarantine that adapter/domain pair for 30
minutes. Unconfirmed evidence expires after 10 minutes so unrelated failures
cannot accumulate indefinitely.

The following cases do not create a quarantine:

- every attempted adapter fails;
- literal-IP traffic, including all TUN TCP and UDP traffic;
- an adapter-local failure already represented by global health;
- a single unconfirmed failure.

A success through the affected adapter clears its evidence for that domain.
Expired entries are pruned lazily during selection and telemetry. If every
adapter is quarantined for a domain, the scheduler falls back to available
adapters instead of rejecting the destination.

This replaces background cross-adapter probes with evidence from the user's
real request path, avoiding extra traffic and false learning during a
destination-wide outage.

## Protocol and frontend contract

`engine.hello.mode_features` adds:

- `adaptive_health` to `proxy` and `tun_tcp_pool`;
- `domain_quarantine` to `proxy` only.

Per-adapter telemetry adds:

- `health_state`: `healthy`, `cooldown`, or `probing`;
- `consecutive_failures`, `health_successes`, and `health_failures`;
- optional `last_success_at`, `last_failure_at`, and `cooldown_until`;
- `domain_quarantines`, the active count for that adapter.

The top-level optional `domain_quarantines` array carries adapter, normalized
domain, evidence count, and expiry. Existing Qt traffic payloads forward the
display-safe health summary. The future WPF UI can bind the same DTO without
depending on Python worker classes.

## Rollback boundary

Qt requires the new mode features before selecting the Phase 10 Go path.
An older or partially deployed engine therefore falls back before acquiring
proxy or TUN runtime resources. The normal Python path remains unchanged.
