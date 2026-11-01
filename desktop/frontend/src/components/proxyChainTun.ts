import type { appServices, RoutingRule } from "../platform/services";

// When HypoMux owns the TUN and a proxy client runs as a plain system proxy, the
// TUN compatibility bypass sends known proxy cores straight to the system NIC.
// An explicit process rule outranks that bypass, so routing the running core to
// aggregation spreads its node connections across adapters. sing-box.exe is
// absent on purpose: HypoMux's own self-loop guard matches it by name ahead of
// user rules, so sing-box based clients use the detour chain instead.
export const CHAIN_TUN_CORE_PROCESSES = [
  // Clash Verge Rev, Mihomo Party, Clash Nyanpasu and other Mihomo clients
  "verge-mihomo.exe", "verge-mihomo-alpha.exe",
  "mihomo.exe", "mihomo-alpha.exe", "mihomo-core.exe",
  "clash.exe", "clash-meta.exe", "clash-win64.exe",
  // FlClash
  "flclashcore.exe",
  // v2rayN and other Xray/V2Ray front ends
  "xray.exe", "v2ray.exe",
  // Hiddify, NekoRay/NekoBox
  "hiddifycli.exe", "hiddify-cli.exe", "nekobox_core.exe", "nekoray_core.exe",
];

type RoutingServices = Pick<typeof appServices.routing, "listProcesses" | "snapshot" | "previewBatch" | "save">;

export type ChainTunResult = {
  detected: string[];
  added: string[];
  existing: string[];
  conflicts: { name: string; outbound: string }[];
  restartRequired: boolean;
};

export async function addChainTunProcessRules(routing: RoutingServices): Promise<ChainTunResult> {
  const known = new Set(CHAIN_TUN_CORE_PROCESSES);
  const running = (await routing.listProcesses()) ?? [];
  const detected = [...new Map(running
    .filter((name) => known.has(name.toLowerCase()))
    .map((name) => [name.toLowerCase(), name])).values()];
  const result: ChainTunResult = { detected, added: [], existing: [], conflicts: [], restartRequired: false };
  if (detected.length === 0) return result;

  const snapshot = await routing.snapshot();
  const rules = snapshot.rules ?? [];
  const preview = await routing.previewBatch("process", detected, "aggregation", rules);
  const additions: RoutingRule[] = [];
  for (const item of preview.items ?? []) {
    if (item.status === "add") {
      additions.push(item.rule);
      result.added.push(item.rule.value);
    } else if (item.status === "conflict") {
      // A rule the user already pointed elsewhere is their decision; leave it.
      result.conflicts.push({ name: item.rule.value, outbound: item.existing_outbound ?? "" });
    } else if (item.status === "duplicate") {
      result.existing.push(item.rule.value);
    }
  }
  if (additions.length > 0) {
    const saved = await routing.save([...rules, ...additions], snapshot.match_order ?? undefined, snapshot.revision);
    result.restartRequired = Boolean(saved?.restart_required);
  }
  return result;
}
