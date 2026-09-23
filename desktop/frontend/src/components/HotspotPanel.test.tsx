// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { HotspotPanel } from "./HotspotPanel";
import { hotspotDraft } from "./hotspotDraft";

const mocks = vi.hoisted(() => ({ save: vi.fn(), preferences: vi.fn(), sessionConfig: vi.fn(), status: vi.fn(), start: vi.fn(), stop: vi.fn() }));
vi.mock("../platform/services", () => ({ appServices: { engine: { saveHotspotPreferences: mocks.save, hotspotPreferences: mocks.preferences, hotspotSessionConfig: mocks.sessionConfig, hotspotStatus: mocks.status, startHotspot: mocks.start, stopHotspot: mocks.stop } } }));
vi.mock("../i18n/i18n", () => ({ useI18n: () => ({ locale: "en" }) }));
const stopped = { state: "stopped", ssid: "", band: "auto", ready: true, sharing_verified: false, clients: 0 };
const running = { ...stopped, state: "running", session_id: "session-one", ssid: "My hotspot", sharing_verified: true, shared_adapter: "HypoMux-Tun", clients: 2 };
beforeEach(() => { vi.resetAllMocks(); mocks.save.mockResolvedValue(undefined); hotspotDraft.current = undefined; mocks.preferences.mockResolvedValue({ ssid: "HypoMux", password: "", band: "auto" }); mocks.sessionConfig.mockResolvedValue({ ssid: "My hotspot", password: "saved-pass", band: "auto" }); mocks.status.mockResolvedValue(stopped); mocks.start.mockResolvedValue(running); mocks.stop.mockResolvedValue(stopped); });
afterEach(() => { cleanup(); vi.restoreAllMocks(); });

