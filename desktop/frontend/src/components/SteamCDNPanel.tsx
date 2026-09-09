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
  const stages: Record<string,string> = en ? {
 recognized:"Download recognized; original node retained as baseline", dns_no_candidates:"DNS returned no usable candidates", http_wait_chunk:"Waiting for a public chunk GET request", http_baseline_failed:"Original HTTP content probe failed", http_probe_failed:"HTTP range request failed or was unsupported", http_content_mismatch:"Candidate content differs from original", tls_validation_failed:"TLS connection or certificate validation failed", verified:"Candidate verified",
 } : {recognized:"已识别下载，原节点保留为基准",dns_no_candidates:"DNS 未返回可用候选",http_wait_chunk:"等待无鉴权的公开内容块 GET 请求",http_baseline_failed:"原节点 HTTP 内容校验失败",http_probe_failed:"HTTP 范围校验失败或不受支持",http_content_mismatch:"候选内容与原节点不一致",tls_validation_failed:"TLS 连接或证书校验失败",verified:"候选验证通过"};
  return <div className="steam-cdn-panel">
    <p role="status">{error || (!status ? (en ? "Loading optimization status…" : "正在读取优选状态…")
      : !status.available ? (en ? "Start a compatible Core to view optimization status." : "启动支持此功能的 Core 后可查看优选状态。")
      : !status.enabled ? (en ? "Optimization is inactive. If enabled above, it will start with the engine." : "优选当前未运行；若已开启开关，将随引擎启动。")
      : status.probing > 0 ? (en ? "Validating download nodes…" : "正在验证下载节点…")
      : (status.recognized ?? 0) > 0 && status.replacements === 0 ? (en ? "Steam traffic recognized; using original nodes while candidates are evaluated. See details below." : "已识别 Steam 流量，当前仍使用原节点；候选验证结果见下方详情。")
      : entries.length === 0 ? (en ? "Waiting for Steam downloads. No verified candidates yet; using the original connection." : "等待 Steam 下载；暂无验证通过的候选，沿用原连接。")
      : (en ? `${status.replacements} connections optimized · ${status.fallbacks} connection fallbacks` : `已优选 ${status.replacements} 条连接 · 连接回退 ${status.fallbacks} 次`))}</p>
    <p>{en ? "Uses DNS candidates for the same domain and observed download rates. HTTP verification reads bounded 4 KiB samples. Improvement depends on available CDN nodes; disable if performance worsens." : "使用同域名 DNS 候选及真实下载观测速率，HTTP 校验只读取受限的 4 KiB 样本。效果取决于可用节点，效果不好可关闭。"}</p>
    <p>{en ? `Recognized downloads: ${status?.recognized ?? 0}` : `已识别下载连接：${status?.recognized ?? 0}`}</p>
    {!!status?.diagnostics?.length && <details><summary>{en ? "Discovery and verification details" : "发现与验证详情"}</summary>
      <ul>{status.diagnostics.slice(-12).map((item,index)=><li key={index}>{new Date(item.at).toLocaleTimeString()} · {item.domain} · {item.adapter} {item.ip} · {stages[item.stage] ?? item.stage}</li>)}</ul>
    </details>}
    <Button disabled={!status?.enabled || saving || resetting} onClick={() => void reset()}>{en ? "Re-evaluate nodes" : "重新评估节点"}</Button>
    {entries.length > 0 && <div style={{ overflowX: "auto", maxHeight: 280 }}>
      <table style={{ width: "100%", textAlign: "left", borderSpacing: "12px 8px" }}>
        <caption>{en ? "Original and verified nodes (observed throughput, not link capacity)" : "原节点与已验证候选（观测吞吐，不代表线路带宽）"}</caption>
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
