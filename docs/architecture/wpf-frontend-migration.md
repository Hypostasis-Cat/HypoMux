# WPF frontend migration

Status: accepted architecture decision

## Product intent

HypoMux targets the Windows 11 / Fluent Design experience. The target is not
to reproduce the current Qt widget library in another toolkit. The new
frontend will be a native WPF application that uses the engine's public
protocol and follows Windows interaction patterns.

The current PySide6 frontend remains the production fallback until the WPF
application reaches feature, packaging, upgrade, and recovery parity.

## Visual stack

The visual stack has one owner:

- **WPF UI (`lepoco/WPF-UI`) is the only application-wide visual, navigation,
  window, dialog, and theme library.**
- Native WPF controls, layout panels, templates, bindings, commands, and
  accessibility behavior are the foundation.
- A native WPF control is preferred when it already provides the required
  behavior. WPF UI may style it or replace it where a Fluent-specific control
  is needed.
- The application must not merge a second complete theme such as
  MaterialDesignInXaml, HandyControl, ModernWpf, or the native
  `PresentationFramework.Fluent` resource dictionary alongside WPF UI's
  global resources.
- A supplemental package is allowed only for a missing, narrowly scoped
  capability. It must not install a competing global theme, window host,
  navigation system, dialog service, or application resource dictionary.
- Exact .NET, WPF UI, and supplemental package versions will be pinned by the
  frontend bootstrap change after a compatibility build has passed. Packages
  are not added during architecture planning.

This rule keeps resource lookup, light/dark switching, dialogs, title-bar
behavior, and control defaults deterministic.

## Process and ownership boundary

The WPF process is a presentation and orchestration client. The Go process is
the network engine.

| Concern | WPF frontend | Go engine |
| --- | --- | --- |
| Windows, navigation, theme, localization | Owns | Does not know |
| User input and validation messages | Owns presentation | Owns authoritative configuration validation |
| Adapter and engine status | Renders | Discovers and owns |
| Proxy, DNS, TUN, WFP, routing, rollback | Sends commands only | Owns |
| Traffic and health telemetry | Subscribes/renders | Measures and publishes |
| Privilege requirement | Explains and requests an action | Reports required privilege and performs privileged work |
| Persistence | Owns user-facing preferences | Owns engine-safe runtime state and recovery data |
| Logging | Displays and filters | Produces structured records |

Network behavior, platform commands, subprocess pipelines, and rollback logic
must not move into WPF code-behind or view models. Code-behind is restricted
to view-only mechanics that cannot be expressed cleanly through binding or
behaviors.

## Protocol-first rule

Protocol v1 is the migration seam shared by PySide6 and WPF. Every new Go
engine slice is considered frontend-ready only when all of the following are
true:

1. Requests, results, errors, and events use explicit transport DTOs rather
   than Qt, WPF, or page-specific models.
2. The capability is advertised by `engine.hello` only after it works.
3. Long-running work defines cancellation, deadline, progress, and shutdown
   behavior.
4. Lifecycle and error events are lossless and ordered. High-frequency
   telemetry is explicitly coalescible.
5. Status is deterministic and sufficient for a newly started UI process to
   reconstruct the current screen without hidden in-process state.
6. Protocol-v1 additions are backward-compatible. A breaking change requires
   a new negotiated protocol version.
7. Canonical JSON request/response/error/event fixtures cover the new
   contract and can be consumed unchanged by Go, Python, and C# tests.
8. Protocol fields describe engine facts and operations, not widget names,
   colors, toast text, page routes, or other frontend policy.

Until canonical fixtures exist, a new UI client would have to reverse-engineer
Go maps and Python assumptions. Contract consolidation therefore precedes the
WPF project bootstrap.

## Migration stages and exit criteria

### F0: architecture and information model

Deliverables:

- Accept this visual-stack and process-boundary decision.
- Map existing PySide6 capabilities to engine commands, events, settings, and
  target pages.
- Mark UI-only preferences separately from engine configuration.

Exit criteria:

