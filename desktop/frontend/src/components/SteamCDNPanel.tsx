import { Badge, Button, Input, Select } from "@fluentui/react-components";
import { useEffect, useRef, useState } from "react";
import { appServices, type SteamCDNStatus } from "../platform/services";
import { useI18n } from "../i18n/i18n";

import { formatSteamBytes, steamGuidance, steamNodeState } from "./steamCDNView";

export function SteamCDNPanel({ enabled, saving }: { enabled: boolean; saving: boolean }) {
  const { locale } = useI18n();
  const en = locale === "en";
  const [status, setStatus] = useState<SteamCDNStatus>();
  const [error, setError] = useState("");
  const [resetting, setResetting] = useState(false);
  const [revision, setRevision] = useState(0);
  const [updatedAt, setUpdatedAt] = useState<Date>();
  const [notice, setNotice] = useState("");
  const [query, setQuery] = useState("");
  const [filter, setFilter] = useState("all");
  const [adapter, setAdapter] = useState("");
  const requestID = useRef(0);
  const resetBusy = useRef(false);
  useEffect(() => {
    if (saving || resetting) return;
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout>;
    const poll = async () => {
      const id = ++requestID.current;
      try {
        const next = await appServices.engine.steamCDNStatus();
        if (!cancelled && id === requestID.current) { setStatus(next); setUpdatedAt(new Date()); setError(""); }
      } catch (reason) { if (!cancelled && id === requestID.current) setError(String(reason)); }
      finally { if (!cancelled) timer = setTimeout(poll, 2500); }
    };
    void poll();
    return () => { cancelled = true; clearTimeout(timer); };
  }, [enabled, saving, resetting, revision]);

  const reset = async () => {
    if (resetBusy.current || saving || !status?.enabled || !!error) return;
    resetBusy.current = true;
    ++requestID.current;
    setNotice("");
    setResetting(true);
    try { setStatus(await appServices.engine.steamCDNStatus(true)); setUpdatedAt(new Date()); setError(""); setNotice(en ? "Learning restarted. New downloads will discover nodes again; existing transfers continue." : "已重新开始学习；新的下载连接将重新发现节点，已有传输继续。"); }
    catch (reason) { setError(String(reason)); }
    finally { resetBusy.current = false; setResetting(false); }
  };
  const entries = status?.entries ?? [];
  const guidance = steamGuidance(status, enabled, en);
  const nodeLabels = en ? { paused: "Route paused", cooldown: "Cooling down", expired: "Validation expired", preferred: "Preferred", trial: "Trial in progress", verified: "Verified candidate", unavailable: "Candidate unavailable", original: "Original node · observation only" } : { paused: "线路暂缓优选", cooldown: "冷却中", expired: "验证已过期", preferred: "优先候选", trial: "试用中", verified: "验证通过的候选", unavailable: "候选暂不可用", original: "原节点 · 仅观察" };
  const adapters = [...new Set(entries.map(e => e.adapter))].sort();
  const visibleEntries = entries.filter(entry => {
    const state = steamNodeState(entry);
    return (!adapter || entry.adapter === adapter)
      && (!query.trim() || [entry.ip, entry.domain, entry.adapter].some(value => value.toLowerCase().includes(query.trim().toLowerCase())))
      && (filter === "all" || (filter === "active" ? (entry.active_connections ?? 0) > 0 : filter === "preferred" ? state === "preferred" : ["paused", "cooldown", "expired", "unavailable"].includes(state)));
  });
  const totalRate = entries.reduce((sum, entry) => sum + (entry.total_bps ?? 0), 0);
  const switchedRate = entries.reduce((sum, entry) => sum + (entry.switched_bps ?? 0), 0);
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
  const decisions: Record<string, string> = en ? {
    trial_active: "Trial in progress; collecting useful samples", content_mismatch: "Content check failed; candidate withdrawn", slow_candidate: "Sustained low throughput; cooling down", group_trial_limit: "Trial budget for this adapter and host is full", load_mismatch: "Waiting for comparable connection load", concurrency_limit: "Trial connection limit reached", route_paused: "Route paused after repeated transfer failures", waiting_trial: "Waiting for a trial connection", insufficient_samples: "Collecting at least 5 samples and 8 MiB", stale_samples: "Waiting for fresh samples", baseline_missing: "Waiting for a fresh original-node baseline", advantage_insufficient: "Advantage has not exceeded 15%", advantage_window: "Advantage observed; awaiting another window", preferred: "Repeated advantage confirmed", validation_expired: "Validation expired; existing transfers continue", cooldown: "Temporarily paused after a failure",
  } : {
    trial_active: "正在试用，等待有效样本", content_mismatch: "内容校验不一致，已撤销候选资格", slow_candidate: "持续低速，暂缓分配新连接", group_trial_limit: "此网卡与域名的试用额度已满", load_mismatch: "等待相近并发负载样本", concurrency_limit: "试用并发额度已满", route_paused: "连续传输失败，暂缓此线路优选", waiting_trial: "等待试用连接", insufficient_samples: "积累至少 5 个样本、8 MiB 数据", stale_samples: "等待新鲜下载样本", baseline_missing: "等待原节点的新鲜基准", advantage_insufficient: "观测优势尚未超过 15%", advantage_window: "已观测优势，等待下一窗口确认", preferred: "连续窗口优势已确认", validation_expired: "验证已过期，已有连接继续传输", cooldown: "失败后暂缓采用",
  };
  stages.http_speed_sampled = en ? "Bounded speed sample completed" : "有限短测完成";
  stages.client_write_closed = en ? "Client stopped receiving; not counted as a node failure" : "客户端接收中断，不计为节点故障";
  stages.http_speed_budget = en ? "Short-test budget exhausted" : "本分钟短测预算已用完";
  stages.http_speed_probe_failed = en ? "Short test unavailable; awaiting real transfer" : "短测未完成，等待实际传输";
  const counts = status?.stage_counts ?? {};
  const eligible = (counts.http_eligible ?? 0) + (counts.http_signed_eligible ?? 0);
  return <div className="steam-cdn-panel">
    <div className="steam-status-banner" data-active={enabled && !!status?.enabled && !error}>
      <span className="steam-status-orb" aria-hidden="true" />
      <div><strong role="status">{saving ? (en ? "Saving settings…" : "正在保存设置…") : !status && !error ? (en ? "Loading optimization status…" : "正在读取优选状态…") : guidance.title}</strong><p>{guidance.body}</p></div>
      <Badge appearance="tint">{error ? (en ? "Stale" : "待刷新") : status?.enabled ? (en ? "Live" : "运行中") : (en ? "Standby" : "待机")}</Badge>
    </div>
    {error && <div className="steam-feedback" role="alert"><strong>{en ? "Status refresh failed" : "状态刷新失败"}</strong><p>{en ? "Displayed values are from the last successful refresh." : "当前保留的是上次成功读取的数据。"} {error}</p><Button disabled={saving || resetting} onClick={() => setRevision(value => value + 1)}>{en ? "Retry" : "重试"}</Button></div>}
    {notice && <p className="steam-feedback" role="status">{notice}</p>}
    <div className="steam-live-summary">
      <div><span>{en ? "Observed download rate · 5 seconds" : "观测下载吞吐 · 最近 5 秒"}</span><strong>{status && !error ? (totalRate / 1048576).toFixed(2) : "—"}<small> MiB/s</small></strong></div>
      <div><span>{en ? "Through switched connections" : "其中优选连接吞吐"}</span><strong>{status?.accounting_version === 1 && !error ? (switchedRate / 1048576).toFixed(2) : "—"}<small> MiB/s</small></strong></div>
      <p>{en ? "Measured forwarding traffic. Steam installation and disk activity are not included in node evaluation." : "以实际转发流量为准，Steam 安装及磁盘活动不作为节点评分。"}</p>
    </div>
    <div className="tool-metrics" aria-label={en ? "Download activity" : "下载概览"}>
      {[
        [en ? "Connections switched" : "已切换连接", status?.replacements ?? 0],
        [en ? "Connections with data" : "已传输连接", status?.effective_replacements ?? 0],
        [en ? "Preferred nodes" : "优先候选", entries.filter(entry => steamNodeState(entry) === "preferred").length],
        [en ? "Connection fallbacks" : "连接回退", status?.fallbacks ?? 0],
      ].map(([label, value]) => <div className="tool-metric" key={label}><span>{label}</span><strong>{value}</strong></div>)}
    </div>
    {status?.accounting_version === 1 && <div className="steam-transfer-summary">
      <span>{en ? "Switched traffic" : "切换流量"}<strong>{formatSteamBytes(status.switched_bytes ?? 0)}</strong></span>
      <span>{en ? "Original traffic" : "原路流量"}<strong>{formatSteamBytes(status.original_bytes ?? 0)}</strong></span>
      <span>{en ? "Transfer failures" : "传输失败"}<strong>{status.transfer_failures ?? 0}</strong></span>
    </div>}
    <div className="tool-activity">
      <span>{en ? `Recognized downloads: ${status?.recognized ?? 0}` : `已识别下载连接：${status?.recognized ?? 0}`}</span>
      <span>{en ? `Eligible chunk requests: ${eligible} · Candidate validations passed: ${counts.verified ?? 0}` : `可评估内容块请求：${eligible} · 候选校验通过：${counts.verified ?? 0}`}</span>
    </div>
    <div className="tool-actions steam-node-heading"><div><h3>{en ? "Download nodes" : "下载节点"} <Badge appearance="tint">{entries.length}</Badge></h3><p>{updatedAt ? (en ? "Updated " : "更新于 ") + updatedAt.toLocaleTimeString() : (en ? "Waiting for status" : "等待状态")}{en ? " · refreshes every 2.5s" : " · 每 2.5 秒刷新"}</p></div><div className="steam-action-buttons"><Button disabled={saving || resetting} onClick={() => setRevision(value => value + 1)}>{en ? "Refresh" : "刷新状态"}</Button><Button disabled={!status?.enabled || saving || resetting || !!error} onClick={() => void reset()}>{resetting ? (en ? "Restarting…" : "正在重新评估…") : (en ? "Re-evaluate nodes" : "重新评估节点")}</Button></div></div>
    <p className="tool-description">{en ? "Re-evaluation clears this session’s node learning and counters. New connections discover candidates again; existing transfers continue." : "重新评估会清空本轮节点学习和统计，新连接重新发现候选；不会中断已有传输。"}</p>
    {entries.length > 0 && <div className="steam-node-filters">
      <Input aria-label={en ? "Search nodes" : "搜索节点"} placeholder={en ? "Search IP, domain or adapter" : "搜索 IP、域名或网卡"} value={query} onChange={(_, data) => setQuery(data.value)} />
      <Select aria-label={en ? "Filter node state" : "筛选节点状态"} value={filter} onChange={event => setFilter(event.target.value)}><option value="all">{en ? "All states" : "全部状态"}</option><option value="active">{en ? "Active" : "正在传输"}</option><option value="preferred">{en ? "Preferred" : "优先候选"}</option><option value="attention">{en ? "Unavailable / paused" : "暂不可用 / 已暂缓"}</option></Select>
      <Select aria-label={en ? "Filter adapter" : "筛选网卡"} value={adapter} onChange={event => setAdapter(event.target.value)}><option value="">{en ? "All adapters" : "全部网卡"}</option>{[...new Set([...adapters, ...(adapter ? [adapter] : [])])].map(value => <option key={value} value={value}>{value}</option>)}</Select>
      <span>{visibleEntries.length} / {entries.length}</span>
    </div>}
    {entries.length === 0 && <div className="steam-empty"><strong>{en ? "No download nodes yet" : "暂无下载节点"}</strong><p>{guidance.body}</p><ol><li>{en ? "Enable optimization" : "开启下载优选"}</li><li>{en ? "Start aggregation on Home" : "在首页启动聚合"}</li><li>{en ? "Start a Steam download through HypoMux" : "让 Steam 下载流量经过 HypoMux"}</li></ol></div>}
    {entries.length > 0 && visibleEntries.length === 0 && <div className="steam-empty"><strong>{en ? "No matching nodes" : "没有匹配的节点"}</strong><Button onClick={() => { setQuery(""); setFilter("all"); setAdapter(""); }}>{en ? "Clear filters" : "清除筛选"}</Button></div>}
    {visibleEntries.length > 0 && <div className="tool-node-table" tabIndex={0} role="region" aria-label={en ? "Download nodes" : "下载节点"}>
      <table>
        <caption>{en ? "Original and verified nodes (observed throughput, not link capacity)" : "原节点与已验证候选（观测吞吐，不代表线路带宽）"}</caption>
        <thead><tr>{(en ? ["Node", "Adapter", "Switched rate (5s)", "Total rate (5s)", "Connections", "State"] : ["节点", "网卡", "切换吞吐（5秒）", "总吞吐（5秒）", "连接", "状态"]).map(label => <th scope="col" key={label}>{label}</th>)}</tr></thead>
        <tbody>{visibleEntries.map(entry => <tr key={`${entry.adapter}/${entry.domain}/${entry.port}/${entry.ip}`}>
          <td className="steam-node-identity"><strong>{entry.ip}</strong><span>{entry.domain}:{entry.port}</span><small>{entry.ip.includes(":") ? "IPv6" : "IPv4"} · {entry.source === "session" ? (en ? "Session" : "会话观察") : entry.source === "dns_and_session" ? (en ? "DNS + session" : "DNS + 会话") : entry.source === "dns" ? "DNS" : (en ? "Original" : "原路")}</small></td>
          <td>{entry.adapter}</td>
          <td>{status?.accounting_version === 1 ? `${((entry.switched_bps ?? 0) / 1048576).toFixed(2)} MiB/s` : "—"}</td>
          <td>{entry.active_connections ? `${((entry.total_bps ?? 0) / 1024 / 1024).toFixed(2)} MiB/s` : (en ? "Idle" : "空闲")}</td>
          <td>{entry.active_connections ?? "—"}</td>
          <td className="steam-node-state"><strong className="steam-state-label" data-state={steamNodeState(entry)}>{nodeLabels[steamNodeState(entry)]}</strong>
          <span>{decisions[entry.decision_reason ?? ""] ?? ""}</span>{entry.admission_reason && <div>{decisions[entry.admission_reason] ?? "—"}</div>}{entry.evaluated_at && Date.parse(entry.evaluated_at) > 0 && <div>{new Date(entry.evaluated_at).toLocaleTimeString()}</div>}
          <details className="steam-node-evidence"><summary>{en ? "Samples and traffic" : "样本与流量"}</summary><dl>
            <dt>{en ? "Useful samples" : "有效样本"}</dt><dd>{entry.samples ?? 0}</dd>
            <dt>{en ? "Useful sample bytes" : "有效样本流量"}</dt><dd>{formatSteamBytes(entry.effective_bytes ?? 0)}</dd>
            <dt>{en ? "Switched traffic" : "切换流量"}</dt><dd>{status?.accounting_version === 1 ? formatSteamBytes(entry.switched_bytes ?? 0) : "—"}</dd>
            <dt>{en ? "Original traffic" : "原路流量"}</dt><dd>{status?.accounting_version === 1 ? formatSteamBytes(entry.original_bytes ?? 0) : "—"}</dd>
            <dt>{en ? "Transfer failures" : "传输失败"}</dt><dd>{status?.accounting_version === 1 ? entry.transfer_failures ?? 0 : "—"}</dd>
          </dl></details></td>
        </tr>)}</tbody>
      </table>
    </div>}
    {<details className="tool-diagnostics"><summary>{en ? "Discovery and verification details" : "发现与验证详情"}</summary>
    <div className="steam-core-info"><span>Core {status?.core_version || "—"}{status?.core_commit ? " · " + status.core_commit.slice(0, 12) : ""}</span><span>{en ? "Mode" : "模式"}: {status?.configured_mode || "—"}</span></div>
    <p className="tool-description">{en ? "No fixed country or public DNS override. Single-address hosts, LAN caches and unsupported authentication keep their original route; encrypted downloads are not decrypted." : "不固定国家节点，不覆盖你的 DNS。仅有一个地址、局域网缓存或不支持的鉴权继续沿用原路；不解密加密下载。"}</p>
    <p className="tool-description">{en ? "Uses DNS candidates for the same domain and observed download rates. Initial HTTP verification reads up to a 4 KiB prefix. Improvement depends on available CDN nodes; disable if performance worsens." : "使用同域名 DNS 候选及真实下载观测速率，HTTP 初步校验读取最多 4 KiB 前缀。效果取决于可用节点，效果不好可关闭。"}</p>
      {(status?.speed_probe_limit ?? 0) > 0 && <p>{en ? `Short tests: ${((status?.speed_probe_bytes ?? 0) / 1048576).toFixed(2)} / ${((status?.speed_probe_limit ?? 0) / 1048576).toFixed(0)} MiB reserved this minute. Up to 256 KiB per request; results only order trials.` : `短测：本分钟已预留 ${((status?.speed_probe_bytes ?? 0) / 1048576).toFixed(2)} / ${((status?.speed_probe_limit ?? 0) / 1048576).toFixed(0)} MiB。单次最多 256 KiB，仅用于安排试用顺序。`}</p>}
      {entries.some(entry => (entry.probe_bps ?? 0) > 0) && <ul aria-label={en ? "Short-test references" : "短测参考"}>{entries.filter(entry => (entry.probe_bps ?? 0) > 0).map(entry => <li key={`${entry.adapter}/${entry.domain}/${entry.port}/${entry.ip}`}>{entry.adapter} · {entry.ip} · {((entry.probe_bps ?? 0) / 1048576).toFixed(2)} MiB/s · {en ? "short-test reference, not actual download rate" : "短测参考，非实际下载速度"}{(!entry.probed_at || Date.now() - Date.parse(entry.probed_at) >= 60000 || !entry.validated || Date.parse(entry.expires_at) <= Date.now()) ? (en ? " · expired" : " · 已过期") : ""}</li>)}</ul>}
      <p>{en ? "Forwarded bytes include protocol overhead and are not a measured speedup." : "转发字节包含协议开销，不代表净提速收益。"}</p>
      {!Object.keys(counts).length && !(status?.diagnostics?.length) && <p>{en ? "No discovery or validation events in this session." : "本轮尚无发现或验证记录。"}</p>}
      <p>{Object.entries(counts).map(([key,value])=>`${stages[key] ?? key}: ${value}`).join(" · ")}</p>
      <ul>{(status?.diagnostics ?? []).slice(-12).map((item,index)=><li key={index}>{new Date(item.at).toLocaleTimeString()} · {item.domain} · {item.adapter} {item.ip} · {stages[item.stage] ?? item.stage}</li>)}</ul>
    </details>}
  </div>;
}
