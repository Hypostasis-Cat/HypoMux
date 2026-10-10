// @vitest-environment jsdom

import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { defaultAppearance } from "../theme/appearance.presets";
import { ToolsPage } from "./ToolsPage";
import { SettingsPage } from "./SettingsPage";
import { PageActivity } from "../components/shell/PageActivity";
import { AI_AVAILABILITY_EVENT } from "../state/aiAvailability";

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
  window.history.replaceState(null, "", "/");
  mocks.get.mockResolvedValue(initial);
  mocks.setSteamCDNEnabled.mockImplementation(async (enabled) => {
    const saved = { ...initial, steam_cdn_enabled: enabled };
    mocks.get.mockResolvedValue(saved);
    return saved;
  });
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

async function openCategory(name: string) {
  fireEvent.click(screen.getByRole("tab", { name }));
  await waitFor(() => expect(screen.getByRole("tab", { name }).getAttribute("aria-selected")).toBe("true"));
}

it("shows common preferences first and exposes one category at a time", async () => {
  render(<SettingsPage adapterRuntime={[]} onOpenBlockedDomains={() => {}} />);
  await screen.findByText("Settings synced");
  expect(screen.getByRole("tabpanel", { name: "General" })).toBeTruthy();
  expect(screen.getByRole("switch", { name: "Connect Wi-Fi at startup" })).toBeTruthy();
  expect(screen.queryByRole("textbox", { name: "DNS 1" })).toBeNull();
  expect(screen.queryByRole("switch", { name: "Enable AI features" })).toBeNull();
  await openCategory("Appearance");
  expect(screen.getByRole("tabpanel", { name: "Appearance" })).toBeTruthy();
  expect(screen.getByRole("button", { name: /^Default background/ })).toBeTruthy();
  expect(screen.queryByRole("switch", { name: "Connect Wi-Fi at startup" })).toBeNull();
  expect(new URLSearchParams(window.location.search).get("settingsCategory")).toBe("appearance");
});

it("opens a linked category and restores category navigation with browser back", async () => {
  window.history.replaceState(null, "", "/?settingsCategory=network");
  render(<SettingsPage adapterRuntime={[]} onOpenBlockedDomains={() => {}} />);
  await screen.findByText("Settings synced");
  expect(screen.getByRole("tabpanel", { name: "Network & DNS" })).toBeTruthy();
  fireEvent.change(screen.getByRole("textbox", { name: "DNS 1" }), { target: { value: "1.1.1.1" } });
  await openCategory("General");
  expect(screen.getByRole("button", { name: "Review changes" })).toBeTruthy();
  await act(async () => window.history.back());
  await screen.findByRole("tabpanel", { name: "Network & DNS" });
  expect((screen.getByRole("textbox", { name: "DNS 1" }) as HTMLInputElement).value).toBe("1.1.1.1");
  expect(mocks.update).not.toHaveBeenCalled();
});

it("persists the global AI switch and removes AI settings and companion controls", async () => {
  const changed = vi.fn();
  window.addEventListener(AI_AVAILABILITY_EVENT, changed);
  try {
    render(<SettingsPage adapterRuntime={[]} onOpenBlockedDomains={() => {}} />);
    await openCategory("AI Assistant");
    await screen.findByText("Settings synced");
    expect(screen.getByRole("button", { name: "AI assistant settings" })).toBeTruthy();
    const toggle = screen.getByRole("switch", { name: "Enable AI features" }) as HTMLInputElement;
    expect(toggle.checked).toBe(true);
    fireEvent.click(toggle);
    await waitFor(() => expect(mocks.update).toHaveBeenCalledWith(expect.objectContaining({ ai_enabled: false }), ["ai_enabled"]));
    await waitFor(() => expect(changed.mock.calls.some(([event]) => event.detail === false)).toBe(true));
    expect(toggle.checked).toBe(false);
    expect(screen.queryByRole("button", { name: "AI assistant settings" })).toBeNull();
    expect(screen.queryByRole("switch", { name: "Show AI companion" })).toBeNull();
    fireEvent.click(toggle);
    await waitFor(() => expect(mocks.update).toHaveBeenLastCalledWith(expect.objectContaining({ ai_enabled: true }), ["ai_enabled"]));
    expect(await screen.findByRole("button", { name: "AI assistant settings" })).toBeTruthy();
  } finally { window.removeEventListener(AI_AVAILABILITY_EVENT, changed); }
});

