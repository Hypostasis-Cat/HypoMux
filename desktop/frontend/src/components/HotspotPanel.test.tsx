// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { HotspotPanel } from "./HotspotPanel";

const mocks = vi.hoisted(() => ({ status: vi.fn(), start: vi.fn(), stop: vi.fn() }));
vi.mock("../platform/services", () => ({ appServices: { engine: { hotspotStatus: mocks.status, startHotspot: mocks.start, stopHotspot: mocks.stop } } }));
vi.mock("../i18n/i18n", () => ({ useI18n: () => ({ locale: "en" }) }));
const stopped = { state: "stopped", ssid: "", band: "auto", ready: true, sharing_verified: false, clients: 0 };
const running = { ...stopped, state: "running", ssid: "My hotspot", sharing_verified: true, shared_adapter: "HypoMux-Tun", clients: 2 };
beforeEach(() => { vi.resetAllMocks(); mocks.status.mockResolvedValue(stopped); mocks.start.mockResolvedValue(running); mocks.stop.mockResolvedValue(stopped); });
afterEach(cleanup);

describe("HotspotPanel", () => {
  it("keeps an operational hotspot stoppable without claiming verified egress", async () => {
    mocks.status.mockResolvedValue({ ...running, sharing_verified: false, gateway_address: "192.168.137.1", message: "Egress verification unavailable" });
    render(<HotspotPanel />);
    await screen.findByText("Hotspot is on · egress unverified");
    expect(screen.queryByText(/Shared egress verified/)).toBeNull();
    expect(screen.getByText("Hotspot gateway: 192.168.137.1")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Turn off hotspot" }).hasAttribute("disabled")).toBe(false);
    fireEvent.click(screen.getByRole("button", { name: "Turn off hotspot" }));
    await screen.findByText("Hotspot is off");
    expect(mocks.stop).toHaveBeenCalledOnce();
  });
  it("requires a running TUN before allowing hotspot startup", async () => {
    mocks.status.mockResolvedValue({ ...stopped, ready: false });
    render(<HotspotPanel />);
    await screen.findByText("Hotspot is off");
    fireEvent.change(screen.getByLabelText("Network password"), { target: { value: "password123" } });
    expect(screen.getByRole("button", { name: "Enable aggregation hotspot" }).hasAttribute("disabled")).toBe(true);
    expect(mocks.start).not.toHaveBeenCalled();
  });
  it("starts with credentials and displays verified egress and real client count", async () => {
    render(<HotspotPanel />);
    await screen.findByText("Hotspot is off");
    fireEvent.change(screen.getByLabelText("Network name"), { target: { value: "My hotspot" } });
    fireEvent.change(screen.getByLabelText("Network password"), { target: { value: "safe-'$`password" } });
    fireEvent.click(screen.getByRole("button", { name: "Enable aggregation hotspot" }));
    await screen.findByText("Hotspot is on");
    expect(mocks.start).toHaveBeenCalledWith({ ssid: "My hotspot", password: "safe-'$`password", band: "auto" });
    expect(screen.getByText(/Connected devices: 2/)).toBeTruthy();
    expect(screen.getByText(/Shared egress verified: HypoMux-Tun/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Turn off hotspot" }));
    await screen.findByText("Hotspot is off");
    expect((screen.getByLabelText("Network password") as HTMLInputElement).value).toBe("");
  });
  it("does not claim success after a sharing failure", async () => {
    mocks.start.mockRejectedValue(new Error("Shared egress mismatch"));
    render(<HotspotPanel />);
    await screen.findByText("Hotspot is off");
    fireEvent.change(screen.getByLabelText("Network password"), { target: { value: "password123" } });
    fireEvent.click(screen.getByRole("button", { name: "Enable aggregation hotspot" }));
    await waitFor(() => expect(screen.getByRole("alert").textContent).toContain("Shared egress mismatch"));
    expect(screen.queryByText("Hotspot is on")).toBeNull();
  });
  it("validates UTF-8 SSID length rather than character count", async () => {
    render(<HotspotPanel />);
    await screen.findByText("Hotspot is off");
    fireEvent.change(screen.getByLabelText("Network password"), { target: { value: "password123" } });
    fireEvent.change(screen.getByLabelText("Network name"), { target: { value: "网".repeat(11) } });
    expect(screen.getByRole("button", { name: "Enable aggregation hotspot" }).hasAttribute("disabled")).toBe(true);
  });
});
