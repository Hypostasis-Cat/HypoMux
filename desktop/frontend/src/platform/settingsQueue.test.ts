import { describe, expect, it } from "vitest";
import { SettingsSaveQueue, type SaveOutcome } from "./settingsQueue";

type Settings = Record<string, unknown>;

const settings = (patch: Record<string, unknown>): Settings => ({
  mode: "tun",
  language: "zh",
  socks_port: 10800,
  http_port: 10801,
  weighted: false,
  strict_route: true,
  close_to_tray: false,
  autostart: false,
  auto_start_engine: false,
  dns_server: "223.5.5.5",
  dns_policy: "auto",
  dns_egress_mode: "auto",
  dns_adapter_id: "",
  ...patch,
});

const ok = (patch: Record<string, unknown>): SaveOutcome<void, Settings> => ({
  ok: true,
  value: undefined,
  authoritative: settings(patch),
});

const fail = (message: string, patch: Record<string, unknown>): SaveOutcome<void, Settings> => ({
  ok: false,
  error: new Error(message),
  restore: settings(patch),
});

/** Drives the queue exactly like the React settings page: a mutable current
 * state, an apply sink that runs updaters immediately, and an optimistic()
 * helper that mimics the pre-enqueue setSettings call. The server state is
 * simulated as cumulative: every successful save persists onto the previous
 * one, like the real backend returning the full settings object. */
function harness() {
  let current: Settings = settings({});
  let server: Settings = settings({});
  const queue = new SettingsSaveQueue<Settings>();
  queue.attach((updater) => {
    current = updater(current);
  });
  return {
    queue,
    get current() {
      return current;
    },
    optimistic(patch: Record<string, unknown>) {
      current = { ...current, ...patch };
    },
    /** Simulates the backend persisting patch and returning full settings. */
    persist(patch: Record<string, unknown>) {
      server = settings({ ...server, ...patch });
      return ok(server);
    },
    serverPatch(patch: Record<string, unknown>) {
      return settings({ ...server, ...patch });
    },
  };
}

describe("SettingsSaveQueue", () => {
  it("merges optimistic values only for operations still queued", async () => {
    const h = harness();
    let resolveA: () => void = () => {};
    const gateA = new Promise<void>((resolve) => {
      resolveA = resolve;
    });
    const pendingA = h.queue.enqueue(
      () => gateA.then(() => h.persist({ close_to_tray: true })),
      ["close_to_tray"],
    );
    // Operation B is queued while A is still running; B's optimistic value
    // must survive A's authoritative write-back.
    h.optimistic({ autostart: true });
    const pendingB = h.queue.enqueue(
      () => Promise.resolve(h.persist({ autostart: true })),
      ["autostart"],
    );
    resolveA();
    await Promise.all([pendingA, pendingB]);
    expect(h.current).toMatchObject({ close_to_tray: true, autostart: true });
  });

  it("does not let a failed earlier operation's recovery overwrite a later success", async () => {
    const h = harness();
    const pendingA = h.queue.enqueue(
      () => Promise.resolve(fail("simulated network failure", { close_to_tray: false })),
      ["close_to_tray"],
    );
    h.optimistic({ close_to_tray: true });
    const pendingB = h.queue.enqueue(
      () => Promise.resolve(h.persist({ close_to_tray: true })),
      ["close_to_tray"],
    );
    await expect(pendingA).rejects.toThrow("simulated network failure");
    // After A's recovery reset the field to server truth, B is still queued:
    // its optimistic close_to_tray survives the recovery merge.
    expect(h.current.close_to_tray).toBe(true);
    await pendingB;
    expect(h.current.close_to_tray).toBe(true);
  });

  it("full-replace success applies its authoritative value (not its own queued flag)", async () => {
    const h = harness();
    const pending = h.queue.enqueue(
      () => Promise.resolve(h.persist({ dns_server: "1.1.1.1" })),
      null,
    );
    await pending;
    expect(h.current.dns_server).toBe("1.1.1.1");
  });

  it("full-replace failure restores the server truth", async () => {
    const h = harness();
    const pending = h.queue.enqueue(
      () => Promise.resolve(fail("simulated failure", { dns_server: "223.5.5.5" })),
      null,
    );
    await expect(pending).rejects.toThrow("simulated failure");
    expect(h.current.dns_server).toBe("223.5.5.5");
  });

  it("migration success applies the returned configuration", async () => {
    const h = harness();
    const pending = h.queue.enqueue(
      () => Promise.resolve(ok({ language: "en", close_to_tray: true })),
      null,
    );
    await pending;
    expect(h.current.language).toBe("en");
    expect(h.current.close_to_tray).toBe(true);
  });

  it("failed operation without restore keeps the UI unchanged while later ops queue", async () => {
    const h = harness();
    // get() failed inside the operation: restore is null and the queue must
    // not wipe the optimistic value of the still-queued operation B.
    const pendingA = h.queue.enqueue(
      () =>
        Promise.resolve({
          ok: false as const,
          error: new Error("get failed"),
          restore: null,
        }),
      ["autostart"],
    );
    h.optimistic({ autostart: true });
    const pendingB = h.queue.enqueue(
      () => Promise.resolve(h.persist({ autostart: true })),
      ["autostart"],
    );
    await expect(pendingA).rejects.toThrow("get failed");
    expect(h.current.autostart).toBe(true);
    await pendingB;
    expect(h.current.autostart).toBe(true);
  });

  it("same-field sequence with a failed first operation converges to disk truth", async () => {
    const h = harness();
    // A=true fails and recovers to server false; B=false and C=true both
    // succeed afterwards. Disk ends at true, so the UI must too.
    const pendingA = h.queue.enqueue(
      () => Promise.resolve(fail("simulated failure", { autostart: false })),
      ["autostart"],
    );
    h.optimistic({ autostart: true });
    const pendingB = h.queue.enqueue(
      () => Promise.resolve(h.persist({ autostart: false })),
      ["autostart"],
    );
    h.optimistic({ autostart: false });
    const pendingC = h.queue.enqueue(
      () => Promise.resolve(h.persist({ autostart: true })),
      ["autostart"],
    );
    h.optimistic({ autostart: true });
    await expect(pendingA).rejects.toThrow("simulated failure");
    await Promise.all([pendingB, pendingC]);
    expect(h.current.autostart).toBe(true);
  });

  it("keeps queued full-replace optimistic values until the operation runs", async () => {
    const h = harness();
    let resolveFull: () => void = () => {};
    const gate = new Promise<void>((resolve) => {
      resolveFull = resolve;
    });
    const pending = h.queue.enqueue(
      () => gate.then(() => h.persist({ dns_server: "1.1.1.1" })),
      null,
    );
    // While the full-replace operation is queued, a merge must keep the
    // current optimistic snapshot instead of the stale authoritative value.
    const merged = h.queue.mergeAuthoritative(
      settings({ dns_server: "223.5.5.5" }),
      settings({ dns_server: "1.1.1.1" }),
    );
    expect(merged.dns_server).toBe("1.1.1.1");
    resolveFull();
    await pending;
  });
});
