import { Button } from "@fluentui/react-components";
import { useEffect, useState } from "react";
import { appServices, type SteamCDNStatus } from "../platform/services";
import { useI18n } from "../i18n/i18n";

export function SteamCDNPanel({ enabled, saving }: { enabled: boolean; saving: boolean }) {
  const { locale } = useI18n();
  const en = locale === "en";
  const [status, setStatus] = useState<SteamCDNStatus>();
  const [error, setError] = useState("");
  const [resetting, setResetting] = useState(false);
  useEffect(() => {
    if (saving || resetting) return;
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout>;
    const poll = async () => {
      try {
        const next = await appServices.engine.steamCDNStatus();
        if (!cancelled) { setStatus(next); setError(""); }
      } catch (reason) { if (!cancelled) setError(String(reason)); }
      finally { if (!cancelled) timer = setTimeout(poll, 2500); }
    };
    void poll();
    return () => { cancelled = true; clearTimeout(timer); };
  }, [enabled, saving, resetting]);

  const reset = async () => {
    setResetting(true);
    try { setStatus(await appServices.engine.steamCDNStatus(true)); setError(""); }
    catch (reason) { setError(String(reason)); }
    finally { setResetting(false); }
  };
  const entries = status?.entries ?? [];
  return <div className="steam-cdn-panel">
    <p role="status">{error || (!status ? (en ? "Loading optimization status…" : "正在读取优选状态…")
      : !status.available ? (en ? "Start a compatible Core to view optimization status." : "启动支持此功能的 Core 后可查看优选状态。")
      : !status.enabled ? (en ? "Optimization is inactive. If enabled above, it will start with the engine." : "优选当前未运行；若已开启开关，将随引擎启动。")
      : status.probing > 0 ? (en ? "Validating download nodes…" : "正在验证下载节点…")
      : entries.length === 0 ? (en ? "Waiting for Steam downloads. No verified candidates yet; using the original connection." : "等待 Steam 下载；暂无验证通过的候选，沿用原连接。")
      : (en ? `${status.replacements} connections optimized · ${status.fallbacks} connection fallbacks` : `已优选 ${status.replacements} 条连接 · 连接回退 ${status.fallbacks} 次`))}</p>
    <p>{en ? "Uses DNS candidates for the same domain and observed download rates. No extra bulk downloads. Improvement depends on available CDN nodes; disable if performance worsens." : "使用同域名 DNS 候选及真实下载观测速率，不额外下载测速文件。效果取决于可用节点，效果不好可关闭。"}</p>
    <Button disabled={!status?.enabled || saving || resetting} onClick={() => void reset()}>{en ? "Re-evaluate nodes" : "重新评估节点"}</Button>
    {entries.length > 0 && <div style={{ overflowX: "auto", maxHeight: 280 }}>
      <table style={{ width: "100%", textAlign: "left", borderSpacing: "12px 8px" }}>
        <caption>{en ? "Verified nodes (observed throughput, not link capacity)" : "已验证节点（观测吞吐，不代表线路带宽）"}</caption>
        <thead><tr>{(en ? ["Adapter", "Domain", "IP", "Observed rate", "Samples", "State"] : ["网卡", "域名", "IP", "观测速率", "样本", "状态"]).map(label => <th scope="col" key={label}>{label}</th>)}</tr></thead>
        <tbody>{entries.map(entry => <tr key={`${entry.adapter}/${entry.domain}/${entry.port}/${entry.ip}`}>
          <td>{entry.adapter}</td><td>{entry.domain}:{entry.port}</td><td>{entry.ip}</td>
          <td>{entry.samples ? `${(entry.download_bps / 1024 / 1024).toFixed(2)} MiB/s` : "—"}</td><td>{entry.samples}</td>
          <td>{Date.parse(entry.cooldown_until) > Date.now() ? (en ? "Cooling down" : "冷却中") : entry.samples ? (en ? "Learning" : "学习中") : (en ? "Awaiting sample" : "等待采样")}</td>
        </tr>)}</tbody>
      </table>
    </div>}
  </div>;
}
