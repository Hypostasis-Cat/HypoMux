// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { HotspotQuickControl } from "./HotspotQuickControl";
import { hotspotDraft } from "./hotspotDraft";

const mocks = vi.hoisted(() => ({ status: vi.fn(), preferences: vi.fn(), start: vi.fn(), stop: vi.fn(), configure: vi.fn() }));
vi.mock("../platform/services", () => ({ appServices: { engine: { hotspotStatus: mocks.status, hotspotPreferences: mocks.preferences, startHotspot: mocks.start, stopHotspot: mocks.stop } } }));
vi.mock("../i18n/i18n", () => ({ useI18n: () => ({ locale: "en" }) }));
const off = { state: "stopped", ready: true, clients: 0 };
const on = { ...off, state: "running", clients: 2, sharing_verified: false };
const config = { ssid: "Saved", password: "saved-password", band: "5" as const };
beforeEach(() => { vi.resetAllMocks(); mocks.status.mockResolvedValue(off); mocks.preferences.mockResolvedValue(config); mocks.start.mockResolvedValue(on); mocks.stop.mockResolvedValue(off); });
afterEach(cleanup);

it("starts with saved settings and stops without opening details", async () => {
  hotspotDraft.current = { ...config, password: "unsaved-draft" };
  render(<HotspotQuickControl onConfigure={mocks.configure} />);
  await screen.findByText("Hotspot is off");
  fireEvent.click(screen.getByRole("switch"));
  await screen.findByText("On · 2 device(s) · egress unverified");
  expect(mocks.start).toHaveBeenCalledWith(config);
  expect(hotspotDraft.current).toEqual(config);
  fireEvent.click(screen.getByRole("switch"));
  await screen.findByText("Hotspot is off");
  expect(mocks.stop).toHaveBeenCalledOnce();
  expect(mocks.configure).not.toHaveBeenCalled();
});
it("opens setup for missing credentials without starting", async () => {
  mocks.preferences.mockResolvedValue({ ...config, password: "" });
  render(<HotspotQuickControl onConfigure={mocks.configure} />);
  await screen.findByText("Hotspot is off");
  fireEvent.click(screen.getByRole("switch"));
  await waitFor(() => expect(mocks.configure).toHaveBeenCalledOnce());
  expect(mocks.start).not.toHaveBeenCalled();
});
it("allows stopping an active hotspot when TUN readiness is lost", async () => {
  mocks.status.mockResolvedValue({ ...on, ready: false });
  render(<HotspotQuickControl onConfigure={mocks.configure} />);
  await screen.findByText("On · 2 device(s) · egress unverified");
  expect(screen.getByRole("switch").hasAttribute("disabled")).toBe(false);
  fireEvent.click(screen.getByRole("switch"));
  await waitFor(() => expect(mocks.stop).toHaveBeenCalledOnce());
});
it("cancels a pending start without reporting an error", async () => {
  let rejectStart!: (reason: Error) => void;
  mocks.start.mockReturnValue(new Promise((_, reject) => { rejectStart = reject; }));
  mocks.stop.mockImplementation(async () => { rejectStart(new Error("已取消开启热点")); return off; });
  render(<HotspotQuickControl onConfigure={mocks.configure} />);
  await screen.findByText("Hotspot is off");
  fireEvent.click(screen.getByRole("switch"));
  await waitFor(() => expect(mocks.start).toHaveBeenCalledOnce());
  const toggle = screen.getByRole("switch");
  expect(toggle.hasAttribute("disabled")).toBe(false);
  fireEvent.click(toggle);
  await screen.findByText("Hotspot is off");
  expect(mocks.stop).toHaveBeenCalledOnce();
  expect(screen.queryByText("Hotspot needs attention")).toBeNull();
});
it("explains a hotspot Windows turned off", async () => {
  mocks.status.mockResolvedValue({ ...off, stop_reason: "windows_off", message: "Windows turned it off" });
  render(<HotspotQuickControl onConfigure={mocks.configure} />);
  await screen.findByText("Windows turned off the hotspot");
});
