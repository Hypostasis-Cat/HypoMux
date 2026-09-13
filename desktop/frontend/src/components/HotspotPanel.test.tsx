// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { HotspotPanel } from "./HotspotPanel";
import { hotspotDraft } from "./hotspotDraft";

const mocks = vi.hoisted(() => ({ save: vi.fn(), preferences: vi.fn(), status: vi.fn(), start: vi.fn(), stop: vi.fn() }));
vi.mock("../platform/services", () => ({ appServices: { engine: { saveHotspotPreferences: mocks.save, hotspotPreferences: mocks.preferences, hotspotStatus: mocks.status, startHotspot: mocks.start, stopHotspot: mocks.stop } } }));
vi.mock("../i18n/i18n", () => ({ useI18n: () => ({ locale: "en" }) }));
const stopped = { state: "stopped", ssid: "", band: "auto", ready: true, sharing_verified: false, clients: 0 };
const running = { ...stopped, state: "running", ssid: "My hotspot", sharing_verified: true, shared_adapter: "HypoMux-Tun", clients: 2 };
beforeEach(() => { vi.resetAllMocks(); mocks.save.mockResolvedValue(undefined); hotspotDraft.current = undefined; mocks.preferences.mockResolvedValue({ ssid: "HypoMux", password: "", band: "auto" }); mocks.status.mockResolvedValue(stopped); mocks.start.mockResolvedValue(running); mocks.stop.mockResolvedValue(stopped); });
afterEach(cleanup);

describe("HotspotPanel", () => {
  it("saves settings without starting aggregation and generates a valid password", async () => {
    mocks.status.mockResolvedValue({ ...stopped, ready: false });
    render(<HotspotPanel />);
    await screen.findByText("Hotspot is off");
    fireEvent.click(screen.getByRole("button", { name: "Generate password" }));
    const password = (screen.getByLabelText("Network password") as HTMLInputElement).value;
    expect(password).toMatch(/^[A-Za-z0-9_-]{16}$/);
    fireEvent.click(screen.getByRole("button", { name: "Save settings" }));
    await screen.findByText("Settings saved encrypted and restored next time you open the app.");
    expect(mocks.save).toHaveBeenCalledWith({ ssid: "HypoMux", password, band: "auto" });
    expect(mocks.start).not.toHaveBeenCalled();
  });
  it("renders Windows device details without claiming per-device throughput", async () => {
    mocks.status.mockResolvedValue({ ...running, devices_available: true, devices: [{ mac: "AA:BB:CC:DD:EE:FF", hosts: ["test-phone"] }] });
    render(<HotspotPanel />);
    expect(await screen.findByText("test-phone")).toBeTruthy();
    expect(screen.getByText("AA:BB:CC:DD:EE:FF")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Save settings" })).toBeNull();
  });
  it("restores encrypted preferences and retains edits across navigation", async () => {
    mocks.preferences.mockResolvedValue({ ssid: "Saved network", password: "saved-pass", band: "5" });
    const view = render(<HotspotPanel />);
    await waitFor(() => expect((screen.getByLabelText("Network password") as HTMLInputElement).value).toBe("saved-pass"));
    fireEvent.change(screen.getByLabelText("Network password"), { target: { value: "edited-pass" } });
    view.unmount();
    render(<HotspotPanel />);
    await screen.findByText("Hotspot is off");
    expect((screen.getByLabelText("Network password") as HTMLInputElement).value).toBe("edited-pass");
    expect((screen.getByLabelText("Network name") as HTMLInputElement).value).toBe("Saved network");
    expect((screen.getByLabelText("Wi-Fi band") as HTMLSelectElement).value).toBe("5");
    expect(mocks.preferences).toHaveBeenCalledOnce();
  });
  it("keeps an operational hotspot stoppable without claiming verified egress", async () => {
    mocks.status.mockResolvedValue({ ...running, sharing_verified: false, gateway_address: "192.168.137.1", message: "Egress verification unavailable" });
    render(<HotspotPanel />);
    await screen.findByText("Hotspot is on · egress unverified");
    expect(screen.queryByText(/Shared egress verified/)).toBeNull();
    expect(screen.getByText("Hotspot gateway: 192.168.137.1")).toBeTruthy();
    expect(screen.getByRole("switch", { name: "Aggregation hotspot" }).hasAttribute("disabled")).toBe(false);
    fireEvent.click(screen.getByRole("switch", { name: "Aggregation hotspot" }));
    await screen.findByText("Hotspot is off");
    expect(mocks.stop).toHaveBeenCalledOnce();
  });
  it("requires a running TUN before allowing hotspot startup", async () => {
    mocks.status.mockResolvedValue({ ...stopped, ready: false });
    render(<HotspotPanel />);
    await screen.findByText("Hotspot is off");
    fireEvent.change(screen.getByLabelText("Network password"), { target: { value: "password123" } });
    expect(screen.getByRole("switch", { name: "Aggregation hotspot" }).hasAttribute("disabled")).toBe(true);
    expect(mocks.start).not.toHaveBeenCalled();
  });
  it("starts with credentials and displays verified egress and real client count", async () => {
    render(<HotspotPanel />);
    await screen.findByText("Hotspot is off");
    fireEvent.change(screen.getByLabelText("Network name"), { target: { value: "My hotspot" } });
    fireEvent.change(screen.getByLabelText("Network password"), { target: { value: "safe-'$`password" } });
    fireEvent.click(screen.getByRole("switch", { name: "Aggregation hotspot" }));
    await screen.findByText("Hotspot is on");
    expect(mocks.start).toHaveBeenCalledWith({ ssid: "My hotspot", password: "safe-'$`password", band: "auto" });
    expect(screen.getByText(/Connected devices: 2/)).toBeTruthy();
    expect(screen.getByText(/Shared egress verified: HypoMux-Tun/)).toBeTruthy();
    fireEvent.click(screen.getByRole("switch", { name: "Aggregation hotspot" }));
    await screen.findByText("Hotspot is off");
    expect((screen.getByLabelText("Network password") as HTMLInputElement).value).toBe("safe-'$`password");
  });
  it("does not claim success after a sharing failure", async () => {
    mocks.start.mockRejectedValue(new Error("Shared egress mismatch"));
    render(<HotspotPanel />);
    await screen.findByText("Hotspot is off");
    fireEvent.change(screen.getByLabelText("Network password"), { target: { value: "password123" } });
    fireEvent.click(screen.getByRole("switch", { name: "Aggregation hotspot" }));
    await waitFor(() => expect(screen.getByRole("alert").textContent).toContain("Shared egress mismatch"));
    expect(screen.queryByText("Hotspot is on")).toBeNull();
  });
  it("validates UTF-8 SSID length rather than character count", async () => {
    render(<HotspotPanel />);
    await screen.findByText("Hotspot is off");
    fireEvent.change(screen.getByLabelText("Network password"), { target: { value: "password123" } });
    fireEvent.change(screen.getByLabelText("Network name"), { target: { value: "网".repeat(11) } });
    expect(screen.getByRole("switch", { name: "Aggregation hotspot" }).hasAttribute("disabled")).toBe(true);
  });
});
