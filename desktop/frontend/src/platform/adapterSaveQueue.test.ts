import { beforeEach, expect, it, vi } from "vitest";

const api = vi.hoisted(() => ({ save: vi.fn(), saveSelected: vi.fn() }));
vi.mock("./services", () => ({ appServices: { adapters: api } }));
beforeEach(() => { vi.resetModules(); vi.clearAllMocks(); });

it("does not drop a pending Home strategy when diagnostics coalesces a selection", async () => {
  const { adapterSaveQueue, adapterSaveInput } = await import("./adapterSaveQueue");
  let finish!: (value: any[]) => void;
  api.save.mockImplementationOnce(() => new Promise(resolve => { finish = resolve; })).mockResolvedValue([]);
  const adapters: any[] = [{ id: "a", selected: true, weight: 9 }, { id: "b", selected: true, weight: 3 }];
  const first = adapterSaveQueue.enqueue(adapterSaveInput("proxy", false, adapters, "round-robin"));
  const home = adapterSaveQueue.enqueue(adapterSaveInput("tun", false, adapters, "latency-first"));
  const diagnostic = adapterSaveQueue.enqueue({ selectedIDs: ["b"] });
  finish([]);
  await Promise.all([first.done, home.done, diagnostic.done]);
  expect(api.save).toHaveBeenLastCalledWith("tun", false, [
    { id: "a", selected: false, weight: 9 }, { id: "b", selected: true, weight: 3 },
  ], "latency-first");
  expect(api.saveSelected).not.toHaveBeenCalled();
});

it("sends an isolated diagnostics edit to the field-specific backend", async () => {
  const { adapterSaveQueue } = await import("./adapterSaveQueue");
  api.saveSelected.mockResolvedValue([]);
  await adapterSaveQueue.enqueue({ selectedIDs: ["a"] }).done;
  expect(api.saveSelected).toHaveBeenCalledWith(["a"]);
  expect(api.save).not.toHaveBeenCalled();
});
