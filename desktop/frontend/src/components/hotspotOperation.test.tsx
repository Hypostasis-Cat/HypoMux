// @vitest-environment jsdom
import { act, cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { runHotspotOperation, useHotspotOperation } from "./hotspotOperation";

afterEach(cleanup);
function Status() { return <span>{useHotspotOperation() ?? "idle"}</span>; }
it("retains the operation across page remounts and refuses duplicate calls", async () => {
  let finish!: () => void;
  const pending = new Promise<void>(resolve => { finish = resolve; });
  const view = render(<Status />);
  let operation!: Promise<void>;
  act(() => { operation = runHotspotOperation("start", () => pending); });
  expect(screen.getByText("start")).toBeTruthy();
  view.unmount();
  render(<Status />);
  expect(screen.getByText("start")).toBeTruthy();
  const duplicate = vi.fn();
  await expect(runHotspotOperation("start", duplicate)).rejects.toThrow();
  expect(duplicate).not.toHaveBeenCalled();
  await act(async () => { finish(); await operation; });
  expect(screen.getByText("idle")).toBeTruthy();
});
it("releases the operation after failure so the user can retry", async () => {
  await expect(runHotspotOperation("stop", async () => { throw new Error("cleanup failed"); })).rejects.toThrow("cleanup failed");
  await expect(runHotspotOperation("start", async () => "restarted")).resolves.toBe("restarted");
});
it("allows exactly one stop to cancel a pending start", async () => {
  let rejectStart!: (reason: Error) => void;
  const start = runHotspotOperation("start", () => new Promise<void>((_, reject) => { rejectStart = reject; }));
  let finishStop!: () => void;
  const stop = runHotspotOperation("stop", () => new Promise<void>(resolve => { finishStop = resolve; }));
  await expect(runHotspotOperation("stop", vi.fn())).rejects.toThrow();
  await expect(runHotspotOperation("save", vi.fn())).rejects.toThrow();
  render(<Status />);
  rejectStart(new Error("已取消开启热点"));
  await act(async () => { await expect(start).rejects.toThrow(); });
  expect(screen.getByText("stop")).toBeTruthy();
  await act(async () => { finishStop(); await stop; });
  expect(screen.getByText("idle")).toBeTruthy();
});
it("never lets a stop or save overtake a running stop", async () => {
  let finish!: () => void;
  const stop = runHotspotOperation("stop", () => new Promise<void>(resolve => { finish = resolve; }));
  await expect(runHotspotOperation("stop", vi.fn())).rejects.toThrow();
  finish(); await stop;
});
