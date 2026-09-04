// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { defaultAppearance } from "../theme/appearance.presets";
import { SettingsPage } from "./SettingsPage";

const mocks = vi.hoisted(() => ({
  get: vi.fn(),
  update: vi.fn(),
  notify: vi.fn(),
  locale: "en",
  translate: (key: string) => key,
  setLocale: vi.fn(),
}));

vi.mock("../platform/services", () => ({
  appServices: {
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
