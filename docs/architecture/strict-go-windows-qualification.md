# Strict-Go Windows qualification

> Historical qualification plan. Python removal and the WPF/Go cutover are
> complete; current releases use the C# smoke client and GitHub Actions.

Status: Phase 14 evidence session implemented; disruptive scenarios require an
operator

## Purpose

Phase 12 made Go the default network backend but deliberately retained the
Python network implementation as an emergency rollback. Phase 13 defines the
evidence required before that rollback can be deleted.

The gate has two layers:

1. an automated, read-only qualification report for packaging, protocol,
   capability, initial state, process reaping, and exact TUN residue;
2. an operator-run physical Windows matrix for scenarios that deliberately
   take over routes, WFP, Wintun, adapters, power state, or installed files.

The automated layer never calls `engine.start` or `tun.activate`. It is safe to
run during development and CI and records `network_modes_started=false`.

## Read-only qualification

From a source checkout:

```powershell
python -m engine_client.qualification `
  --engine .\hypomux-engine.exe `
  --output .\qualification\preflight.json
```

For a release candidate, run from an elevated terminal against the signed
installed binary:

```powershell
python -m engine_client.qualification `
  --engine "C:\Program Files\HypoMux\bin\hypomux-engine.exe" `
  --require-elevated `
  --require-signed `
  --output .\qualification\installed-preflight.json
```

SignPath test certificates intentionally use an untrusted test root. They may
be used for the pre-release physical matrix only by pinning the exact
certificate thumbprint:

```powershell
python -m engine_client.qualification `
  --engine "C:\Program Files\HypoMux\bin\hypomux-engine.exe" `
  --require-elevated `
  --require-signed `
  --allow-test-signer-thumbprint "<exact certificate SHA-1 thumbprint>" `
  --output .\qualification\test-signed-preflight.json
```

The exception accepts only an Authenticode signature whose status is
`UnknownError` or `NotTrusted` because of the untrusted test root and whose
certificate thumbprint exactly matches the explicit pin. It never accepts
`NotSigned`, `HashMismatch`, or an arbitrary untrusted signer. Production
qualification must omit this option and pass with Windows trust status
`Valid`.

The command exits nonzero when a required check fails. Its report contains:

- report schema, UTC timestamps, and host architecture;
- exact engine path and Authenticode status;
- protocol hello, health, engine status, and TUN status;
- complete ordinary-proxy and managed-TUN missing-capability sets;
- before/after `HypoMux-Tun` IPv4/IPv6 default routes;
- before/after matching Wintun devices;
- `hypomux-engine.exe` and `sing-box.exe` process snapshots;
- proof that the qualification host PID was reaped.

The report intentionally does not collect adapter addresses, user
configuration, routing rules, domains, or traffic payloads.

## Unified physical qualification session

Do not qualify an older production install. The session is bound to the exact
engine candidate by SHA-256, engine version, and commit. By default, preparing
a session requires both elevation and a valid Authenticode signature:

```powershell
python -m engine_client.qualification_session prepare `
  --engine "C:\Program Files\HypoMux\bin\hypomux-engine.exe" `
  --allow-test-signer-thumbprint "<exact certificate SHA-1 thumbprint>" `
  --output-dir ".\qualification\phase14"
```

For a local unsigned build, `--development` may be used to rehearse the
matrix. A development session is permanently ineligible to authorize Python
removal, even if every row is marked passed.

After completing and closing HypoMux for one row, capture the clean postflight
and record its evidence in one command:

```powershell
python -m engine_client.qualification_session capture `
  --session ".\qualification\phase14\session.json" `
  --scenario proxy `
  --result passed `
  --evidence "SOCKS5 and HTTP CONNECT succeeded on Ethernet and Wi-Fi" `
  --evidence "application log: C:\Users\me\.hypomux\logs\app.log"
