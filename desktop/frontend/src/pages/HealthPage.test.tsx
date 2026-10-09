// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { HealthPage } from "./HealthPage";
import { PageActivity } from "../components/shell/PageActivity";

const mock = vi.hoisted(() => ({ saveSelected: vi.fn(), save: vi.fn(), notify: vi.fn(), results: [] as any[] }));
vi.mock("../platform/services", () => ({
  appServices: {
    adapters: { saveSelected: mock.saveSelected, save: mock.save },
    diagnostics: { latest: async () => ({ state: "completed", results: mock.results, total: mock.results.length, completed: mock.results.length }) },
  },
  withServiceTimeout: (promise: Promise<unknown>) => promise,
}));
vi.mock("../platform/runtime", () => ({ isDesktopRuntime: () => true }));
vi.mock("../components/notifications/AppNotifications", () => ({ useAppNotifications: () => ({ notify: mock.notify }) }));
vi.mock("../i18n/i18n", () => ({ useI18n: () => ({ locale: "en" }) }));
vi.mock("./MTUDetectionPage", () => ({ MTUDetectionPage: () => null }));
vi.mock("./NATDetectionPage", () => ({ NATDetectionPage: () => null }));
afterEach(() => { cleanup(); mock.results = []; });

it.each([
  ["limited", "Partial connectivity"],
  ["unverified", "Unverified"],
  ["available", "Probe passed"],
])("shows evidence-based status %s without claiming total outage", async (status, label) => {
  mock.results = [{ adapter_id: "nic", name: "nic", address: "192.0.2.1", status, sent: 10, received: 0, loss_rate: 100, checks: [] }];
  render(<HealthPage adapterRuntime={[{ id: "nic", name: "nic", address: "192.0.2.1", operational: true, selected: true } as any]} enginePhase="stopped" />);
  await waitFor(() => expect(screen.getAllByText(label).length).toBeGreaterThan(0));
  expect(screen.getByText("ICMP non-response")).toBeTruthy();
  expect(screen.queryByText("Unavailable")).toBeNull();
});

it("submits only selected IDs after returning to a preserved diagnostics page", async () => {
  mock.saveSelected.mockResolvedValue([]);
  const adapters: any[] = [{ id: "nic", name: "nic", address: "192.168.1.2", operational: true, selected: true, weight: 1 }];
  const show = (active: boolean) => <PageActivity.Provider value={active}><HealthPage adapterRuntime={adapters} enginePhase="stopped" /></PageActivity.Provider>;
  const view = render(show(true));
  await waitFor(() => expect(screen.getAllByRole("button", { name: "Start diagnostics" })[0].hasAttribute("disabled")).toBe(false));
  view.rerender(show(false));
  // Home or AI can change mode/strategy while this component stays mounted.
  view.rerender(show(true));
  fireEvent.click(screen.getByRole("button", { name: "Clear selection" }));
  await waitFor(() => expect(mock.saveSelected).toHaveBeenCalledWith([]));
  expect(mock.save).not.toHaveBeenCalled();
});
