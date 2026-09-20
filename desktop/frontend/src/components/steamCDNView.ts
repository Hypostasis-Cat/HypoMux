import type { SteamCDNStatus } from "../platform/services";

export type SteamNode = SteamCDNStatus["entries"][number];
export type SteamNodeState = "paused" | "cooldown" | "expired" | "preferred" | "trial" | "verified" | "unavailable" | "original";

export function steamNodeState(entry: SteamNode, now = Date.now()): SteamNodeState {
  if (entry.decision_reason === "route_paused") return "paused";
  if (Date.parse(entry.cooldown_until) > now) return "cooldown";
  if (entry.decision_reason === "validation_expired" || (entry.validated && Date.parse(entry.expires_at) <= now)) return "expired";
  if (entry.validated && entry.preferred) return "preferred";
  if (entry.validated && (entry.switched_active ?? 0) > 0) return "trial";
  if (entry.validated) return "verified";
  return entry.source ? "unavailable" : "original";
}

export function formatSteamBytes(value: number): string {
  const bytes = Math.max(0, Number.isFinite(value) ? value : 0);
  const unit = bytes >= 1073741824 ? "GiB" : bytes >= 1048576 ? "MiB" : bytes >= 1024 ? "KiB" : "B";
  const divisor = { GiB: 1073741824, MiB: 1048576, KiB: 1024, B: 1 }[unit];
  return `${(bytes / divisor).toFixed(unit === "B" ? 0 : 2)} ${unit}`;
}

export function steamGuidance(status: SteamCDNStatus | undefined, enabled: boolean, en: boolean) {
  const text = (zh: string, english: string) => en ? english : zh;
  if (!enabled) return { title: text("下载优选已关闭", "Optimization is off"), body: text("开启上方开关后，工具会根据真实下载学习节点；已有连接继续传输。", "Enable the switch above to learn nodes from real downloads. Existing transfers continue.") };
  if (status?.runtime_state === "unsupported") return { title: text("需要更新 Core", "Core update required"), body: text("当前核心不支持下载优选，请在设置中更新核心后重试。", "This Core does not support download optimization. Update it in Settings and try again.") };
  if (!status?.available || !status.enabled) return { title: text("已开启，等待引擎运行", "Enabled, waiting for the engine"), body: text("前往首页启动聚合，再开始 Steam 下载。设置已保存，将随引擎生效。", "Start aggregation on Home, then start a Steam download. Your saved preference will take effect with the engine.") };
  if (status.probing > 0) return { title: text("正在验证候选节点", "Validating candidate nodes"), body: text("下载仍正常进行。验证通过后先有限试用，再与原节点的真实吞吐比较。", "Downloads continue while candidates are checked, tried with limited connections, and compared with the original node.") };
  if (!(status.recognized ?? 0)) return { title: text("准备就绪，等待 Steam 下载", "Ready for a Steam download"), body: text("在 Steam 中开始或继续下载，并确保流量经过 HypoMux。系统代理模式下，Steam 流量需要单独接入代理；也可使用 TUN 模式。", "Start or resume a Steam download and route its traffic through HypoMux. In system proxy mode, Steam traffic needs proxy routing; TUN mode is another option.") };
  if (status.entries.some(e => steamNodeState(e) === "preferred")) return { title: text("已找到优先节点", "Preferred nodes available"), body: text("已通过连续窗口的实际下载比较，将优先分配新连接。节点表现变化时会重新评估。", "Repeated download comparisons confirmed an advantage. New connections prefer these nodes while performance is re-evaluated.") };
  if (status.entries.some(e => e.validated)) return { title: text("正在学习下载表现", "Learning download performance"), body: text("候选已验证，正在积累可比较的下载样本。短测速度仅用于安排试用，不代表实际提速。", "Verified candidates are collecting comparable download samples. Short tests order trials and do not measure a speedup.") };
  return { title: text("已识别下载，当前沿用原节点", "Download recognized, using original nodes"), body: text("目前没有可采用的候选。单地址、鉴权或协议限制都可能导致原路下载，展开诊断可查看具体原因。", "No eligible alternative is available yet. Single-address hosts, authentication or protocol limits can retain the original route; see diagnostics for details.") };
}
