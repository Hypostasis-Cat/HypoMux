import { afterEach, describe, expect, it, vi } from "vitest";
import { startSerialPoll } from "./serialPoll";

afterEach(() => vi.useRealTimers());

describe("startSerialPoll", () => {
  it("does not start another request until the previous request settles", async () => {
    vi.useFakeTimers();
    let resolve: () => void = () => undefined;
    let calls = 0;
    const stop = startSerialPoll(() => {
      calls += 1;
      return new Promise<void>((done) => { resolve = done; });
    }, 100, { immediate: true });

    expect(calls).toBe(1);
    await vi.advanceTimersByTimeAsync(500);
    expect(calls).toBe(1);
    resolve();
    await vi.advanceTimersByTimeAsync(99);
    expect(calls).toBe(1);
    await vi.advanceTimersByTimeAsync(1);
    expect(calls).toBe(2);
    stop();
  });

  it("stops future work", async () => {
    vi.useFakeTimers();
    const task = vi.fn(() => Promise.resolve());
    const stop = startSerialPoll(task, 100);
    stop();
    await vi.advanceTimersByTimeAsync(500);
    expect(task).not.toHaveBeenCalled();
  });
});
