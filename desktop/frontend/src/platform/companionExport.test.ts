import { beforeEach, expect, it, vi } from "vitest";
import { saveCompanionFile } from "./companionExport";
const call = vi.hoisted(() => vi.fn());
vi.mock("@wailsio/runtime", () => ({ Call: { ByName: call } }));
vi.mock("./runtime", () => ({ isDesktopRuntime: () => true }));
beforeEach(() => { call.mockReset(); });

it("sends binary skin data intact to the native save dialog", async () => {
  const bytes = Uint8Array.from({ length: 20000 }, (_, index) => index % 256);
  call.mockResolvedValue(true);
  expect(await saveCompanionFile("example.muxskin", bytes)).toBe("saved");
  const [method, name, encoded] = call.mock.calls[0];
  expect(method).toContain("DesktopHost.ExportCompanionFile");
  expect(name).toBe("example.muxskin");
  expect(Uint8Array.from(atob(encoded), character => character.charCodeAt(0))).toEqual(bytes);
});
it("distinguishes cancellation and reports write errors", async () => {
  call.mockResolvedValue(false);
  expect(await saveCompanionFile("example.muxskin", new Uint8Array([1]))).toBe("cancelled");
  call.mockRejectedValue(new Error("disk full"));
  await expect(saveCompanionFile("example.muxskin", new Uint8Array([1]))).rejects.toThrow("disk full");
});
