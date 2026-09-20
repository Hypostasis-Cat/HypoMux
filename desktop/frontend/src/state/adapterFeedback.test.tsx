// @vitest-environment jsdom
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useEngineState } from "./useEngineState";

const mocks = vi.hoisted(() => ({ list: vi.fn(), save: vi.fn() }));
vi.mock("../platform/runtime", () => ({ isDesktopRuntime: () => true }));
vi.mock("../platform/desktop", () => ({ desktopPlatform: { setEngineTrayStatus: vi.fn() } }));
vi.mock("../platform/serialPoll", () => ({ startSerialPoll: () => () => {} }));
vi.mock("../platform/services", () => ({
  appServices: {
    adapters: mocks,
    settings: { get: async () => ({ weighted: false, hide_virtual_adapters: false }) },
    diagnostics: { latest: async () => ({ results: [] }) },
    engine: { snapshot: async () => ({ phase: "running", mode: "proxy", adapters: [], download_bps: 0 }) },
  },
  withServiceTimeout: (task: Promise<unknown>) => task,
}));

const adapters = [
  { id: "a", name: "Ethernet", selected: true, weight: 1 },
  { id: "b", name: "WLAN", selected: false, weight: 1 },
];
const added = adapters.map((adapter) => ({ ...adapter, selected: true }));

describe("adapter application feedback", () => {
  beforeEach(() => { vi.clearAllMocks(); mocks.list.mockResolvedValue(adapters); });
  afterEach(cleanup);

  it("waits for backend confirmation before reporting success, even with zero traffic", async () => {
    let confirm!: (value: unknown) => void;
    mocks.save.mockImplementation(() => new Promise((resolve) => { confirm = resolve; }));
    const { result } = renderHook(() => useEngineState(vi.fn()));
    await waitFor(() => expect(result.current.loading).toBe(false));
    act(() => result.current.toggleAdapter("b", true));
    expect(result.current.adapterFeedback?.status).toBe("pending");
    await act(async () => confirm(added));
    expect(result.current.adapterFeedback?.status).toBe("success");
    expect(result.current.adapterFeedback?.changes).toEqual([{ id: "b", name: "WLAN", selected: true }]);
  });

  it("reports an error when the saved selection does not contain the added adapter", async () => {
    mocks.save.mockResolvedValue(adapters);
    const { result } = renderHook(() => useEngineState(vi.fn()));
    await waitFor(() => expect(result.current.loading).toBe(false));
    await act(async () => result.current.toggleAdapter("b", true));
    expect(result.current.adapterFeedback?.status).toBe("error");
    expect(result.current.adapters[1].selected).toBe(false);
  });

  it("recovers authoritative selection after failure and reports a successful retry", async () => {
    const onError = vi.fn();
    mocks.save.mockRejectedValueOnce(new Error("offline")).mockResolvedValueOnce(added);
    const { result } = renderHook(() => useEngineState(onError));
    await waitFor(() => expect(result.current.loading).toBe(false));
    await act(async () => result.current.toggleAdapter("b", true));
    expect(result.current.adapterFeedback?.status).toBe("error");
    expect(result.current.adapters[1].selected).toBe(false);
    await act(async () => onError.mock.calls[0][1]());
    expect(result.current.adapterFeedback?.status).toBe("success");
  });

  it("keeps accumulated changes pending until the latest queued save completes", async () => {
    const resolves: ((value: unknown) => void)[] = [];
    mocks.save.mockImplementation(() => new Promise((resolve) => resolves.push(resolve)));
    const { result } = renderHook(() => useEngineState(vi.fn()));
    await waitFor(() => expect(result.current.loading).toBe(false));
    act(() => result.current.toggleAdapter("b", true));
    act(() => result.current.updateWeight("a", 2));
    await act(async () => resolves[0](added));
    expect(result.current.adapterFeedback?.status).toBe("pending");
    await act(async () => resolves[1](added.map((adapter) => ({ ...adapter, weight: 2 }))));
    expect(result.current.adapterFeedback?.status).toBe("success");
    expect(result.current.adapterFeedback?.changes[0].id).toBe("b");
  });

  it("does not create success feedback for a rejected last-adapter removal", async () => {
    const onError = vi.fn();
    const { result } = renderHook(() => useEngineState(onError));
    await waitFor(() => expect(result.current.phase).toBe("running"));
    act(() => result.current.selectAll(false));
    expect(mocks.save).not.toHaveBeenCalled();
    expect(result.current.adapterFeedback).toBeNull();
    expect(onError).toHaveBeenCalledOnce();
  });
});