it("publishes AI changes only after save succeeds and restores failed saves", async () => {
  let reject!: (error: Error) => void;
  mocks.update.mockReturnValue(new Promise((_, fail) => { reject = fail; }));
  const changed = vi.fn();
  window.addEventListener(AI_AVAILABILITY_EVENT, changed);
  try {
    render(<SettingsPage adapterRuntime={[]} onOpenBlockedDomains={() => {}} />);
    await openCategory("AI Assistant");
    await screen.findByText("Settings synced");
    changed.mockClear();
    const toggle = screen.getByRole("switch", { name: "Enable AI features" }) as HTMLInputElement;
    fireEvent.click(toggle);
    await waitFor(() => expect(mocks.update).toHaveBeenCalled());
    expect(toggle.disabled).toBe(true);
    expect(changed).not.toHaveBeenCalled();
    await act(async () => reject(new Error("disk full")));
    await waitFor(() => expect(toggle.checked).toBe(true));
    expect(changed.mock.calls.every(([event]) => event.detail === true)).toBe(true);
    expect(screen.getByRole("button", { name: "AI assistant settings" })).toBeTruthy();
  } finally { window.removeEventListener(AI_AVAILABILITY_EVENT, changed); }
});

it("loads persisted AI opt-out and exposes the switch in Chinese", async () => {
  mocks.locale = "zh";
  mocks.get.mockResolvedValue({ ...initial, ai_enabled: false });
  render(<SettingsPage adapterRuntime={[]} onOpenBlockedDomains={() => {}} />);
    await openCategory("AI 助手");
  await screen.findByText("配置已同步");
  expect((screen.getByRole("switch", { name: "启用 AI 功能" }) as HTMLInputElement).checked).toBe(false);
  expect(screen.queryByRole("button", { name: "AI 助手设置" })).toBeNull();
  expect(screen.queryByRole("switch", { name: "显示 AI 小精灵" })).toBeNull();
});

