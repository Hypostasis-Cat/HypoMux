// @vitest-environment jsdom

import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { PropsWithChildren, ReactElement } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AppNotificationProvider } from "../components/notifications/AppNotifications";
import type { HomeAdapter } from "../state/useEngineState";
import { HomePage } from "./HomePage";
import { dismissStartupWarningsToday, startupWarningsDismissedToday } from "../state/startupWarningReminder";

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

const NotificationTestProvider = ({ children }: PropsWithChildren) => (
  <AppNotificationProvider>{children}</AppNotificationProvider>
);

const renderPage = (ui: ReactElement) => render(ui, { wrapper: NotificationTestProvider });

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
  systemProxyTakeover: true,
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
    localStorage.clear();
    mocks.useEngineState.mockReturnValue(engineState());
  });

  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it("opens active connections for the clicked adapter", () => {
    const onNavigate = vi.fn();
    renderPage(<HomePage onNavigate={onNavigate} />);

    fireEvent.click(screen.getByRole("button", { name: "View active connections for Ethernet" }));

    expect(onNavigate).toHaveBeenCalledOnce();
    expect(onNavigate).toHaveBeenCalledWith("connections", "Ethernet");
  });

  it("keeps connection navigation available and uses the card to select an inactive adapter", () => {
    const inactiveAdapter = { ...adapter, selected: false };
    const inactiveEngine = {
      ...engineState(),
      adapters: [inactiveAdapter],
      selected: [],
      totalWeight: 0,
    };
    mocks.useEngineState.mockReturnValue(inactiveEngine);
    const onNavigate = vi.fn();
    renderPage(<HomePage onNavigate={onNavigate} />);

    fireEvent.click(screen.getByRole("button", { name: "View active connections for Ethernet" }));
    fireEvent.click(screen.getByRole("article"));

    expect(onNavigate).toHaveBeenCalledWith("connections", "Ethernet");
    expect(inactiveEngine.toggleAdapter).toHaveBeenCalledWith("Ethernet", true);
  });

  it("keeps adapter controls from triggering connection navigation", () => {
    const onNavigate = vi.fn();
    renderPage(<HomePage onNavigate={onNavigate} />);

    fireEvent.click(screen.getByRole("checkbox", { name: "Disable Ethernet" }));
    fireEvent.click(screen.getByRole("button", { name: "Increase Ethernet Weight" }));

    expect(onNavigate).not.toHaveBeenCalled();
  });

  it("makes local-port-only proxy mode explicit", () => {
    mocks.useEngineState.mockReturnValue({ ...engineState(), systemProxyTakeover: false });
    renderPage(<HomePage />);

    expect(screen.getByText(/Local proxy ports only; Windows system proxy is unchanged/)).not.toBeNull();
  });

  it("publishes adapter readiness and engine phase to the shared runtime feed", () => {
    const onAdapterRuntimeChange = vi.fn();
    const onEnginePhaseChange = vi.fn();
    mocks.useEngineState.mockReturnValue({ ...engineState(), loading: true, phase: "starting" });
    const { rerender } = renderPage(
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

  const warningSnapshot = {
    ready: true,
    issues: [{ code: "foreign_network_risk", level: "warning", title: "Third-party network risk", detail: "Test risk" }],
  };
  const preflight = () => mocks.useEngineState.mock.calls[mocks.useEngineState.mock.calls.length - 1][1];

  it("remembers the checkbox only after Continue and suppresses warnings after remount", async () => {
    const page = renderPage(<HomePage />);
    let result: Promise<boolean>;
    act(() => { result = preflight()(warningSnapshot); });
    expect(screen.getByText("Startup risks detected")).toBeTruthy();
    fireEvent.click(screen.getByRole("checkbox", { name: "Don't remind me again today" }));
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    await expect(result!).resolves.toBe(true);
    expect(startupWarningsDismissedToday()).toBe(true);
    page.unmount();
    renderPage(<HomePage />);
    await act(async () => {
      expect(await preflight()(warningSnapshot)).toBe(true);
    });
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("does not remember the checkbox when the user goes back", async () => {
    renderPage(<HomePage />);
    let result: Promise<boolean>;
    act(() => { result = preflight()(warningSnapshot); });
    fireEvent.click(screen.getByRole("checkbox", { name: "Don't remind me again today" }));
    fireEvent.click(screen.getByRole("button", { name: "Go back" }));
    await expect(result!).resolves.toBe(false);
    expect(startupWarningsDismissedToday()).toBe(false);
    act(() => { result = preflight()(warningSnapshot); });
    expect(screen.getByRole<HTMLInputElement>("checkbox", { name: "Don't remind me again today" }).checked).toBe(false);
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    await expect(result!).resolves.toBe(true);
    expect(startupWarningsDismissedToday()).toBe(false);
  });

  it("still shows blockers and disables Continue even when today's warnings are muted", async () => {
    dismissStartupWarningsToday();
    renderPage(<HomePage />);
    let result: Promise<boolean>;
    act(() => {
      result = preflight()({ ready: false, issues: [{ code: "missing_core", level: "blocker", title: "Missing core", detail: "Test" }] });
    });
    expect(screen.getByText("Virtual NIC cannot start yet")).toBeTruthy();
    expect(screen.queryByRole("checkbox", { name: "Don't remind me again today" })).toBeNull();
    expect(screen.getByRole("button", { name: "Continue" }).hasAttribute("disabled")).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Go back" }));
    await expect(result!).resolves.toBe(false);
  });
});
