// @vitest-environment jsdom

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { TunPreflightSnapshot } from "../platform/services";
import { canDismissStartupWarnings, dismissStartupWarningsToday, startupWarningsDismissedToday } from "./startupWarningReminder";

beforeEach(() => localStorage.clear());
afterEach(() => vi.restoreAllMocks());

describe("startup warning reminder policy", () => {
  it("persists only for the local calendar day, including across month boundaries", () => {
    const evening = new Date(2026, 8, 30, 23, 59, 59);
    expect(startupWarningsDismissedToday(evening)).toBe(false);
    expect(dismissStartupWarningsToday(evening)).toBe(true);
    expect(startupWarningsDismissedToday(evening)).toBe(true);
    expect(startupWarningsDismissedToday(new Date(2026, 9, 1, 0, 0, 0))).toBe(false);
    expect(startupWarningsDismissedToday(new Date(2026, 8, 29))).toBe(false);
  });

  it("does not suppress unready, blocking or unknown issue levels", () => {
    const snapshot = (ready: boolean, level: string) => ({ ready, issues: [{ level }] }) as TunPreflightSnapshot;
    expect(canDismissStartupWarnings(snapshot(true, "warning"))).toBe(true);
    expect(canDismissStartupWarnings(snapshot(false, "warning"))).toBe(false);
    expect(canDismissStartupWarnings(snapshot(true, "blocker"))).toBe(false);
    expect(canDismissStartupWarnings(snapshot(true, "unknown"))).toBe(false);
  });

  it("fails safely when browser storage is unavailable", () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => { throw new Error("unavailable"); });
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => { throw new Error("unavailable"); });
    expect(startupWarningsDismissedToday()).toBe(false);
    expect(dismissStartupWarningsToday()).toBe(false);
  });
});
