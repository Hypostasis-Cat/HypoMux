// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { defaultAppearance } from "../theme/appearance.presets";
import { ToolsPage } from "./ToolsPage";
import { SettingsPage } from "./SettingsPage";

const mocks = vi.hoisted(() => ({
  get: vi.fn(),
  update: vi.fn(),
  notify: vi.fn(),
  locale: "en",
  translate: (key: string) => key,
  setLocale: vi.fn(),
  setSteamCDNEnabled: vi.fn(),
  steamCDNStatus: vi.fn(),
  hotspotPreferences: vi.fn(),
  hotspotStatus: vi.fn(),
}));

vi.mock("../platform/services", () => ({
  appServices: {
    engine: { hotspotPreferences: mocks.hotspotPreferences, setSteamCDNEnabled: mocks.setSteamCDNEnabled, steamCDNStatus: mocks.steamCDNStatus, hotspotStatus: mocks.hotspotStatus },
    settings: {
      get: mocks.get,
      update: mocks.update,
      configPath: async () => "C:\\HypoMux\\settings.json",
      migrationStatus: async () => ({ legacy_found: false, applied: false, message: "" }),
    },
  },
}));
vi.mock("../platform/desktop", () => ({ desktopPlatform: {} }));
vi.mock("../theme/appearance.store", () => ({
  useAppearance: () => ({ settings: defaultAppearance, update: vi.fn() }),
}));
vi.mock("../components/notifications/AppNotifications", () => ({
  useAppNotifications: () => ({ notify: mocks.notify }),
}));
vi.mock("../i18n/i18n", () => ({
  useI18n: () => ({ locale: mocks.locale, t: mocks.translate, setLocale: mocks.setLocale }),
}));

const initial = {
  mode: "tun", language: "en", tun_stack: "system", socks_port: 10800, http_port: 10801,
  system_proxy_takeover: true, strict_route: true, dns_server: "223.5.5.5", dns_policy: "auto",
  dns_egress_mode: "auto", selected_adapter_ids: [], adapter_weights: {}, routing_rules: [],
};

