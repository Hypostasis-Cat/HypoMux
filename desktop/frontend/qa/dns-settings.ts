// Development-only UI acceptance harness. Uses the real app and settings page
// with local fixtures; it never modifies Windows networking or user settings.
import { appServices } from "../src/platform/services";
import { isDesktopRuntime } from "../src/platform/runtime";

if (!import.meta.env.DEV || isDesktopRuntime()) throw new Error("DNS acceptance fixture requires the development browser preview");

const fixture = new URLSearchParams(location.search).get("fixture");
const storageKey = `hypomux.qa.dns-settings${fixture ? `.${fixture}` : ""}`;
const defaults = {
  ai_enabled: false, mode: "tun", language: "zh", socks_port: 10800, http_port: 10801,
  tun_stack: "system", system_proxy_takeover: true, strict_route: true, update_channel: "stable",
  dns_server: "223.5.5.5", dns_policy: "auto", dns_egress_mode: "auto", dns_adapter_id: "",
  selected_adapter_ids: [], adapter_weights: {}, routing_rules: [],
};
let settings = JSON.parse(localStorage.getItem(storageKey) ?? JSON.stringify(defaults));
Object.assign(appServices.settings, {
  get: async () => structuredClone(settings),
  update: async (next: Record<string, unknown>, fields: string[]) => {
    for (const field of fields) settings[field] = structuredClone(next[field]);
    localStorage.setItem(storageKey, JSON.stringify(settings));
    return structuredClone(settings);
  },
  configPath: async () => "浏览器验收 · 配置仅保存在本页，未连接桌面服务",
  migrationStatus: async () => ({ legacy_found: false, applied: false, message: "" }),
});
Object.assign(appServices.engine, {
  snapshot: async () => ({ running: false, mode: "tun", adapters: [] }),
  connections: async () => ({ phase: "stopped", mode: "tun", sampled_at: new Date().toISOString(), connections: [] }),
});
appServices.adapters.list = async () => [];

await import("../src/main");