- New backend work can be reviewed against a single WPF-readiness checklist.
- No WPF package or project decision is required to continue Go migration.

### F1: protocol contract consolidation

Deliverables:

- Canonical protocol-v1 JSON fixtures for hello, status, diagnostics,
  lifecycle events, proxy start/stop, telemetry, and structured errors.
- A method/event matrix with privilege, cancellation, timeout, and
  idempotency semantics.
- Stable configuration and telemetry DTOs that do not expose Python types.

Exit criteria:

- Go and Python tests validate the same fixtures.
- A C# client can be implemented without importing or reading PySide6 code.

Current status:

- The baseline manifest, named Go DTOs, and canonical fixtures for all
  capabilities advertised through engine migration phase 4 are complete.
- Each later engine slice extends this same manifest and fixture set before
  its protocol capability is advertised.

### F2: WPF shell and read-only engine client

Deliverables:

- A pinned WPF project using WPF UI as its only global visual library.
- Application shell, Windows 11 title bar, navigation, light/dark/system
  theme, localization boundary, and accessible keyboard navigation.
- A supervised JSONL engine process client with capability negotiation,
  deadlines, bounded messages, stderr draining, and graceful shutdown.
- Read-only status, adapter, log, and diagnostic views.

Exit criteria:

- Starting and closing the shell cannot leak `hypomux-engine.exe`.
- Engine crashes, incompatible versions, malformed messages, and timeouts
  produce recoverable UI states.
- The read-only views match canonical fixtures and a real engine process.

### F3: settings and ordinary proxy parity

Deliverables:

- Typed settings editing and validation.
- Start/stop controls for Go-owned ordinary proxy mode.
- Live adapter health, traffic, and connection telemetry.
- Navigation guards and clear running/starting/stopping/error states.

Exit criteria:

- WPF and PySide6 can control the same engine build independently.
- Proxy mode has unit, contract, real-process, and Windows network coverage.
- Failure and cancellation leave the engine in a reconstructible state.

### F4: TUN, WFP, DNS, and system integration parity

Deliverables:

- UI flows for all Go-owned DNS, TUN, WFP, routing, privilege, and rollback
  operations.
- Tray behavior, startup behavior, update/installer integration, recovery,
  and support-log export.

Exit criteria:

- Production scenarios pass from install through upgrade and uninstall.
- Forced termination, reboot recovery, adapter churn, and partial startup
  failures do not leave routes, WFP rules, or child processes behind.
- PySide6 remains available as a release fallback.

### F5: cutover and legacy frontend removal

Deliverables:

- WPF becomes the default frontend after an explicit release gate.
- PySide6 packaging and code are removed only after the fallback window.

Exit criteria:

- Feature, localization, accessibility, packaging, upgrade, recovery, and
  telemetry parity are signed off.
- At least one shipped WPF release has completed rollback/shadow validation.
- No production installer or runtime path depends on Python, Qt, or PySide6.

## Batch migration workflow

Large migration slices follow four checkpoints:

1. **Plan:** define ownership, DTOs, lifecycle, rollback, compatibility, and
   exit criteria before implementation.
2. **Implement:** move a cohesive capability into Go and expose only the
   protocol surface needed by any frontend.
3. **Bridge:** keep the PySide6 adapter thin and preserve the production
   fallback while making the same contract directly usable by WPF.
4. **Gate:** run formatting and focused compile/unit checks during the batch,
   then run the complete automated and real-Windows scenario suite at the
   slice exit.

This avoids repeating slow manual application testing after every small edit,
but it does not postpone compile or contract failures until the end of the
entire migration.

## Effect on the current engine roadmap

The next backend slices remain DNS/DoH and the TUN multi-port TCP outbound
pool. Before either surface is declared complete, its protocol DTOs and
canonical fixtures must be added under F1 rules.

WPF implementation begins with the F2 read-only shell after F1 is complete.
It must not block continued Go engine work, and it must not become the
production control path until the corresponding Go capability has passed its
own migration gate.
