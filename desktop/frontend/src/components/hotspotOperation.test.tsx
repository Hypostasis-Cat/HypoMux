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
