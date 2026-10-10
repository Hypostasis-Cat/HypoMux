// @vitest-environment jsdom
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { useEngineState } from "./useEngineState";

const mocks = vi.hoisted(() => ({ snapshot: vi.fn(), latest: vi.fn(), polls: [] as Array<() => Promise<void>> }));
vi.mock("../platform/runtime", () => ({ isDesktopRuntime: () => true }));
vi.mock("../platform/desktop", () => ({ desktopPlatform: { setEngineTrayStatus: vi.fn() } }));
vi.mock("../platform/serialPoll", () => ({ startSerialPoll: (task: () => Promise<void>) => { mocks.polls.push(task); return () => {}; } }));
vi.mock("../platform/services", async importOriginal => ({
  ...(await importOriginal<typeof import("../platform/services")>()),
  appServices: {
    engine: { snapshot: mocks.snapshot },
    adapters: { list: async () => [{ id: "a", name: "Ethernet", selected: true, weight: 1 }] },
    settings: { get: async () => ({ weighted: false, system_proxy_takeover: false, socks_port: 10800, http_port: 10801 }) },
    diagnostics: { latest: mocks.latest },
  },
}));

const snapshot = {
  phase: "running", mode: "proxy", download_bps: 4096, upload_bps: 1024, connections: 2,
  adapters: [{ id: "Ethernet", download_bps: 4096, upload_bps: 1024, connections: 2 }],
};
beforeEach(() => {
  vi.clearAllMocks(); mocks.polls = [];
  mocks.snapshot.mockResolvedValue(snapshot);
  mocks.latest.mockResolvedValue({ results: [] });
});
afterEach(() => { cleanup(); vi.useRealTimers(); });

it("loads manual-proxy throughput even when optional diagnostics fail", async () => {
  mocks.latest.mockRejectedValue(new Error("diagnostics offline"));
  const { result } = renderHook(() => useEngineState(vi.fn()));
  await waitFor(() => expect(result.current.phase).toBe("running"));
  expect(result.current.systemProxyTakeover).toBe(false);
  expect(result.current.totalDownload).toBe(4096);
  expect(result.current.adapters[0].uploadBPS).toBe(1024);
  expect(result.current.telemetryError).toBe("");
});

it("marks failed polls unavailable and restores live data after retry", async () => {
  const { result } = renderHook(() => useEngineState(vi.fn()));
  await waitFor(() => expect(result.current.phase).toBe("running"));
  mocks.snapshot.mockRejectedValueOnce(new Error("telemetry offline"));
  await act(async () => { await mocks.polls[0](); });
  expect(result.current.telemetryError).toBe("telemetry offline");
  expect(result.current.totalDownload).toBe(4096);
  mocks.snapshot.mockResolvedValue({ ...snapshot, download_bps: 8192 });
  await act(async () => { await result.current.refreshTelemetry(); });
  expect(result.current.telemetryError).toBe("");
  expect(result.current.totalDownload).toBe(8192);
});

it("bounds a hung poll so subsequent retries can recover", async () => {
  const { result } = renderHook(() => useEngineState(vi.fn()));
  await waitFor(() => expect(result.current.phase).toBe("running"));
  vi.useFakeTimers();
  mocks.snapshot.mockImplementationOnce(() => new Promise(() => {}));
  await act(async () => {
    const pending = mocks.polls[0]();
    await vi.advanceTimersByTimeAsync(8_000);
    await pending;
  });
  expect(result.current.telemetryError).not.toBe("");
  await act(async () => { await mocks.polls[0](); });
  expect(result.current.telemetryError).toBe("");
});

it("ignores an older failed poll after a newer retry succeeds", async () => {
  const { result } = renderHook(() => useEngineState(vi.fn()));
  await waitFor(() => expect(result.current.phase).toBe("running"));
  let rejectOld!: (error: Error) => void;
  mocks.snapshot.mockImplementationOnce(() => new Promise((_, reject) => { rejectOld = reject; }));
  let oldPoll!: Promise<void>;
  act(() => { oldPoll = mocks.polls[0](); });
  mocks.snapshot.mockResolvedValue({ ...snapshot, download_bps: 8192 });
  await act(async () => { await result.current.refreshTelemetry(); });
  await act(async () => { rejectOld(new Error("old poll failed")); await oldPoll; });
  expect(result.current.telemetryError).toBe("");
  expect(result.current.totalDownload).toBe(8192);
});
