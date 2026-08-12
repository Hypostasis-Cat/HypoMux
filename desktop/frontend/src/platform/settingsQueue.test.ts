import { describe, expect, it } from "vitest";
import { SettingsSaveQueue } from "./settingsQueue";

const authoritative = (patch: Record<string, unknown>) => ({
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
  ...patch,
});

describe("SettingsSaveQueue", () => {
  it("merges optimistic values only for operations still queued", async () => {
    const queue = new SettingsSaveQueue();
    let resolveA: () => void = () => {};
    const gateA = new Promise<void>((resolve) => {
      resolveA = resolve;
    });
    const pendingA = queue.enqueue(
      () => gateA.then(() => authoritative({ close_to_tray: true })),
      ["close_to_tray"],
    );
    // Operation B is queued while A is still running; B's optimistic value
    // must survive A's authoritative write-back.
    const pendingB = queue.enqueue(
      () => Promise.resolve(authoritative({ autostart: true })),
      ["autostart"],
    );
    const mergedWhileBQueued = queue.mergeAuthoritative(
      authoritative({ close_to_tray: true, autostart: false }),
      authoritative({ close_to_tray: true, autostart: true }),
    );
    expect(mergedWhileBQueued.autostart).toBe(true);
    resolveA();
    await Promise.all([pendingA, pendingB]);
    // B has run; nothing is queued anymore, so the authoritative response
    // wins completely.
    const mergedAfterAll = queue.mergeAuthoritative(
      authoritative({ close_to_tray: true, autostart: true }),
      authoritative({ close_to_tray: false, autostart: false }),
    );
    expect(mergedAfterAll).toEqual(
      authoritative({ close_to_tray: true, autostart: true }),
    );
  });

  it("does not let a failed earlier operation's recovery overwrite a later success", async () => {
    const queue = new SettingsSaveQueue();
    // Operation A fails; its recovery re-fetches server truth (old values).
    const pendingA = queue.enqueue(
      () => Promise.reject(new Error("simulated network failure")),
      ["close_to_tray"],
    );
    // Operation B is already queued with its own optimistic value; gate its
    // completion so the recovery merge happens while B is still pending.
    let resolveB: () => void = () => {};
    const gateB = new Promise<void>((resolve) => {
      resolveB = resolve;
    });
    const pendingB = queue.enqueue(
      () => gateB.then(() => authoritative({ close_to_tray: true })),
      ["close_to_tray"],
    );
    await expect(pendingA).rejects.toThrow("simulated network failure");
    // After A failed and the UI was reset to server truth, B is still queued:
    // the recovery merge must keep B's optimistic field, not the stale truth.
    const mergedDuringRecovery = queue.mergeAuthoritative(
      authoritative({ close_to_tray: false }),
      authoritative({ close_to_tray: true }),
    );
    expect(mergedDuringRecovery.close_to_tray).toBe(true);
    resolveB();
    await pendingB;
    // B succeeded; its response is authoritative and nothing is queued.
    const mergedAfterB = queue.mergeAuthoritative(
      authoritative({ close_to_tray: true }),
      authoritative({ close_to_tray: false }),
    );
    expect(mergedAfterB.close_to_tray).toBe(true);
  });

  it("keeps queued full-replace optimistic values until the operation runs", async () => {
    const queue = new SettingsSaveQueue();
    const pending = queue.enqueue(
      () => Promise.resolve(authoritative({ dns_server: "1.1.1.1" })),
      null,
    );
    const merged = queue.mergeAuthoritative(
      authoritative({ dns_server: "223.5.5.5" }),
      authoritative({ dns_server: "1.1.1.1" }),
    );
    expect(merged.dns_server).toBe("1.1.1.1");
    await pending;
    const mergedAfter = queue.mergeAuthoritative(
      authoritative({ dns_server: "1.1.1.1" }),
      authoritative({ dns_server: "223.5.5.5" }),
    );
    expect(mergedAfter.dns_server).toBe("1.1.1.1");
  });

  it("recovery merge inside the failed operation still prefers authoritative values for its own fields", async () => {
    const queue = new SettingsSaveQueue();
    // The recovery merge runs inside the operation, before its field
    // ownership is released by the finally block, exactly like the production
    // catch block.
    let recoveredSnapshot: Record<string, unknown> = {};
    const pendingA = queue.enqueue(async () => {
      recoveredSnapshot = queue.recoverAuthoritative(
        ["close_to_tray"],
        authoritative({ close_to_tray: false, autostart: false }),
        authoritative({ close_to_tray: true, autostart: true }),
      );
      throw new Error("simulated failure");
    }, ["close_to_tray"]);
    // B is queued behind A with its own optimistic value.
    const pendingB = queue.enqueue(
      () => Promise.resolve(authoritative({ autostart: true })),
      ["autostart"],
    );
    await expect(pendingA).rejects.toThrow("simulated failure");
    // A's own field must come from the authoritative (stale server) value
    // even though A's ownership was not released yet; B's queued field
    // survives.
    expect(recoveredSnapshot.close_to_tray).toBe(false);
    expect(recoveredSnapshot.autostart).toBe(true);
    await pendingB;
  });
});
