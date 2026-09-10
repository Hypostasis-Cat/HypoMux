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
  Object.assign(stages, en ? {
 http_redirect_ip:"Direct IP redirect; Steam keeps the original route (no same-host DNS candidates)",
 http_reference_ready:"Original response sample ready",
 http_response_status:"Original response not 200; observation only", http_response_encoding:"Compressed original response; observation only", http_response_chunked:"Chunked original response; observation only", http_response_length:"No known positive chunk length", http_response_stale:"Reference request expired",
 http_method:"Waiting for a chunk GET after metadata/authentication", http_request_body:"Request with a body; original routing", http_credentials:"Cookie or Authorization; original routing", http_path:"Not a supported chunk path", http_client_range:"Client range request; observation only", http_query:"Unsupported query profile", http_signature_expired:"Signature expired", http_signed_eligible:"Signed chunk eligible on its original adapter", http_eligible:"Public chunk eligible", http_signature_rejected:"Content server rejected authentication", http_redirect_observed:"Known redirect; Steam follows normally", http_redirect_unsupported:"Unsupported redirect target", http_observer_unsupported:"HTTP framing unsupported or observation limit reached; forwarding continues", http_header_timeout:"Incomplete HTTP header; forwarding continues", http_budget:"Probe budget exhausted", http_range_unsupported:"Server does not support sample ranges", only_original:"DNS returned only the original node", client_backpressure:"Client receiving slowly; sample excluded from ranking",
 } : {
 http_redirect_ip:"直接 IP 重定向，交由 Steam 原路下载（没有同域名 DNS 候选）",
 http_reference_ready:"原连接响应样本已就绪",
 http_response_status:"原响应不是 200，仅观察", http_response_encoding:"原响应使用压缩编码，仅观察", http_response_chunked:"原响应为 chunked 编码，仅观察", http_response_length:"内容块长度未知或为空", http_response_stale:"参考请求已过期",
 http_method:"等待鉴权/元数据之后的内容块 GET", http_request_body:"请求有正文，保留原路", http_credentials:"携带 Cookie 或 Authorization，保留原路", http_path:"不是支持的内容块路径", http_client_range:"客户端范围请求，仅观察", http_query:"查询参数组合暂不支持", http_signature_expired:"签名已过期", http_signed_eligible:"签名内容块可在原网卡评估", http_eligible:"公开内容块可评估", http_signature_rejected:"内容服务器拒绝鉴权", http_redirect_observed:"已识别重定向，交由 Steam 正常跳转", http_redirect_unsupported:"重定向目标暂不支持", http_observer_unsupported:"HTTP 格式不支持或观察超限，继续正常转发", http_header_timeout:"HTTP 请求头不完整，继续正常转发", http_budget:"本分钟探测预算已用完", http_range_unsupported:"服务器不支持小样本范围校验", only_original:"DNS 仅返回原节点", client_backpressure:"客户端接收较慢，该样本不参与排名",
 });
  const counts = status?.stage_counts ?? {};
  const eligible = (counts.http_eligible ?? 0) + (counts.http_signed_eligible ?? 0);
  return <div className="steam-cdn-panel">
    <p role="status">{error || (!status ? (en ? "Loading optimization status…" : "正在读取优选状态…")
      : !status.available ? (en ? "Start a compatible Core to view optimization status." : "启动支持此功能的 Core 后可查看优选状态。")
      : !status.enabled ? (en ? "Optimization is inactive. If enabled above, it will start with the engine." : "优选当前未运行；若已开启开关，将随引擎启动。")
      : status.probing > 0 ? (en ? "Validating download nodes…" : "正在验证下载节点…")
      : (status.recognized ?? 0) > 0 && status.replacements === 0 ? (en ? "Steam traffic recognized; using original nodes while candidates are evaluated. See details below." : "已识别 Steam 流量，当前仍使用原节点；候选验证结果见下方详情。")
      : entries.length === 0 ? (en ? "Waiting for Steam downloads. No verified candidates yet; using the original connection." : "等待 Steam 下载；暂无验证通过的候选，沿用原连接。")
      : (en ? `${status.replacements} connections switched · ${status.effective_replacements ?? 0} transferred data · ${status.fallbacks} connection fallbacks` : `已切换 ${status.replacements} 条连接 · ${status.effective_replacements ?? 0} 条已传输数据 · 连接回退 ${status.fallbacks} 次`))}</p>
    <p className="tool-description">{en ? "No fixed country or public DNS override. Single-address hosts, LAN caches and unsupported authentication keep their original route; encrypted downloads are not decrypted." : "不固定国家节点，不覆盖你的 DNS。仅有一个地址、局域网缓存或不支持的鉴权继续沿用原路；不解密加密下载。"}</p>
    <p className="tool-description">{en ? "Uses DNS candidates for the same domain and observed download rates. HTTP verification reads bounded 4 KiB samples. Improvement depends on available CDN nodes; disable if performance worsens." : "使用同域名 DNS 候选及真实下载观测速率，HTTP 校验只读取受限的 4 KiB 样本。效果取决于可用节点，效果不好可关闭。"}</p>
    <div className="tool-metrics" aria-label={en ? "Download activity" : "下载概览"}>
      {[
        [en ? "Connections switched" : "已切换连接", status?.replacements ?? 0],
        [en ? "Transferred data" : "已传输数据", status?.effective_replacements ?? 0],
        [en ? "Preferred nodes" : "优先候选", entries.filter(entry => entry.preferred).length],
        [en ? "Connection fallbacks" : "连接回退", status?.fallbacks ?? 0],
      ].map(([label, value]) => <div className="tool-metric" key={label}><span>{label}</span><strong>{value}</strong></div>)}
    </div>
    <div className="tool-activity">
      <span>{en ? `Recognized downloads: ${status?.recognized ?? 0}` : `已识别下载连接：${status?.recognized ?? 0}`}</span>
      <span>{en ? `Eligible chunk requests: ${eligible} · Candidate validations passed: ${counts.verified ?? 0}` : `可评估内容块请求：${eligible} · 候选校验通过：${counts.verified ?? 0}`}</span>
    </div>
    <div className="tool-actions"><Button disabled={!status?.enabled || saving || resetting} onClick={() => void reset()}>{en ? "Re-evaluate nodes" : "重新评估节点"}</Button></div>
    {entries.length > 0 && <div className="tool-node-table" tabIndex={0} role="region" aria-label={en ? "Download nodes" : "下载节点"}>
      <table>
        <caption>{en ? "Original and verified nodes (observed throughput, not link capacity)" : "原节点与已验证候选（观测吞吐，不代表线路带宽）"}</caption>
        <thead><tr>{(en ? ["Adapter", "Domain", "IP", "Network", "Total rate (5s)", "Active connections", "State"] : ["网卡", "域名", "IP", "网络", "节点总吞吐（5秒）", "活跃连接", "状态"]).map(label => <th scope="col" key={label}>{label}</th>)}</tr></thead>
        <tbody>{entries.map(entry => <tr key={`${entry.adapter}/${entry.domain}/${entry.port}/${entry.ip}`}>
          <td>{entry.adapter}</td><td>{entry.domain}:{entry.port}</td><td>{entry.ip}</td><td>{entry.ip.includes(":") ? "IPv6" : "IPv4"}</td>
          <td>{entry.active_connections ? `${((entry.total_bps ?? 0) / 1024 / 1024).toFixed(2)} MiB/s` : (en ? "Idle" : "空闲")}</td><td>{entry.active_connections ?? "—"}</td>
          <td>{Date.parse(entry.cooldown_until) > Date.now() ? (en ? "Cooling down" : "冷却中") : entry.preferred ? (en ? "Preferred" : "优先候选") : entry.validated ? (en ? "Verified candidate" : "验证通过的候选") : (en ? "Original node · observation only" : "原节点 · 仅观察")}</td>
        </tr>)}</tbody>
      </table>
    </div>}
    {!!status?.diagnostics?.length && <details className="tool-diagnostics"><summary>{en ? "Discovery and verification details" : "发现与验证详情"}</summary>
      <p>{Object.entries(counts).map(([key,value])=>`${stages[key] ?? key}: ${value}`).join(" · ")}</p>
      <ul>{status.diagnostics.slice(-12).map((item,index)=><li key={index}>{new Date(item.at).toLocaleTimeString()} · {item.domain} · {item.adapter} {item.ip} · {stages[item.stage] ?? item.stage}</li>)}</ul>
    </details>}
  </div>;
}
