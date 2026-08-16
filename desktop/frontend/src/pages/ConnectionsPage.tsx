import {
  Badge,
  Button,
  SearchBox,
  Spinner,
  Switch,
  Toast,
  ToastBody,
  ToastTitle,
  useId,
  useToastController,
} from "@fluentui/react-components";
import {
  AppsListDetail24Regular,
  ArrowDownload20Regular,
  ArrowSync20Regular,
  ArrowUpload20Regular,
  Globe20Regular,
  PlugConnected20Regular,
} from "@fluentui/react-icons";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { GlassSurface } from "../components/material/GlassSurface";
import { AppToaster } from "../components/AppToaster";
import { useI18n } from "../i18n/i18n";
import {
  appServices,
  type ConnectionListSnapshot,
  type ConnectionView,
  withServiceTimeout,
} from "../platform/services";
import { startSerialPoll } from "../platform/serialPoll";

const emptySnapshot: ConnectionListSnapshot = {
  phase: "stopped",
  sampled_at: "",
  connections: [],
};

const formatBytes = (value: number) => {
  if (value >= 1024 * 1024 * 1024) return `${(value / (1024 * 1024 * 1024)).toFixed(1)} GB`;
  if (value >= 1024 * 1024) return `${(value / (1024 * 1024)).toFixed(1)} MB`;
  if (value >= 1024) return `${(value / 1024).toFixed(1)} KB`;
  return `${Math.max(0, value)} B`;
};

