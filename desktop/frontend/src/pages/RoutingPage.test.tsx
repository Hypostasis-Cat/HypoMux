// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { RoutingPage } from "./RoutingPage";

const mocks = vi.hoisted(() => ({ snapshot: vi.fn(), save: vi.fn(), validate: vi.fn(), notify: vi.fn(), translate: (key: string) => key }));
vi.mock("../platform/services", () => ({ appServices: {
  routing: { snapshot: mocks.snapshot, save: mocks.save, validate: mocks.validate },
  engine: { snapshot: async () => ({ phase: "stopped", mode: "tun" }) },
} }));
vi.mock("../components/notifications/AppNotifications", () => ({ useAppNotifications: () => ({ notify: mocks.notify }) }));
vi.mock("../i18n/i18n", () => ({ useI18n: () => ({ locale: "en", t: mocks.translate }) }));

const outbounds = [{ id: "direct", label: "Direct" }, { id: "aggregation", label: "Aggregation" }];
beforeEach(() => {
  vi.clearAllMocks();
  vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });
  mocks.snapshot.mockResolvedValue({ rules: [
    { match_type: "process", value: "old.exe", outbound: "nic_old", priority: 70 },
    { match_type: "process", value: "browser.exe", outbound: "direct" },
    { match_type: "domain", value: "example.com", outbound: "nic_old" },
  ], outbounds, restart_required: false });
  mocks.validate.mockImplementation(async (rule) => ({ valid: true, rule, duplicate: false }));
  mocks.save.mockImplementation(async (rules) => ({ rules, outbounds, restart_required: false }));
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

it("disables unavailable rules across tabs and persists priority without deleting rules", async () => {
  render(<RoutingPage />);
  await screen.findByRole("switch", { name: "Enable rule old.exe" });
  fireEvent.click(screen.getByRole("button", { name: "Disable unavailable rules" }));
  await waitFor(() => expect(mocks.save).toHaveBeenCalled(), { timeout: 4000 });
  const saved = mocks.save.mock.calls[mocks.save.mock.calls.length - 1][0];
  expect(saved).toHaveLength(3);
  expect(saved[0]).toMatchObject({ disabled: true, priority: 70, outbound: "nic_old" });
  expect(saved[1].disabled).toBeUndefined();
  expect(saved[2].disabled).toBe(true);
  expect((screen.getByRole("switch", { name: "Enable rule old.exe" }) as HTMLInputElement).checked).toBe(false);
  expect(screen.queryByRole("spinbutton")).toBeNull();
  fireEvent.click(screen.getByRole("combobox", { name: "Match order" }));
  expect(screen.getAllByRole("option")).toHaveLength(6);
  fireEvent.click(screen.getByRole("option", { name: "IP → Domain → Process" }));
  await waitFor(() => expect(mocks.save).toHaveBeenLastCalledWith(expect.any(Array), ["ip", "domain", "process"]), { timeout: 4000 });
});

it("keeps rules unchanged when refreshing adapter availability fails", async () => {
  render(<RoutingPage />);
  await screen.findByRole("switch", { name: "Enable rule old.exe" });
  mocks.snapshot.mockRejectedValueOnce(new Error("Adapter enumeration failed"));
  fireEvent.click(screen.getByRole("button", { name: "Disable unavailable rules" }));
  await waitFor(() => expect(mocks.notify).toHaveBeenCalledWith(expect.objectContaining({ title: "Egress check failed" })));
  expect(mocks.save).not.toHaveBeenCalled();
  expect((screen.getByRole("switch", { name: "Enable rule old.exe" }) as HTMLInputElement).checked).toBe(true);
});


it("saves type order with an empty rule list", async () => {
  mocks.snapshot.mockResolvedValue({ rules: [], outbounds, restart_required: false, match_order: ["domain", "ip", "process"] });
  render(<RoutingPage />);
  const order = await screen.findByRole("combobox", { name: "Match order" });
  await waitFor(() => expect(order.textContent).toContain("Domain → IP → Process"));
  fireEvent.click(order);
  fireEvent.click(screen.getByRole("option", { name: "IP → Process → Domain" }));
  await waitFor(() => expect(mocks.save).toHaveBeenLastCalledWith([], ["ip", "process", "domain"]), { timeout: 4000 });
});