describe("HotspotPanel", () => {
  it.each(["My hotspot", "Unsaved name"])("uses session credentials after external startup and preserves draft %s", async (ssid) => {
    const draft = { ssid, password: "unsaved-pass", band: "5" as const };
    hotspotDraft.current = draft;
    mocks.status.mockResolvedValue(running);
    const view = render(<HotspotPanel />);
    await waitFor(() => expect((screen.getByLabelText("Network password") as HTMLInputElement).value).toBe("saved-pass"));
    expect(mocks.sessionConfig).toHaveBeenCalledWith("session-one");
    fireEvent.click(screen.getByRole("button", { name: "Scan to connect" }));
    const actualQR = screen.getByTitle("Wi-Fi connection QR code").outerHTML;
    expect(hotspotDraft.current).toEqual(draft);
    view.unmount();
    // Compare the real SVG with the same session and no stale draft.
    hotspotDraft.current = { ssid: "My hotspot", password: "saved-pass", band: "auto" };
    const clean = render(<HotspotPanel />);
    await waitFor(() => expect(screen.getByRole("button", { name: "Scan to connect" }).hasAttribute("disabled")).toBe(false));
    fireEvent.click(screen.getByRole("button", { name: "Scan to connect" }));
    expect(screen.getByTitle("Wi-Fi connection QR code").outerHTML).toBe(actualQR);
    clean.unmount();
    hotspotDraft.current = draft;
    render(<HotspotPanel />);
    await screen.findByText("Hotspot is on");
    fireEvent.click(screen.getByRole("switch", { name: "Aggregation hotspot" }));
    await screen.findByText("Hotspot is off");
    expect((screen.getByLabelText("Network password") as HTMLInputElement).value).toBe("unsaved-pass");
    expect((screen.getByLabelText("Network name") as HTMLInputElement).value).toBe(ssid);
  });
  it("ignores delayed credentials from a previous hotspot session", async () => {
    vi.useFakeTimers();
    try {
      hotspotDraft.current = { ssid: "My hotspot", password: "unsaved-pass", band: "auto" };
      let resolveOld!: (config: { ssid: string; password: string; band: "auto" }) => void;
      mocks.sessionConfig.mockImplementationOnce(() => new Promise(resolve => { resolveOld = resolve; }));
      mocks.status.mockResolvedValue(running);
      await act(async () => { render(<HotspotPanel />); });
      expect(mocks.sessionConfig).toHaveBeenCalledWith("session-one");
      mocks.status.mockResolvedValue({ ...running, session_id: "session-two" });
      await act(async () => { await vi.advanceTimersByTimeAsync(3000); });
      expect(mocks.sessionConfig).toHaveBeenCalledWith("session-two");
      expect((screen.getByLabelText("Network password") as HTMLInputElement).value).toBe("saved-pass");
      await act(async () => { resolveOld({ ssid: "My hotspot", password: "old-session-pass", band: "auto" }); });
      expect((screen.getByLabelText("Network password") as HTMLInputElement).value).toBe("saved-pass");
    } finally { cleanup(); vi.useRealTimers(); }
  });
  it("does not expose a draft password when session credential loading fails", async () => {
    hotspotDraft.current = { ssid: "My hotspot", password: "unsaved-pass", band: "auto" };
    mocks.status.mockResolvedValue(running);
    mocks.sessionConfig.mockRejectedValueOnce(new Error("session changed"));
    render(<HotspotPanel />);
    await screen.findByText("Cannot read this hotspot's connection details.");
    expect((screen.getByLabelText("Network password") as HTMLInputElement).value).toBe("");
    expect(screen.getByRole("button", { name: "Copy password" }).hasAttribute("disabled")).toBe(true);
    expect(screen.getByRole("button", { name: "Scan to connect" }).hasAttribute("disabled")).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    await waitFor(() => expect((screen.getByLabelText("Network password") as HTMLInputElement).value).toBe("saved-pass"));
  });
  it.each([true, false])("reports cleanup failure accurately when off confirmation is %s", async (off) => {
    mocks.status.mockResolvedValue({ ...stopped, state: "failed", cleanup_complete: false, hotspot_off_confirmed: off, configuration_restored: false });
    render(<HotspotPanel />);
    expect(await screen.findByText(off ? /The hotspot is off, but its original settings/ : /Hotspot shutdown is unconfirmed/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: /Retry/ })).toBeNull();
    expect(mocks.stop).not.toHaveBeenCalled();
  });
  it("generates an initial password without starting or saving the hotspot", async () => {
    render(<HotspotPanel />);
    await waitFor(() => expect((screen.getByLabelText("Network password") as HTMLInputElement).value).toMatch(/^[A-Za-z0-9_-]{16}$/));
    expect(mocks.start).not.toHaveBeenCalled();
    expect(mocks.save).not.toHaveBeenCalled();
  });
  it("shows a connection QR only on request while the hotspot is running", async () => {
    mocks.preferences.mockResolvedValue({ ssid: "My hotspot", password: "saved-pass", band: "auto" });
    mocks.status.mockResolvedValue({ ...running, devices_available: true, devices: [{mac:"AA:BB", hosts:["QR test phone"]}] });
    render(<HotspotPanel />);
    const show = await screen.findByRole("button", { name: "Scan to connect" });
    expect(screen.queryByTitle("Wi-Fi connection QR code")).toBeNull();
    fireEvent.click(show);
    expect(screen.getByTitle("Wi-Fi connection QR code")).toBeTruthy();
    expect(screen.queryByText("QR test phone")).toBeNull();
    fireEvent.click(screen.getByRole("button", {name:"Hide connection code"}));
    expect(screen.getByText("QR test phone")).toBeTruthy();
    expect(screen.queryByTitle("Wi-Fi connection QR code")).toBeNull();
    fireEvent.click(show);
    fireEvent.click(screen.getByRole("switch", { name: "Aggregation hotspot" }));
    await screen.findByText("Hotspot is off");
    expect(screen.queryByTitle("Wi-Fi connection QR code")).toBeNull();
  });
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
    expect(screen.getByRole("button", { name: "Save settings" }).hasAttribute("disabled")).toBe(false);
  });
  it("saves while running without restarting the hotspot or hiding its QR", async () => {
    const config = { ssid: "My hotspot", password: "saved-pass", band: "auto" };
    mocks.preferences.mockResolvedValue(config);
    mocks.status.mockResolvedValue(running);
    let finishSave!: () => void;
    mocks.save.mockImplementation(() => new Promise<void>(resolve => { finishSave = resolve; }));
    render(<HotspotPanel />);
    await screen.findByText("Hotspot is on");
    fireEvent.click(screen.getByRole("button", { name: "Scan to connect" }));
    fireEvent.click(screen.getByRole("button", { name: "Save settings" }));
    expect(mocks.save).toHaveBeenCalledWith(config);
    expect(screen.getByRole("button", { name: "Save settings" }).hasAttribute("disabled")).toBe(true);
    expect(screen.getByTitle("Wi-Fi connection QR code")).toBeTruthy();
    finishSave();
    await screen.findByText("Settings saved encrypted and restored next time you open the app.");
    expect(screen.getByTitle("Wi-Fi connection QR code")).toBeTruthy();
    expect(mocks.start).not.toHaveBeenCalled();
    expect(mocks.stop).not.toHaveBeenCalled();
  });
  it("keeps unverified egress details in diagnostics without an orange error", async () => {
    mocks.status.mockResolvedValue({ ...running, sharing_verified: false, message: "Egress verification unavailable" });
    const view = render(<HotspotPanel />);
    await screen.findByText("Hotspot is on · egress unverified");
    expect(screen.getByText("Egress verification unavailable").closest("details")).toBeTruthy();
    expect(view.container.querySelector(".hotspot-feedback .hotspot-error")).toBeNull();
    expect(screen.queryByRole("alert")).toBeNull();
  });
  it("animates internal state changes without remounting the settings inputs", async () => {
    const animated: Element[] = [];
    const animate = vi.fn(function (this: Element) { animated.push(this); return { cancel: vi.fn() }; });
    Object.defineProperty(Element.prototype, "animate", { configurable: true, value: animate });
    try {
      render(<HotspotPanel />);
      await screen.findByText("Hotspot is off");
      const input = screen.getByLabelText("Network name");
      fireEvent.change(input, { target: { value: "My hotspot" } });
      animated.length = 0;
      fireEvent.click(screen.getByRole("switch", { name: "Aggregation hotspot" }));
      await screen.findByText("Hotspot is on");
      expect(screen.getByLabelText("Network name")).toBe(input);
      expect(animated.some(element => element.classList.contains("hotspot-settings-content"))).toBe(true);
      expect(animated.some(element => element.classList.contains("hotspot-connect-body"))).toBe(true);
      animated.length = 0;
      fireEvent.click(screen.getByRole("button", { name: "Scan to connect" }));
      expect(animated.map(element => element.className)).toEqual(["hotspot-connect-body"]);
    } finally {
      Reflect.deleteProperty(Element.prototype, "animate");
    }
  });
  it("restores encrypted preferences and retains edits across navigation", async () => {
    mocks.preferences.mockResolvedValue({ ssid: "Saved network", password: "saved-pass", band: "5" });
    const view = render(<HotspotPanel />);
    await waitFor(() => expect((screen.getByLabelText("Network password") as HTMLInputElement).value).toBe("saved-pass"));
    expect(screen.getByRole("combobox", { name: "Wi-Fi band" }).textContent).toContain("5 GHz");
    fireEvent.click(screen.getByRole("combobox", { name: "Wi-Fi band" }));
    fireEvent.click(await screen.findByRole("option", { name: "2.4 GHz" }));
    fireEvent.change(screen.getByLabelText("Network password"), { target: { value: "edited-pass" } });
    view.unmount();
    render(<HotspotPanel />);
    await screen.findByText("Hotspot is off");
    expect((screen.getByLabelText("Network password") as HTMLInputElement).value).toBe("edited-pass");
    expect((screen.getByLabelText("Network name") as HTMLInputElement).value).toBe("Saved network");
    expect(screen.getByRole("combobox", { name: "Wi-Fi band" }).textContent).toContain("2.4 GHz");
    fireEvent.click(screen.getByRole("button", { name: "Save settings" }));
    await waitFor(() => expect(mocks.save).toHaveBeenCalledWith({ ssid: "Saved network", password: "edited-pass", band: "2.4" }));
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
    expect(screen.getByLabelText("Connected devices: 2").textContent).toBe("2");
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
