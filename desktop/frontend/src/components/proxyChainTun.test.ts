import { describe, expect, it, vi } from "vitest";
import { addChainTunProcessRules, summarizeChainConnections } from "./proxyChainTun";

const routingFixture = (processes: string[], existing: { match_type: string; value: string; outbound: string }[] = []) => {
  const listProcesses = vi.fn().mockResolvedValue(processes);
  const snapshot = vi.fn().mockResolvedValue({ revision: "rev-1", match_order: ["domain", "process", "ip"], rules: existing, outbounds: [] });
  // Mirrors RoutingRuleService.PreviewBatch identity and conflict handling.
  const previewBatch = vi.fn(async (matchType: string, values: string[], outbound: string, rules: typeof existing) => ({
    items: values.map((value) => {
      const current = rules.find((rule) => rule.match_type === matchType && rule.value.toLowerCase() === value.toLowerCase());
      const rule = { match_type: matchType, value, outbound };
      if (!current) return { input: value, status: "add", rule };
      return { input: value, status: current.outbound === outbound ? "duplicate" : "conflict", rule, existing_outbound: current.outbound };
    }),
  }));
  const save = vi.fn().mockResolvedValue({ restart_required: false });
  return { listProcesses, snapshot, previewBatch, save } as any;
};

describe("addChainTunProcessRules", () => {
  it("routes every running proxy core to aggregation and keeps the saved order", async () => {
    const routing = routingFixture(["explorer.exe", "verge-mihomo.exe", "Xray.exe", "verge-mihomo.exe", "sing-box.exe"]);

    const result = await addChainTunProcessRules(routing);

    expect(result.detected).toEqual(["verge-mihomo.exe", "Xray.exe"]);
    expect(result.added).toEqual(["verge-mihomo.exe", "Xray.exe"]);
    expect(routing.previewBatch).toHaveBeenCalledWith("process", ["verge-mihomo.exe", "Xray.exe"], "aggregation", []);
    expect(routing.save).toHaveBeenCalledWith(
      [
        { match_type: "process", value: "verge-mihomo.exe", outbound: "aggregation" },
        { match_type: "process", value: "Xray.exe", outbound: "aggregation" },
      ],
      ["domain", "process", "ip"],
      "rev-1",
    );
  });

  it("leaves rules the user already pointed elsewhere untouched", async () => {
    const existing = [
      { match_type: "process", value: "mihomo.exe", outbound: "direct" },
      { match_type: "process", value: "FlClashCore.exe", outbound: "aggregation" },
    ];
    const routing = routingFixture(["mihomo.exe", "FlClashCore.exe"], existing);

    const result = await addChainTunProcessRules(routing);

    expect(result.conflicts).toEqual([{ name: "mihomo.exe", outbound: "direct" }]);
    expect(result.existing).toEqual(["FlClashCore.exe"]);
    expect(result.added).toEqual([]);
    expect(routing.save).not.toHaveBeenCalled();
  });

  it("does not touch routing when no proxy core is running", async () => {
    const routing = routingFixture(["sing-box.exe", "chrome.exe"]);

    const result = await addChainTunProcessRules(routing);

    expect(result.detected).toEqual([]);
    expect(routing.snapshot).not.toHaveBeenCalled();
    expect(routing.save).not.toHaveBeenCalled();
  });
});

describe("summarizeChainConnections", () => {
  const connection = (id: number, process: string, adapter: string) =>
    ({ id, process, adapter, protocol: "socks5", outbound: "aggregation", started_at: "", bytes_up: 0, bytes_down: 0 });

  it("reports proxy cores and the adapters they leave through", () => {
    expect(summarizeChainConnections({
      phase: "running", mode: "proxy", sampled_at: "",
      connections: [connection(1, "Xray.exe", "WLAN"), connection(2, "chrome.exe", "以太网"), connection(3, "sing-box.exe", "以太网")],
    })).toEqual({ state: "active", processes: ["Xray.exe", "sing-box.exe"], connections: 2, adapters: ["WLAN", "以太网"] });
  });

  it("separates a running engine without chained traffic from a stopped one", () => {
    expect(summarizeChainConnections({ phase: "running", mode: "tun", sampled_at: "", connections: [connection(1, "chrome.exe", "WLAN")] }))
      .toEqual({ state: "waiting" });
    expect(summarizeChainConnections({ phase: "stopped", mode: "proxy", sampled_at: "", connections: [] }))
      .toEqual({ state: "stopped" });
  });
});
