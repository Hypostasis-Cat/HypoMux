import { afterEach, expect, it, vi } from "vitest";
import { parseSkinAsync, exportSkinAsync } from "./packageAsync";
import type { Skin } from "./package";
class FakeWorker {
  static current: FakeWorker;
  onmessage?: (event: any) => void;
  onerror?: () => void;
  onmessageerror?: () => void;
  terminate = vi.fn();
  postMessage = vi.fn();
  constructor() { FakeWorker.current = this; }
}
afterEach(() => { vi.unstubAllGlobals(); vi.useRealTimers(); });
it("dispatches parsing off-thread, returns the result and releases the worker", async () => {
  vi.stubGlobal("Worker", FakeWorker);
  const bytes = new Uint8Array([1]);
  const promise = parseSkinAsync(bytes);
  const worker = FakeWorker.current;
  expect(worker.postMessage).toHaveBeenCalledWith({ type: "parse", bytes });
  const skin = { manifest: { id: "test" } };
  worker.onmessage!({ data: { result: skin } });
  expect(await promise).toBe(skin);
  expect(worker.terminate).toHaveBeenCalledOnce();
});
it("rejects corrupt packages without falling back to synchronous parsing", async () => {
  vi.stubGlobal("Worker", FakeWorker);
  const promise = parseSkinAsync(new Uint8Array());
  const result = expect(promise).rejects.toThrow("Invalid ZIP");
  FakeWorker.current.onmessage!({ data: { error: "Invalid ZIP" } });
  await result; expect(FakeWorker.current.terminate).toHaveBeenCalledOnce();
});
it("cleans up a timed out export", async () => {
  vi.useFakeTimers(); vi.stubGlobal("Worker", FakeWorker);
  const promise = exportSkinAsync({} as Skin);
  const result = expect(promise).rejects.toThrow(/超时/);
  await vi.advanceTimersByTimeAsync(60000);
  await result; expect(FakeWorker.current.terminate).toHaveBeenCalledOnce();
});
