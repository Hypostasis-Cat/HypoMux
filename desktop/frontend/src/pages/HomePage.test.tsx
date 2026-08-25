// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { HomeAdapter } from "../state/useEngineState";
import { HomePage } from "./HomePage";

const mocks = vi.hoisted(() => ({
  useEngineState: vi.fn(),
}));

vi.mock("../state/useEngineState", () => ({
  useEngineState: mocks.useEngineState,
}));

vi.mock("../i18n/i18n", () => ({
  useI18n: () => ({
    locale: "en",
    t: (key: string) => ({
      home_bw_column: "Weight",
      home_bw_column_hint: "Adjust adapter weight",
      home_refresh_tip: "Refresh",
      home_select_all: "Select all",
      home_deselect_all: "Deselect all",
    })[key] ?? key,
  }),
}));

const adapter: HomeAdapter = {
  id: "Ethernet",
  name: "Ethernet",
  description: "Realtek PCIe",
  address: "192.0.2.10",
  prefix_length: 24,
  if_index: 7,
  dns_servers: ["192.0.2.1"],
  metric: 25,
  automatic_metric: true,
  selected: true,
  weight: 3,
  kind: "ethernet",
  operational: true,
  downloadBPS: 1024,
  uploadBPS: 512,
  connections: 2,
  bytesDown: 4096,
  bytesUp: 2048,
  health: "healthy",
};

const engineState = () => ({
  phase: "stopped",
  mode: "proxy",
  weighted: false,
  adapters: [adapter],
  selected: [adapter],
  totalWeight: adapter.weight,
  history: Array.from({ length: 18 }, () => 0),
  loading: false,
  refreshing: false,
  preview: false,
  transitioning: false,
  coreConnected: true,
  coreVersion: "test",
  coreElevated: false,
  ports: { socks: 10800, http: 10801 },
  totalDownload: adapter.downloadBPS,
  totalUpload: adapter.uploadBPS,
  totalConnections: adapter.connections,
  sessionBytes: adapter.bytesDown + adapter.bytesUp,
  setMode: vi.fn(),
  setWeighted: vi.fn(),
  toggleEngine: vi.fn(),
  toggleAdapter: vi.fn(),
  updateWeight: vi.fn(),
  selectAll: vi.fn(),
  refreshAdapters: vi.fn(),
});

describe("HomePage adapter interactions", () => {
  beforeEach(() => {
    mocks.useEngineState.mockReturnValue(engineState());
  });

  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it("opens active connections for the clicked adapter", () => {
    const onNavigate = vi.fn();
    render(<HomePage onNavigate={onNavigate} />);

    fireEvent.click(screen.getByRole("button", { name: "View active connections for Ethernet" }));

    expect(onNavigate).toHaveBeenCalledOnce();
    expect(onNavigate).toHaveBeenCalledWith("connections", "Ethernet");
  });

  it("does not open connections for an adapter outside aggregation", () => {
    const inactiveAdapter = { ...adapter, selected: false };
    mocks.useEngineState.mockReturnValue({
      ...engineState(),
      adapters: [inactiveAdapter],
      selected: [],
      totalWeight: 0,
    });
    const onNavigate = vi.fn();
    render(<HomePage onNavigate={onNavigate} />);

    const nameButton = screen.getByRole("button", { name: "View active connections for Ethernet" }) as HTMLButtonElement;
    expect(nameButton.disabled).toBe(true);
    fireEvent.click(nameButton);
    fireEvent.click(screen.getByRole("article"));

    expect(onNavigate).not.toHaveBeenCalled();
  });

  it("keeps adapter controls from triggering connection navigation", () => {
    const onNavigate = vi.fn();
    render(<HomePage onNavigate={onNavigate} />);

    fireEvent.click(screen.getByRole("checkbox", { name: "Disable Ethernet" }));
    fireEvent.click(screen.getByRole("button", { name: "Increase Ethernet Weight" }));

    expect(onNavigate).not.toHaveBeenCalled();
  });

  it("publishes adapter readiness and engine phase to the shared runtime feed", () => {
    const onAdapterRuntimeChange = vi.fn();
    const onEnginePhaseChange = vi.fn();
    mocks.useEngineState.mockReturnValue({ ...engineState(), loading: true, phase: "starting" });
    const { rerender } = render(
      <HomePage
        onAdapterRuntimeChange={onAdapterRuntimeChange}
        onEnginePhaseChange={onEnginePhaseChange}
      />,
    );

    expect(onAdapterRuntimeChange).toHaveBeenLastCalledWith(undefined);
    expect(onEnginePhaseChange).toHaveBeenLastCalledWith("starting");

    mocks.useEngineState.mockReturnValue({ ...engineState(), adapters: [], selected: [], totalWeight: 0 });
    rerender(
      <HomePage
        onAdapterRuntimeChange={onAdapterRuntimeChange}
        onEnginePhaseChange={onEnginePhaseChange}
      />,
    );

    expect(onAdapterRuntimeChange).toHaveBeenLastCalledWith([]);
    expect(onEnginePhaseChange).toHaveBeenLastCalledWith("stopped");
  });
});