beforeEach(() => {
  vi.clearAllMocks();
  mocks.locale = "en";
  mocks.get.mockResolvedValue(initial);
  mocks.setSteamCDNEnabled.mockImplementation(async (enabled) => ({ ...initial, steam_cdn_enabled: enabled }));
  mocks.steamCDNStatus.mockResolvedValue({available: true, enabled: false, probing: 0, replacements: 0, fallbacks: 0, entries: []});
  mocks.hotspotPreferences.mockResolvedValue({ ssid: "HypoMux", password: "", band: "auto" });
  mocks.hotspotStatus.mockResolvedValue({state: "stopped", ready: false, ssid: "", clients: 0, band: "auto", sharing_verified: false});
  mocks.update.mockImplementation(async (settings) => settings);
  class Observer {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
  vi.stubGlobal("ResizeObserver", Observer);
  vi.stubGlobal("IntersectionObserver", Observer);
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("TUN settings", () => {
  it("shows a recoverable read failure instead of claiming settings are synced", async () => {
    mocks.get.mockRejectedValueOnce(new Error("Temporarily unavailable"));
    render(<SettingsPage adapterRuntime={[]} onOpenBlockedDomains={() => {}} />);
    expect(await screen.findByText("Settings unavailable")).toBeTruthy();
    expect(screen.queryByText("Settings synced")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(await screen.findByText("Settings synced")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Retry" })).toBeNull();
  });
  it("opens the aggregation hotspot from the toolbox and restores entry focus", async () => {
    render(<ToolsPage />);
    const entry = screen.getByRole("button", { name: "View aggregation hotspot details" });
    fireEvent.click(entry);
    expect(await screen.findByRole("switch", { name: "Aggregation hotspot" })).toBeTruthy();
    expect(document.activeElement).toBe(screen.getByRole("heading", { level: 1, name: "Aggregation hotspot" }));
    fireEvent.click(screen.getByRole("button", { name: "Back to toolbox" }));
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "View aggregation hotspot details" }));
  });
  it("opens tool details separately and restores focus when returning", async () => {
    render(<ToolsPage />);
    const entry = await screen.findByRole("button", { name: "View Steam download optimization details" });
    expect(screen.queryByRole("button", { name: "Re-evaluate nodes" })).toBeNull();
    expect(screen.queryByText("Global networks")).toBeNull();
    fireEvent.click(entry);
    expect(await screen.findByRole("button", { name: "Re-evaluate nodes" })).toBeTruthy();
    expect(document.activeElement).toBe(screen.getByRole("heading", { level: 1, name: "Steam download optimization" }));
    fireEvent.click(screen.getByRole("button", { name: "Back to toolbox" }));
    expect(screen.queryByRole("button", { name: "Re-evaluate nodes" })).toBeNull();
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "View Steam download optimization details" }));
  });
  it("distinguishes enabled preference from waiting for the engine", async () => {
    mocks.get.mockResolvedValue({ ...initial, steam_cdn_enabled: true });
    render(<ToolsPage />);
    expect(await screen.findByText("Enabled · waiting for engine")).toBeTruthy();
    const toggle = screen.getByRole("switch", { name: "Steam download optimization" });
    fireEvent.click(toggle);
    expect(await screen.findByText("Off")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Back to toolbox" })).toBeNull();
  });
  it("toolbox defaults Steam CDN off and uses the runtime-aware toggle", async () => {
    render(<ToolsPage />);
    const toggle = await screen.findByRole("switch", { name: "Steam download optimization" });
    await waitFor(() => expect(toggle.hasAttribute("disabled")).toBe(false));
    expect((toggle as HTMLInputElement).checked).toBe(false);
    fireEvent.click(toggle);
    await waitFor(() => expect(mocks.setSteamCDNEnabled).toHaveBeenCalledWith(true));
    await waitFor(() => expect(toggle.hasAttribute("disabled")).toBe(false));
    fireEvent.click(toggle);
    await waitFor(() => expect(mocks.setSteamCDNEnabled).toHaveBeenCalledWith(false));
    expect(mocks.update).not.toHaveBeenCalled();
  });

  it("restores the saved toolbox preference when a toggle fails", async () => {
    mocks.get.mockResolvedValue({ ...initial, steam_cdn_enabled: true });
    mocks.setSteamCDNEnabled.mockRejectedValueOnce(new Error("Core unavailable"));
    render(<ToolsPage />);
    const toggle = await screen.findByRole("switch", { name: "Steam download optimization" });
    await waitFor(() => expect(toggle.hasAttribute("disabled")).toBe(false));
    fireEvent.click(toggle);
    await waitFor(() => expect(mocks.notify).toHaveBeenCalledWith(expect.objectContaining({ intent: "error" })));
    await waitFor(() => expect(toggle.hasAttribute("disabled")).toBe(false));
    expect((toggle as HTMLInputElement).checked).toBe(true);
    expect(mocks.update).not.toHaveBeenCalled();
  });

  it("defaults to hiding virtual adapters and persists turning it off", async () => {
    render(<SettingsPage adapterRuntime={[]} onOpenBlockedDomains={() => {}} />);
    const toggle = await screen.findByRole("switch", { name: "Hide virtual adapters on Home" });
    await waitFor(() => expect(toggle.hasAttribute("disabled")).toBe(false));
    expect((toggle as HTMLInputElement).checked).toBe(true);
    fireEvent.click(toggle);
    await waitFor(() => expect(mocks.update).toHaveBeenCalledWith(expect.objectContaining({ hide_virtual_adapters: false })));
  });
  it.each([
    ["Mixed (hybrid)", "mixed"],
    ["gVisor (userspace)", "gvisor"],
  ])("persists %s and explains that it applies on the next start", async (label, stack) => {
    render(<SettingsPage adapterRuntime={[]} onOpenBlockedDomains={() => {}} />);
    const dropdown = await screen.findByRole("combobox", { name: "TUN stack" });
    await waitFor(() => expect(dropdown.hasAttribute("disabled")).toBe(false));
    expect(dropdown.textContent).toContain("System (default)");
    fireEvent.click(dropdown);
    fireEvent.click(await screen.findByRole("option", { name: label }));
    await waitFor(() => expect(mocks.update).toHaveBeenCalledWith(expect.objectContaining({
      tun_stack: stack, dns_policy: "auto", strict_route: true,
    })));
    await waitFor(() => expect(mocks.notify).toHaveBeenCalledWith(expect.objectContaining({
      message: "TUN stack saved; applies the next time TUN starts",
    })));
    expect(screen.getByText(/cache\/sing-box.db/)).toBeTruthy();
  });

  it("shows the saved stack and Chinese labels", async () => {
    mocks.locale = "zh";
    mocks.get.mockResolvedValue({ ...initial, language: "zh", tun_stack: "gvisor" });
    render(<SettingsPage adapterRuntime={[]} onOpenBlockedDomains={() => {}} />);
    await waitFor(() => expect(screen.getByRole("combobox", { name: "TUN 协议栈" }).textContent).toContain("gVisor（用户态）"));
    expect(screen.getByText("FakeIP 与规则集缓存")).toBeTruthy();
  });
});


