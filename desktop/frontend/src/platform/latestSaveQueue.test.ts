import { describe, expect, it } from "vitest";
import { LatestSaveQueue } from "./latestSaveQueue";

const deferred = <T>() => {
  let resolve: (value: T) => void = () => undefined;
  let reject: (error: unknown) => void = () => undefined;
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, resolve, reject };
};

describe("LatestSaveQueue", () => {
  it("serialises saves and coalesces pending inputs to the newest snapshot", async () => {
    const first = deferred<string>();
    const latest = deferred<string>();
    const inputs: number[] = [];
    const queue = new LatestSaveQueue<number, string>((input) => {
      inputs.push(input);
      return input === 1 ? first.promise : latest.promise;
    });

    const a = queue.enqueue(1);
    const b = queue.enqueue(2);
    const c = queue.enqueue(3);
    expect(inputs).toEqual([1]);
    first.resolve("saved-1");
    await a.done;
    await Promise.resolve();
    expect(inputs).toEqual([1, 3]);
    latest.resolve("saved-3");
    await expect(Promise.all([b.done, c.done])).resolves.toEqual(["saved-3", "saved-3"]);
    expect(queue.isCurrent(a.revision)).toBe(false);
    expect(queue.isCurrent(b.revision)).toBe(false);
    expect(queue.isCurrent(c.revision)).toBe(true);
  });

  it("continues with newer work after an older save fails", async () => {
    const first = deferred<number>();
    const queue = new LatestSaveQueue<number, number>((input) => input === 1 ? first.promise : Promise.resolve(input));
    const old = queue.enqueue(1);
    const current = queue.enqueue(2);
    first.reject(new Error("old failed"));
    await expect(old.done).rejects.toThrow("old failed");
    await expect(current.done).resolves.toBe(2);
    await expect(queue.flush()).resolves.toBeUndefined();
  });

  it("makes flush fail when the latest save failed", async () => {
    const queue = new LatestSaveQueue<number, number>(() => Promise.reject(new Error("disk failed")));
    const handle = queue.enqueue(1);
    await expect(handle.done).rejects.toThrow("disk failed");
    await expect(queue.flush()).rejects.toThrow("disk failed");
  });
});