describe("TUN settings", () => {
  it("defaults to stable and persists the chosen update channel separately", async () => {
    const view = render(<SettingsPage adapterRuntime={[]} onOpenBlockedDomains={() => {}} />);
    await screen.findByText("Settings synced");
    const channel = screen.getByRole("combobox", { name: "Update channel" });
    expect(channel.textContent).toContain("Stable");
    fireEvent.click(channel);
    fireEvent.click(await screen.findByRole("option", { name: "Preview (Beta / RC)" }));
    await waitFor(() => expect(mocks.update).toHaveBeenCalledWith(
      expect.objectContaining({ update_channel: "preview" }), ["update_channel"],
    ));
    mocks.get.mockResolvedValue({ ...initial, update_channel: "preview" });
    view.unmount();
    render(<SettingsPage adapterRuntime={[]} onOpenBlockedDomains={() => {}} />);
    await waitFor(() => expect(screen.getByRole("combobox", { name: "Update channel" }).textContent).toContain("Preview"));
  });
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
    await waitFor(() => expect(mocks.update).toHaveBeenCalledWith(expect.objectContaining({ hide_virtual_adapters: false }), ["hide_virtual_adapters"]));
  });
  it.each([
    ["Mixed (hybrid)", "mixed"],
    ["gVisor (userspace)", "gvisor"],
  ])("persists %s and explains that it applies on the next start", async (label, stack) => {
    render(<SettingsPage adapterRuntime={[]} onOpenBlockedDomains={() => {}} />);
    await openCategory("Advanced Network");
    const dropdown = await screen.findByRole("combobox", { name: "TUN stack" });
    await waitFor(() => expect(dropdown.hasAttribute("disabled")).toBe(false));
    expect(dropdown.textContent).toContain("System (default)");
    fireEvent.click(dropdown);
    fireEvent.click(await screen.findByRole("option", { name: label }));
    await waitFor(() => expect(mocks.update).toHaveBeenCalledWith(expect.objectContaining({
      tun_stack: stack, dns_policy: "auto", strict_route: true,
    }), ["tun_stack"]));
    await waitFor(() => expect(mocks.notify).toHaveBeenCalledWith(expect.objectContaining({
      message: "TUN stack saved; applies the next time TUN starts",
    })));
    expect(screen.getByText(/cache\/sing-box.db/)).toBeTruthy();
  });

  it("shows the saved stack and Chinese labels", async () => {
    mocks.locale = "zh";
    mocks.get.mockResolvedValue({ ...initial, language: "zh", tun_stack: "gvisor" });
    render(<SettingsPage adapterRuntime={[]} onOpenBlockedDomains={() => {}} />);
    await openCategory("高级网络");
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
  await waitFor(() => expect(mocks.update).toHaveBeenCalledWith(expect.objectContaining({ auto_connect_wifi: true }), ["auto_connect_wifi"]));
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
  it("validates and saves a custom DoT policy with multiple servers", async () => {
    render(<SettingsPage adapterRuntime={[]} onOpenBlockedDomains={() => {}} />);
    await openCategory("Network & DNS");
    await screen.findByText("Settings synced");
    fireEvent.click(screen.getByRole("combobox", { name: "DNS resolution policy" }));
    fireEvent.click(await screen.findByRole("option", { name: "Custom DoT only" }));
    fireEvent.click(screen.getByRole("button", { name: "Save ports and DNS" }));
    expect(await screen.findByText("Add a DoT URL or change the policy.")).toBeTruthy();
    expect(mocks.update).not.toHaveBeenCalled();
    await waitFor(() => expect(document.activeElement?.textContent).toContain("Add DoT"));
    fireEvent.click(screen.getByRole("button", { name: "Add DoT" }));
    fireEvent.change(screen.getByRole("textbox", { name: "DoT 1" }), { target: { value: "tls://dns.example.com/query" } });
    fireEvent.click(screen.getByRole("button", { name: "Save ports and DNS" }));
    expect(await screen.findByText(/Enter tls:\/\/hostname/)).toBeTruthy();
    expect(mocks.update).not.toHaveBeenCalled();
    await waitFor(() => expect(document.activeElement?.getAttribute("name")).toBe("dot_server_1"));
    fireEvent.change(screen.getByRole("textbox", { name: "DoT 1" }), { target: { value: "tls://dns.example.com:8853" } });
    fireEvent.click(screen.getByRole("button", { name: "Add DoT" }));
    fireEvent.change(screen.getByRole("textbox", { name: "DoT 2" }), { target: { value: "tls://[2001:db8::53]" } });
    fireEvent.click(screen.getByRole("button", { name: "Save ports and DNS" }));
    await waitFor(() => expect(mocks.update).toHaveBeenLastCalledWith(expect.objectContaining({ dns_policy: "dot", dot_servers: ["tls://dns.example.com:8853", "tls://[2001:db8::53]"] }), ["dns_policy", "dot_servers"]));
    await screen.findByText("Settings synced");
    fireEvent.click(screen.getByRole("button", { name: "Remove DoT 1" }));
    expect((screen.getByRole("textbox", { name: "DoT 1" }) as HTMLInputElement).value).toBe("tls://[2001:db8::53]");
  });

  it("adds multiple DNS and DoH servers, removes entries, and saves the complete list", async () => {
    render(<SettingsPage adapterRuntime={[]} onOpenBlockedDomains={() => {}} />);
    await openCategory("Network & DNS");
    await screen.findByText("Settings synced");
    fireEvent.click(screen.getByRole("button", { name: "Add DNS" }));
    fireEvent.change(screen.getByRole("textbox", { name: "DNS 2" }), { target: { value: "1.1.1.1" } });
    fireEvent.click(screen.getByRole("button", { name: "Add DoH" }));
    fireEvent.change(screen.getByRole("textbox", { name: "DoH 1" }), { target: { value: "https://dns.example.com:8443/query?key=hello" } });
    fireEvent.click(screen.getByRole("button", { name: "Add DoH" }));
    fireEvent.change(screen.getByRole("textbox", { name: "DoH 2" }), { target: { value: "https://dns.google/dns-query" } });
    fireEvent.click(screen.getByRole("button", { name: "Save ports and DNS" }));
    await waitFor(() => expect(mocks.update).toHaveBeenLastCalledWith(expect.objectContaining({ dns_servers: ["223.5.5.5", "1.1.1.1"], doh_servers: ["https://dns.example.com:8443/query?key=hello", "https://dns.google/dns-query"] }), ["dns_server", "dns_servers", "doh_servers"]));
    await screen.findByText("Settings synced");
    fireEvent.click(screen.getByRole("button", { name: "Remove DoH 1" }));
    expect((screen.getByRole("textbox", { name: "DoH 1" }) as HTMLInputElement).value).toBe("https://dns.google/dns-query");
    fireEvent.click(screen.getByRole("button", { name: "Remove DNS 2" }));
    expect(screen.getByRole("button", { name: "Remove DNS 1" }).hasAttribute("disabled")).toBe(true);
  });

  it("shows errors beside invalid addresses and prevents saving until corrected", async () => {
    render(<SettingsPage adapterRuntime={[]} onOpenBlockedDomains={() => {}} />);
    await openCategory("Network & DNS");
    await screen.findByText("Settings synced");
    fireEvent.click(screen.getByRole("button", { name: "Add DoH" }));
    fireEvent.change(screen.getByRole("textbox", { name: "DoH 1" }), { target: { value: "http://dns.example.com/query" } });
    fireEvent.click(screen.getByRole("button", { name: "Save ports and DNS" }));
    expect(await screen.findByText(/Enter a full HTTPS URL/)).toBeTruthy();
    expect(mocks.update).not.toHaveBeenCalled();
    await waitFor(() => expect(document.activeElement?.getAttribute("name")).toBe("doh_server_1"));
    fireEvent.change(screen.getByRole("textbox", { name: "DoH 1" }), { target: { value: "https://dns.example.com/query" } });
    fireEvent.click(screen.getByRole("button", { name: "Save ports and DNS" }));
    await waitFor(() => expect(mocks.update).toHaveBeenCalledTimes(1));
  });
  it("does not include unfinished ports or DNS in an unrelated automatic save", async () => {
    render(<SettingsPage adapterRuntime={[]} onOpenBlockedDomains={() => {}} />);
    await openCategory("Network & DNS");
    await screen.findByText("Settings synced");
    fireEvent.change(screen.getByRole("spinbutton", { name: "HTTP" }), { target: { value: "12345" } });
    fireEvent.change(screen.getByRole("textbox", { name: "DNS 1" }), { target: { value: "1.1.1.1" } });
    expect(screen.getByText("Unsaved port and DNS changes")).toBeTruthy();
    await openCategory("General");
    fireEvent.click(screen.getByRole("switch", { name: "Hide virtual adapters on Home" }));
    await waitFor(() => expect(mocks.update).toHaveBeenCalledWith(expect.objectContaining({ http_port: 10801, dns_server: "223.5.5.5", hide_virtual_adapters: false }), ["hide_virtual_adapters"]));
    await openCategory("Network & DNS");
    const save = screen.getByRole("button", { name: "Save ports and DNS" });
    await waitFor(() => expect(save.hasAttribute("disabled")).toBe(false));
    expect((screen.getByRole("spinbutton", { name: "HTTP" }) as HTMLInputElement).value).toBe("12345");
    fireEvent.click(save);
    await waitFor(() => expect(mocks.update).toHaveBeenLastCalledWith(expect.objectContaining({ http_port: 12345, dns_server: "1.1.1.1", dns_servers: ["1.1.1.1"] }), ["http_port", "dns_server", "dns_servers"]));
    await screen.findByText("Settings synced");
  });
  it("keeps the draft editable and retryable when saving and recovery both fail", async () => {
    render(<SettingsPage adapterRuntime={[]} onOpenBlockedDomains={() => {}} />);
    await openCategory("Network & DNS");
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
    await openCategory("Network & DNS");
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

it("preserves an AI rule saved after loading the settings page", async () => {
  let persisted: any = { ...initial, routing_rules: [] };
  mocks.get.mockImplementation(async () => persisted);
  mocks.update.mockImplementation(async (next, fields: string[]) => {
    persisted = { ...persisted, ...Object.fromEntries(fields.map(field => [field, next[field]])) };
    return persisted;
  });
  render(<SettingsPage adapterRuntime={[]} onOpenBlockedDomains={() => {}} />);
  await screen.findByText("Settings synced");
  const rule = { match_type: "process", value: "cs2.exe", outbound: "direct", priority: 1 };
  persisted = { ...persisted, routing_rules: [rule] };
  // The save must be safe even before the next AI notification arrives.
  fireEvent.click(screen.getByRole("switch", { name: "Hide virtual adapters on Home" }));
  await waitFor(() => expect(mocks.update).toHaveBeenCalledOnce());
  expect(mocks.update.mock.calls[0][1]).toEqual(["hide_virtual_adapters"]);
  expect(persisted.routing_rules).toEqual([rule]);
});

it("refreshes AI changes without discarding the manual network draft", async () => {
  render(<SettingsPage adapterRuntime={[]} onOpenBlockedDomains={() => {}} />);
    await openCategory("Network & DNS");
  await screen.findByText("Settings synced");
  fireEvent.change(screen.getByRole("spinbutton", { name: "HTTP" }), { target: { value: "12345" } });
  mocks.get.mockResolvedValue({ ...initial, hide_virtual_adapters: false });
  act(() => { window.dispatchEvent(new CustomEvent("hypomux:ai-changed")); });
  await openCategory("General");
  await waitFor(() => expect((screen.getByRole("switch", { name: "Hide virtual adapters on Home" }) as HTMLInputElement).checked).toBe(false));
  await openCategory("Network & DNS");
  expect((screen.getByRole("spinbutton", { name: "HTTP" }) as HTMLInputElement).value).toBe("12345");
  expect(screen.getByText("Unsaved port and DNS changes")).toBeTruthy();
});

it("refreshes Steam preferences when returning from AI and while already visible", async () => {
  const show = (active: boolean) => <PageActivity.Provider value={active}><ToolsPage /></PageActivity.Provider>;
  const view = render(show(true));
  const toggle = await screen.findByRole("switch", { name: "Steam download optimization" });
  await waitFor(() => expect(toggle.hasAttribute("disabled")).toBe(false));
  view.rerender(show(false));
  mocks.get.mockResolvedValue({ ...initial, steam_cdn_enabled: true });
  view.rerender(show(true));
  await waitFor(() => expect((toggle as HTMLInputElement).checked).toBe(true));
  mocks.get.mockResolvedValue({ ...initial, steam_cdn_enabled: false });
  act(() => { window.dispatchEvent(new CustomEvent("hypomux:ai-changed")); });
  await waitFor(() => expect((toggle as HTMLInputElement).checked).toBe(false));
});

it("ignores an old Steam preference response after a newer AI change", async () => {
  render(<ToolsPage />);
  const toggle = await screen.findByRole("switch", { name: "Steam download optimization" });
  await waitFor(() => expect(toggle.hasAttribute("disabled")).toBe(false));
  let finish!: (value: unknown) => void;
  mocks.get.mockImplementationOnce(() => new Promise(resolve => { finish = resolve; }));
  act(() => { window.dispatchEvent(new CustomEvent("hypomux:ai-changed")); });
  await waitFor(() => expect(finish).toBeTypeOf("function"));
  mocks.get.mockResolvedValue({ ...initial, steam_cdn_enabled: true });
  act(() => { window.dispatchEvent(new CustomEvent("hypomux:ai-changed")); });
  await waitFor(() => expect((toggle as HTMLInputElement).checked).toBe(true));
  await act(async () => { finish({ ...initial, steam_cdn_enabled: false }); });
  expect((toggle as HTMLInputElement).checked).toBe(true);
});