it("adds Wi-Fi startup control below auto acceleration and persists opt-in", async () => {
  mocks.get.mockResolvedValue({ ...initial, autostart: true, auto_start_engine: true });
  render(<SettingsPage adapterRuntime={[]} onOpenBlockedDomains={() => {}} />);
  const toggle = await screen.findByRole("switch", { name: "Connect Wi-Fi at startup" });
  await waitFor(() => expect(toggle.hasAttribute("disabled")).toBe(false));
  expect((toggle as HTMLInputElement).checked).toBe(false);
  fireEvent.click(toggle);
  await waitFor(() => expect(mocks.update).toHaveBeenCalledWith(expect.objectContaining({ auto_connect_wifi: true })));
});

it("disables Wi-Fi startup control when automatic acceleration is off", async () => {
  mocks.get.mockResolvedValue({ ...initial, autostart: true, auto_start_engine: false, auto_connect_wifi: true });
  render(<SettingsPage adapterRuntime={[]} onOpenBlockedDomains={() => {}} />);
  const toggle = await screen.findByRole("switch", { name: "Connect Wi-Fi at startup" });
  await waitFor(() => expect((toggle as HTMLInputElement).checked).toBe(true));
  expect(toggle.hasAttribute("disabled")).toBe(true);
  expect(mocks.update).not.toHaveBeenCalled();
});


describe("manual network drafts", () => {
  it("does not include unfinished ports or DNS in an unrelated automatic save", async () => {
    render(<SettingsPage adapterRuntime={[]} onOpenBlockedDomains={() => {}} />);
    await screen.findByText("Settings synced");
    fireEvent.change(screen.getByRole("spinbutton", { name: "HTTP" }), { target: { value: "12345" } });
    fireEvent.change(screen.getByRole("textbox", { name: "settings_dns_server" }), { target: { value: "1.1.1.1" } });
    expect(screen.getByText("Unsaved port and DNS changes")).toBeTruthy();
    fireEvent.click(screen.getByRole("switch", { name: "Hide virtual adapters on Home" }));
    await waitFor(() => expect(mocks.update).toHaveBeenCalledWith(expect.objectContaining({ http_port: 10801, dns_server: "223.5.5.5", hide_virtual_adapters: false })));
    const save = screen.getByRole("button", { name: "Save ports and DNS" });
    await waitFor(() => expect(save.hasAttribute("disabled")).toBe(false));
    expect((screen.getByRole("spinbutton", { name: "HTTP" }) as HTMLInputElement).value).toBe("12345");
    fireEvent.click(save);
    await waitFor(() => expect(mocks.update).toHaveBeenLastCalledWith(expect.objectContaining({ http_port: 12345, dns_server: "1.1.1.1" })));
    await screen.findByText("Settings synced");
  });
  it("keeps the draft editable and retryable when saving and recovery both fail", async () => {
    render(<SettingsPage adapterRuntime={[]} onOpenBlockedDomains={() => {}} />);
    await screen.findByText("Settings synced");
    mocks.update.mockRejectedValueOnce(new Error("offline"));
    mocks.get.mockRejectedValueOnce(new Error("offline"));
    fireEvent.change(screen.getByRole("spinbutton", { name: "HTTP" }), { target: { value: "12345" } });
    fireEvent.click(screen.getByRole("button", { name: "Save ports and DNS" }));
    await waitFor(() => expect(mocks.notify).toHaveBeenCalledWith(expect.objectContaining({ title: "Save failed" })));
    await screen.findByText("Unsaved port and DNS changes");
    expect(screen.getByRole("button", { name: "Save ports and DNS" }).hasAttribute("disabled")).toBe(false);
    expect((screen.getByRole("spinbutton", { name: "HTTP" }) as HTMLInputElement).value).toBe("12345");
  });
  it("preserves edits typed while an earlier manual save is in flight", async () => {
    render(<SettingsPage adapterRuntime={[]} onOpenBlockedDomains={() => {}} />);
    await screen.findByText("Settings synced");
    let finish!: (value: unknown) => void;
    mocks.update.mockImplementationOnce(() => new Promise(resolve => { finish = resolve; }));
    const input = screen.getByRole("spinbutton", { name: "HTTP" });
    fireEvent.change(input, { target: { value: "12345" } });
    fireEvent.click(screen.getByRole("button", { name: "Save ports and DNS" }));
    await waitFor(() => expect(mocks.update).toHaveBeenCalledTimes(1));
    fireEvent.change(input, { target: { value: "23456" } });
    finish({ ...initial, http_port: 12345 });
    await screen.findByText("Unsaved port and DNS changes");
    expect((input as HTMLInputElement).value).toBe("23456");
  });
});