```

Valid scenario IDs are `package`, `proxy`, `dns`, `scheduling`, `ipv6`,
`tun_tcp`, `tun_udp`, `wfp`, `lifecycle`, `crash`, `adapter_churn`, `power`,
`upgrade`, and `uninstall`. A passed row requires at least one evidence note
and a clean read-only postflight from the exact candidate binary.

The final machine-readable decision is:

```powershell
python -m engine_client.qualification_session summary `
  --session ".\qualification\phase14\session.json"
```

It exits successfully only when the formal signed/elevated preflight and all
14 physical rows pass. `verdict.python_removal_ready=true` is the sole Phase
15 deletion gate.

## Physical scenario matrix

Run the installed application with:

```powershell
$env:HYPOMUX_NETWORK_BACKEND = "go"
```

Each row must record pass/fail, timestamp, application log reference, adapter
types, and the postflight report path.

| Area | Required scenario | Evidence |
| --- | --- | --- |
| Package | Fresh install starts the signed `{app}\bin` engine | Strict read-only report passes with signature and elevation required |
| Proxy | SOCKS5 and HTTP CONNECT over each selected adapter | Remote address/echo plus engine telemetry identifies the selected adapter |
| DNS | Traditional DNS, each configured DoH provider, A and AAAA | Source-bound result and cache/transport telemetry |
| Scheduling | Round-robin, weighted scheduling, adapter failure and recovery | Health cooldown/recovery and no traffic on an unselected adapter |
| IPv6 | IPv6-capable adapter plus IPv4-only peer | Correct family selection without weakening source/interface binding |
| TUN TCP | HTTPS and a sustained download through managed TUN | Connectivity validation and byte telemetry |
| TUN UDP | DNS-independent UDP echo or QUIC through managed TUN | Stable flow telemetry and successful response |
| WFP | `strict_route=true`, then the controlled compatibility restart | One bounded retry; no mixed Go/Python ownership |
| Lifecycle | Start, stop, restart, close-to-tray, full exit | No residual route, Wintun device, sing-box, or qualification engine PID |
| Crash | Force-close the UI and separately terminate the Go host | Job kills its owned sing-box tree and exact cleanup completes on next start |
| Adapter churn | Disable/re-enable one selected physical adapter | Other eligible adapter continues; recovered adapter becomes eligible again |
| Power | Sleep/resume while idle and while a session is active | Reconstructible status and clean stop/restart |
| Upgrade | Upgrade over the previous production version | Signed binaries replaced without locked-file or stale-process failure |
| Uninstall | Uninstall after normal stop and after simulated crash recovery | No service, task, process, route, Wintun device, or install directory residue |

After closing HypoMux for each disruptive group, rerun the read-only command
and attach the JSON as the postflight artifact.

## Interruption and rollback

If a scenario disrupts connectivity:

1. request normal stop from the application when possible;
2. close the application so the Go host closes its Job object;
3. rerun the read-only report to identify exact HypoMux residue;
4. restart once with `HYPOMUX_NETWORK_BACKEND=python` only when the Go path
   itself cannot be used to recover;
5. preserve both application logs and qualification reports.

Do not run an image-wide `taskkill /IM sing-box.exe` during strict-Go
qualification. Such a command can hide an ownership bug and can terminate
another application's process.

## Python removal gate

Python `ProxyWorker`, `MultiPortProxyWorker`, `TunManager`, and their
image-wide process cleanup may be deleted only when every matrix row passes
and all attached postflight reports contain:

- zero `HypoMux-Tun` IPv4/IPv6 default routes;
- zero matching Wintun devices;
- no qualification-owned engine PID;
- no HypoMux-owned sing-box process;
- complete proxy and TUN capability contracts.

The remaining DNS preflight facade intentionally continues to reuse the
Python compatibility worker until this gate passes. Rewriting that algorithm
during qualification would invalidate the comparison baseline. Phase 15 may
start only when the Phase 14 session reports
`verdict.python_removal_ready=true`.