const formatDuration = (startedAt: string, now: number) => {
  const started = new Date(startedAt).getTime();
  if (!Number.isFinite(started)) return "—";
  const seconds = Math.max(0, Math.floor((now - started) / 1000));
  const hours = Math.floor(seconds / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  const remaining = seconds % 60;
  return hours > 0
    ? `${hours}:${String(minutes).padStart(2, "0")}:${String(remaining).padStart(2, "0")}`
    : `${minutes}:${String(remaining).padStart(2, "0")}`;
};

export function ConnectionsPage() {
  const { locale } = useI18n();
  const text = useCallback((zh: string, en: string) => locale === "en" ? en : zh, [locale]);
  const [snapshot, setSnapshot] = useState<ConnectionListSnapshot>(emptySnapshot);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [live, setLive] = useState(true);
  const [query, setQuery] = useState("");
  const [now, setNow] = useState(Date.now());
  const requestActive = useRef(false);
  const toasterId = useId("connections-toaster");
  const { dispatchToast } = useToastController(toasterId);

  const load = useCallback(async (manual = false) => {
    if (requestActive.current) return;
    requestActive.current = true;
    if (manual) setRefreshing(true);
    try {
      const next = await withServiceTimeout(
        appServices.engine.connections(),
        8_000,
        text("读取活动连接", "Loading active connections"),
      );
      setSnapshot({ ...next, connections: next.connections ?? [] });
    } catch (error) {
      dispatchToast(
        <Toast>
          <ToastTitle>{text("无法读取活动连接", "Unable to load active connections")}</ToastTitle>
          <ToastBody>{error instanceof Error ? error.message : String(error)}</ToastBody>
        </Toast>,
        { intent: "error", timeout: 4200 },
      );
    } finally {
      requestActive.current = false;
      setLoading(false);
      setRefreshing(false);
    }
  }, [dispatchToast, text]);

  useEffect(() => {
    void load();
  }, [load]);

  useEffect(() => {
    if (!live) return;
    return startSerialPoll(() => load(), 1500);
  }, [live, load]);

  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, []);

  const filtered = useMemo(() => {
    const needle = query.trim().toLocaleLowerCase();
    if (!needle) return snapshot.connections;
    return snapshot.connections.filter((connection) => [
      connection.process,
      connection.domain,
      connection.remote_ip,
      connection.remote_port,
      connection.target,
      connection.adapter,
      connection.outbound,
      connection.protocol,
    ].some((value) => value?.toLocaleLowerCase().includes(needle)));
  }, [query, snapshot.connections]);

  const totals = useMemo(() => snapshot.connections.reduce(
    (current, connection) => ({
      up: current.up + connection.bytes_up,
      down: current.down + connection.bytes_down,
    }),
    { up: 0, down: 0 },
  ), [snapshot.connections]);

  const policy = (connection: ConnectionView) => {
    if (connection.outbound === "direct") {
      return { label: text("直连", "Direct"), color: "success" as const };
    }
    if (connection.outbound === "adapter") {
      return {
        label: text(`指定网卡 · ${connection.outbound_detail || connection.adapter || "—"}`, `NIC · ${connection.outbound_detail || connection.adapter || "—"}`),
        color: "informative" as const,
      };
    }
    return { label: text("多网卡聚合", "Aggregated"), color: "brand" as const };
  };

  const engineRunning = snapshot.phase === "running";

  return (
    <main className="connections-page">
      <AppToaster toasterId={toasterId} position="top-end" />
      <header className="connections-heading">
        <div>
          <span className="section-kicker">{text("当前加速会话的实时网络流", "Live network flows in this acceleration session")}</span>
          <h1>{text("活动连接", "Active connections")}</h1>
          <p>{text(
            "按进程查看目标域名、远端 IP、实际出口网卡、分流策略与会话流量。",
            "Inspect destination domains, remote IPs, actual egress adapters, routing policy, and session traffic by process.",
          )}</p>
        </div>
        <div className="connections-heading-actions">
          <SearchBox
            value={query}
            placeholder={text("搜索进程、域名、IP 或网卡", "Search process, domain, IP, or adapter")}
            onChange={(_, data) => setQuery(data.value)}
          />
          <Switch
            checked={live}
            label={text("实时刷新", "Live")}
            onChange={(_, data) => setLive(data.checked)}
          />
          <Button
            appearance="secondary"
            icon={refreshing ? <Spinner size="tiny" /> : <ArrowSync20Regular />}
            disabled={refreshing}
            onClick={() => void load(true)}
          >
            {text("刷新", "Refresh")}
          </Button>
        </div>
      </header>

      <GlassSurface className="connection-summary" tone="secondary">
        <span>
          <Badge key={engineRunning ? "running" : "stopped"} className="motion-status-swap" appearance="tint" color={engineRunning ? "success" : "informative"}>
            {engineRunning ? text("聚合运行中", "Engine running") : text("聚合未运行", "Engine stopped")}
          </Badge>
        </span>
        <span><PlugConnected20Regular /> <strong>{snapshot.connections.length}</strong> {text("条活动连接", "active")}</span>
        <span><ArrowUpload20Regular /> <strong>{formatBytes(totals.up)}</strong> {text("上传", "uploaded")}</span>
        <span><ArrowDownload20Regular /> <strong>{formatBytes(totals.down)}</strong> {text("下载", "downloaded")}</span>
        <small>{snapshot.sampled_at
          ? text(`采样于 ${new Date(snapshot.sampled_at).toLocaleTimeString("zh-CN", { hour12: false })}`, `Sampled ${new Date(snapshot.sampled_at).toLocaleTimeString("en-US")}`)
          : text("等待 Core 遥测", "Waiting for Core telemetry")}</small>
      </GlassSurface>

      <GlassSurface className={`connections-surface${loading || filtered.length === 0 ? " is-empty" : ""}`}>
        <div className="connection-table-head" role="row">
          <span>{text("进程", "Process")}</span>
          <span>{text("目标", "Destination")}</span>
          <span>{text("出口策略", "Egress policy")}</span>
          <span>{text("流量", "Traffic")}</span>
          <span>{text("时长", "Duration")}</span>
        </div>
        <div className="connection-list">
          {loading ? (
            <div key="connections-loading" className="connections-empty motion-state-content"><Spinner label={text("正在读取实时连接", "Loading live connections")} /></div>
          ) : !engineRunning ? (
            <div key="connections-stopped" className="connections-empty motion-state-content">
              <AppsListDetail24Regular />
              <strong>{text("聚合引擎尚未运行", "The aggregation engine is not running")}</strong>
              <span>{text("启动聚合后，这里会实时显示本次会话正在处理的连接。", "Start aggregation to see the connections handled in this session.")}</span>
            </div>
          ) : filtered.length === 0 ? (
            <div key={query ? "connections-no-match" : "connections-idle"} className="connections-empty motion-state-content">
              <Globe20Regular />
              <strong>{query ? text("没有匹配的连接", "No matching connections") : text("当前没有活动连接", "No active connections")}</strong>
              <span>{text("短连接可能只会短暂出现；实时刷新会保留最新状态。", "Short-lived flows may appear briefly; live refresh keeps the view current.")}</span>
            </div>
          ) : filtered.map((connection) => {
            const outbound = policy(connection);
            const identity = connection.process || connection.domain || connection.remote_ip || connection.target;
            const identitySource = connection.process
              ? `${connection.protocol.toUpperCase()} · ${connection.client || "—"}`
              : connection.domain
                ? text("按目标域名显示", "Shown by destination domain")
                : connection.remote_ip || connection.target
                  ? text("按远端 IP 显示", "Shown by remote IP")
                  : text("未识别连接", "Unidentified connection");
            return (
              <article className="connection-row" key={connection.id}>
                <div className="connection-process">
                  <span className="connection-process-icon"><AppsListDetail24Regular /></span>
                  <span>
                    <strong>{identity || text("未识别连接", "Unidentified connection")}</strong>
                    <small>{identitySource}</small>
                  </span>
                </div>
                <div className="connection-destination">
                  <strong>{connection.domain || connection.remote_ip || connection.target || "—"}</strong>
                  <small>{connection.remote_ip && connection.domain ? `${connection.remote_ip}:${connection.remote_port || ""}` : connection.target || "—"}</small>
                </div>
                <div className="connection-policy">
                  <Badge appearance="tint" color={outbound.color}>{outbound.label}</Badge>
                  <small>{connection.adapter
                    ? text(`实际出口：${connection.adapter}`, `Actual egress: ${connection.adapter}`)
                    : text("出口待建立", "Egress pending")}</small>
                </div>
                <div className="connection-traffic">
                  <span><ArrowUpload20Regular /> {formatBytes(connection.bytes_up)}</span>
                  <span><ArrowDownload20Regular /> {formatBytes(connection.bytes_down)}</span>
                </div>
                <div className="connection-duration">
                  <strong>{formatDuration(connection.started_at, now)}</strong>
                  <small>{new Date(connection.started_at).toLocaleTimeString(locale === "en" ? "en-US" : "zh-CN", { hour12: false })}</small>
                </div>
              </article>
            );
          })}
        </div>
      </GlassSurface>
    </main>
  );
}
