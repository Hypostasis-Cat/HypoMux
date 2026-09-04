import {
  Badge,
  Button,
  Dialog,
  DialogActions,
  DialogBody,
  DialogContent,
  DialogSurface,
  DialogTitle,
  Dropdown,
  Menu,
  MenuGroup,
  MenuGroupHeader,
  MenuItem,
  MenuList,
  MenuPopover,
  MessageBar,
  MessageBarBody,
  Option,
  SearchBox,
  Spinner,
  Switch,
} from "@fluentui/react-components";
import {
  AppsListDetail24Regular,
  Add20Regular,
  ArrowDownload20Regular,
  ArrowSync20Regular,
  ArrowUpload20Regular,
  Dismiss16Regular,
  Filter20Regular,
  Globe20Regular,
  PlugConnected20Regular,
} from "@fluentui/react-icons";
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { GlassSurface } from "../components/material/GlassSurface";
import { useAppNotifications } from "../components/notifications/AppNotifications";
import { useI18n } from "../i18n/i18n";
import {
  appServices,
  type ConnectionListSnapshot,
  type ConnectionView,
  type RoutingBatchPreview,
  type RoutingRule,
  type RoutingSnapshot,
  withServiceTimeout,
} from "../platform/services";
import { startSerialPoll } from "../platform/serialPoll";
import type { HomeAdapter } from "../state/useEngineState";
import { groupConnectionsByAdapter } from "./connectionGroups";
import {
  selectConnections,
  type ConnectionOutboundFilter,
} from "./connectionView";
import {
  type ConnectionSort,
  type ConnectionSortKey,
} from "./connectionSort";
import { routingRuleIdentity } from "./routingBatch";

type QuickRuleMatchType = "process" | "domain" | "ip";

type QuickRuleCandidate = {
  matchType: QuickRuleMatchType;
  value: string;
};

type ConnectionContextMenu = {
  connection: ConnectionView;
  x: number;
  y: number;
};

type QuickRuleSelection = QuickRuleCandidate & {
  connection: ConnectionView;
};

export const connectionRuleCandidates = (connection: ConnectionView): QuickRuleCandidate[] => {
  const candidates: QuickRuleCandidate[] = [
    { matchType: "process", value: connection.process?.trim() ?? "" },
    { matchType: "domain", value: connection.domain?.trim() ?? "" },
    { matchType: "ip", value: connection.remote_ip?.trim() ?? "" },
  ];
  return candidates.filter((candidate) => candidate.value.length > 0);
};

export const preferredConnectionRuleOutbound = (
  connection: ConnectionView,
  outbounds: RoutingSnapshot["outbounds"],
) => {
  const available = outbounds ?? [];
  if (available.some((outbound) => outbound.id === connection.outbound)) return connection.outbound;
  if (connection.outbound === "adapter") {
    const adapterNames = [connection.outbound_detail, connection.adapter]
      .map((value) => value?.trim().toLocaleLowerCase())
      .filter(Boolean);
    const adapterOutbound = available.find((outbound) => {
      const label = outbound.label.trim().toLocaleLowerCase();
      const id = outbound.id.replace(/^nic_/, "").trim().toLocaleLowerCase();
      return adapterNames.includes(label) || adapterNames.includes(id);
    });
    if (adapterOutbound) return adapterOutbound.id;
  }
  return available.some((outbound) => outbound.id === "aggregation")
    ? "aggregation"
    : available[0]?.id ?? "";
};

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

export function ConnectionsPage({
  initialAdapter = "",
  adapterRevision = 0,
  adapterRuntime = [],
}: {
  initialAdapter?: string;
  adapterRevision?: number;
  adapterRuntime?: readonly HomeAdapter[];
}) {
  const { locale } = useI18n();
  const text = useCallback((zh: string, en: string) => locale === "en" ? en : zh, [locale]);
  const [snapshot, setSnapshot] = useState<ConnectionListSnapshot>(emptySnapshot);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [live, setLive] = useState(true);
  const [query, setQuery] = useState("");
  const [outboundFilter, setOutboundFilter] = useState<ConnectionOutboundFilter>("all");
  const [adapterFilter, setAdapterFilter] = useState(initialAdapter.trim());
  const [sort, setSort] = useState<ConnectionSort>({ key: "duration", direction: "descending" });
  const [now, setNow] = useState(Date.now());
  const [contextMenu, setContextMenu] = useState<ConnectionContextMenu | null>(null);
  const [quickRule, setQuickRule] = useState<QuickRuleSelection | null>(null);
  // Retain the content during Fluent's exit animation to avoid a collapsing dialog.
  const [quickRuleOpen, setQuickRuleOpen] = useState(false);
  const [quickRuleSnapshot, setQuickRuleSnapshot] = useState<RoutingSnapshot>({ rules: [], outbounds: [] });
  const [quickRuleOutbound, setQuickRuleOutbound] = useState("");
  const [quickRulePreview, setQuickRulePreview] = useState<RoutingBatchPreview | null>(null);
  const [quickRuleLoading, setQuickRuleLoading] = useState(false);
  const [quickRuleSaving, setQuickRuleSaving] = useState(false);
  const requestActive = useRef(false);
  const connectionListRef = useRef<HTMLDivElement>(null);
  const contextMenuTargetRef = useRef<HTMLSpanElement>(null);
  const quickRuleRequest = useRef(0);
  const pendingScrollTop = useRef<number | null>(null);
  const { notify } = useAppNotifications();
  const groupedByAdapter = adapterFilter.length > 0;

  useEffect(() => {
    setAdapterFilter(initialAdapter.trim());
    setQuery("");
    setOutboundFilter("all");
  }, [adapterRevision, initialAdapter]);

  const textRef = useRef(text);
  textRef.current = text;

  const load = useCallback(async (manual = false) => {
    if (requestActive.current) return;
    requestActive.current = true;
    if (manual) setRefreshing(true);
    try {
      const next = await withServiceTimeout(
        appServices.engine.connections(),
        8_000,
        textRef.current("读取活动连接", "Loading active connections"),
      );
      pendingScrollTop.current = connectionListRef.current?.scrollTop ?? null;
      setSnapshot({ ...next, connections: next.connections ?? [] });
    } catch (error) {
      notify({
        title: textRef.current("无法读取活动连接", "Unable to load active connections"),
        message: error instanceof Error ? error.message : String(error),
        intent: "error",
        dedupeKey: "connections:load-error",
      });
    } finally {
      requestActive.current = false;
      setLoading(false);
      setRefreshing(false);
    }
  }, [notify]);

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

  useLayoutEffect(() => {
    const list = connectionListRef.current;
    const savedScrollTop = pendingScrollTop.current;
    if (!list || savedScrollTop === null) return;
    list.scrollTop = Math.min(savedScrollTop, Math.max(0, list.scrollHeight - list.clientHeight));
    pendingScrollTop.current = null;
  }, [snapshot]);

  const filtered = useMemo(
    () => selectConnections(snapshot.connections, query, outboundFilter, adapterFilter, sort),
    [adapterFilter, outboundFilter, query, snapshot.connections, sort],
  );

  const totals = useMemo(() => snapshot.connections.reduce(
    (current, connection) => ({
      up: current.up + connection.bytes_up,
      down: current.down + connection.bytes_down,
    }),
    { up: 0, down: 0 },
  ), [snapshot.connections]);

  const hasSingleAdapterRouting = useMemo(
    () => snapshot.connections.some((connection) => connection.outbound === "adapter"),
    [snapshot.connections],
  );

  useEffect(() => {
    if (!loading && outboundFilter === "adapter" && !hasSingleAdapterRouting) {
      setOutboundFilter("all");
    }
  }, [hasSingleAdapterRouting, loading, outboundFilter]);

  const groups = useMemo(
    () => groupedByAdapter ? groupConnectionsByAdapter(filtered, adapterRuntime) : [],
    [adapterRuntime, filtered, groupedByAdapter],
  );

  const policy = (connection: ConnectionView) => {
    if (connection.outbound === "direct") {
      return { label: text("直连", "Direct"), color: "success" as const };
    }
    if (connection.outbound === "adapter") {
      return {
        label: text(`单网卡分流 · ${connection.outbound_detail || connection.adapter || "—"}`, `Single-NIC route · ${connection.outbound_detail || connection.adapter || "—"}`),
        color: "informative" as const,
      };
    }
    return { label: text("多网卡聚合", "Aggregated"), color: "brand" as const };
  };

  const matchTypeLabel = useCallback((matchType: QuickRuleMatchType) => ({
    process: text("进程", "Process"),
    domain: text("域名", "Domain"),
    ip: "IP",
  })[matchType], [text]);

  const outboundLabel = useCallback((id: string, outbounds = quickRuleSnapshot.outbounds ?? []) => {
    if (id === "aggregation") return text("多网卡聚合", "Aggregated");
    if (id === "direct") return text("直连 / 绕过", "Direct / bypass");
    return outbounds.find((outbound) => outbound.id === id)?.label ?? id.replace(/^nic_/, "");
  }, [quickRuleSnapshot.outbounds, text]);

  const loadQuickRulePreview = useCallback(async (
    selection: QuickRuleSelection,
    outbound: string,
    rules: RoutingRule[],
    request: number,
  ) => {
    try {
      const preview = await appServices.routing.previewBatch(
        selection.matchType,
        [selection.value],
        outbound,
        rules,
      );
      if (quickRuleRequest.current === request) {
        setQuickRulePreview({ ...preview, items: preview.items ?? [] });
      }
    } catch (error) {
      if (quickRuleRequest.current !== request) return;
      setQuickRulePreview(null);
      notify({
        title: text("无法检查分流规则", "Unable to check routing rule"),
        message: error instanceof Error ? error.message : String(error),
        intent: "error",
        dedupeKey: "connections:quick-rule-preview",
      });
    }
  }, [notify, text]);

  const openQuickRule = useCallback(async (candidate: QuickRuleCandidate, connection: ConnectionView) => {
    const selection = { ...candidate, connection };
    const request = quickRuleRequest.current + 1;
    quickRuleRequest.current = request;
    setContextMenu(null);
    setQuickRule(selection);
    setQuickRuleOpen(true);
    setQuickRuleLoading(true);
    setQuickRulePreview(null);
    try {
      const routingSnapshot = await appServices.routing.snapshot();
      if (quickRuleRequest.current !== request) return;
      const normalizedSnapshot = {
        ...routingSnapshot,
        rules: routingSnapshot.rules ?? [],
        outbounds: routingSnapshot.outbounds ?? [],
      };
      const outbound = preferredConnectionRuleOutbound(connection, normalizedSnapshot.outbounds);
      setQuickRuleSnapshot(normalizedSnapshot);
      setQuickRuleOutbound(outbound);
      if (outbound) {
        await loadQuickRulePreview(selection, outbound, normalizedSnapshot.rules, request);
      }
    } catch (error) {
      if (quickRuleRequest.current !== request) return;
      notify({
        title: text("无法读取分流规则", "Unable to load routing rules"),
        message: error instanceof Error ? error.message : String(error),
        intent: "error",
        dedupeKey: "connections:quick-rule-load",
      });
      setQuickRuleOpen(false);
    } finally {
      if (quickRuleRequest.current === request) setQuickRuleLoading(false);
    }
  }, [loadQuickRulePreview, notify, text]);

  const changeQuickRuleOutbound = useCallback((outbound: string) => {
    if (!quickRule) return;
    const request = quickRuleRequest.current + 1;
    quickRuleRequest.current = request;
    setQuickRuleOutbound(outbound);
    setQuickRulePreview(null);
    setQuickRuleLoading(true);
    void loadQuickRulePreview(
      quickRule,
      outbound,
      quickRuleSnapshot.rules ?? [],
      request,
    ).finally(() => {
      if (quickRuleRequest.current === request) setQuickRuleLoading(false);
    });
  }, [loadQuickRulePreview, quickRule, quickRuleSnapshot.rules]);

  const saveQuickRule = useCallback(async () => {
    if (!quickRule || !quickRuleOutbound || quickRuleSaving) return;
    setQuickRuleSaving(true);
    try {
      // Read immediately before saving so a quick add never replaces a newer
      // copy of the user's rule list with a stale snapshot.
      const latest = await appServices.routing.snapshot();
      const latestRules = latest.rules ?? [];
      const preview = await appServices.routing.previewBatch(
        quickRule.matchType,
        [quickRule.value],
        quickRuleOutbound,
        latestRules,
      );
      const item = preview.items?.[0];
      if (!item || item.status === "invalid") {
        setQuickRulePreview({ ...preview, items: preview.items ?? [] });
        throw new Error(item?.message || text("规则内容无效", "The rule value is invalid"));
      }
      if (item.status === "duplicate") {
        setQuickRuleOpen(false);
        notify({
          title: text("规则已经存在", "Rule already exists"),
          message: text("没有修改现有规则。", "Existing rules were left unchanged."),
          intent: "info",
          dedupeKey: "connections:quick-rule-duplicate",
        });
        return;
      }
      const identity = routingRuleIdentity(item.rule.match_type, item.rule.value);
      const nextRules = item.status === "conflict"
        ? latestRules.filter((rule) => routingRuleIdentity(rule.match_type, rule.value) !== identity)
        : [...latestRules];
      nextRules.push(item.rule);
      await appServices.routing.save(nextRules);
      setQuickRuleOpen(false);
      notify({
        title: item.status === "conflict"
          ? text("分流规则已更新", "Routing rule updated")
          : text("分流规则已添加", "Routing rule added"),
        message: text(
          `${matchTypeLabel(quickRule.matchType)} ${item.rule.value} 已指向${outboundLabel(quickRuleOutbound, latest.outbounds ?? [])}；新连接立即生效。`,
          `${matchTypeLabel(quickRule.matchType)} ${item.rule.value} now uses ${outboundLabel(quickRuleOutbound, latest.outbounds ?? [])}; new connections take effect immediately.`,
        ),
        intent: "success",
        dedupeKey: `connections:quick-rule-saved:${identity}`,
      });
    } catch (error) {
      notify({
        title: text("无法保存分流规则", "Unable to save routing rule"),
        message: error instanceof Error ? error.message : String(error),
        intent: "error",
        dedupeKey: "connections:quick-rule-save",
      });
    } finally {
      setQuickRuleSaving(false);
    }
  }, [matchTypeLabel, notify, outboundLabel, quickRule, quickRuleOutbound, quickRuleSaving, text]);

  const engineRunning = snapshot.phase === "running";
  const hasViewFilter = query.trim().length > 0 || outboundFilter !== "all" || groupedByAdapter;
  const columns: Array<{
    key: ConnectionSortKey;
    label: string;
    defaultDirection: ConnectionSort["direction"];
  }> = [
    { key: "process", label: text("进程", "Process"), defaultDirection: "ascending" },
    { key: "destination", label: text("目标", "Destination"), defaultDirection: "ascending" },
    { key: "policy", label: text("出口策略", "Egress policy"), defaultDirection: "ascending" },
    { key: "traffic", label: text("流量", "Traffic"), defaultDirection: "descending" },
    { key: "duration", label: text("时长", "Duration"), defaultDirection: "descending" },
  ];
  const activeSortColumn = columns.find((column) => column.key === sort.key) ?? columns[4];
  const sortSummary = text(
    `${activeSortColumn.label} · ${sort.direction === "ascending" ? "升序" : "降序"}`,
    `${activeSortColumn.label} · ${sort.direction === "ascending" ? "Ascending" : "Descending"}`,
  );

  const renderConnection = (connection: ConnectionView) => {
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
      <article
        className={`connection-row${contextMenu?.connection.id === connection.id ? " is-context-active" : ""}`}
        key={connection.id}
        tabIndex={0}
        aria-label={text(`连接 ${identity || "未识别"}`, `Connection ${identity || "unidentified"}`)}
        title={text("右键可快速添加分流规则", "Right-click to quickly add a routing rule")}
        onContextMenu={(event) => {
          event.preventDefault();
          setContextMenu({ connection, x: event.clientX, y: event.clientY });
        }}
        onKeyDown={(event) => {
          if (event.key !== "ContextMenu" && !(event.shiftKey && event.key === "F10")) return;
          event.preventDefault();
          const bounds = event.currentTarget.getBoundingClientRect();
          setContextMenu({ connection, x: bounds.left + 42, y: bounds.top + 42 });
        }}
      >
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
  };

  return (
    <main className="connections-page">
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
        <div className="connection-view-toolbar">
          <span className="connection-view-result">
            <Filter20Regular />
            <span>{text(`显示 ${filtered.length} / ${snapshot.connections.length}`, `Showing ${filtered.length} of ${snapshot.connections.length}`)}</span>
            <span className="connection-sort-summary" role="status" aria-live="polite">
              {sortSummary}
              <span aria-hidden="true">{sort.direction === "ascending" ? "↑" : "↓"}</span>
            </span>
          </span>
          <div className="connection-view-controls">
            {groupedByAdapter && (
              <div
                className="connection-adapter-filter"
                role="group"
                aria-label={text("适配器筛选", "Adapter filter")}
              >
                <span>{text("适配器", "Adapter")}</span>
                <Badge appearance="tint" color="brand" title={adapterFilter}>{adapterFilter}</Badge>
                <Button
                  appearance="subtle"
                  size="small"
                  icon={<Dismiss16Regular />}
                  aria-label={text(`清除适配器筛选 ${adapterFilter}`, `Clear adapter filter ${adapterFilter}`)}
                  onClick={() => setAdapterFilter("")}
                >
                  {text("清除", "Clear")}
                </Button>
              </div>
            )}
            <label>
              <span>{text("出口策略", "Egress")}</span>
              <Dropdown
                appearance="filled-darker"
                size="small"
                positioning={{ position: "below", align: "end", pinned: true, strategy: "fixed", offset: 4 }}
                aria-label={text("按出口策略筛选", "Filter by egress policy")}
                value={{
                  all: text("全部出口", "All egress"),
                  aggregation: text("多网卡聚合", "Aggregated"),
                  direct: text("直连", "Direct"),
                  adapter: text("单网卡分流", "Single-NIC routing"),
                }[outboundFilter]}
                selectedOptions={[outboundFilter]}
                onOptionSelect={(_, data) => data.optionValue && setOutboundFilter(data.optionValue as ConnectionOutboundFilter)}
              >
                <Option value="all">{text("全部出口", "All egress")}</Option>
                <Option value="aggregation">{text("多网卡聚合", "Aggregated")}</Option>
                <Option value="direct">{text("直连", "Direct")}</Option>
                {hasSingleAdapterRouting && (
                  <Option value="adapter">{text("单网卡分流", "Single-NIC routing")}</Option>
                )}
              </Dropdown>
            </label>
          </div>
        </div>
        <div className="connection-table-head" role="row">
          {columns.map((column) => {
            const direction = sort.key === column.key ? sort.direction : undefined;
            const nextDirection = direction
              ? direction === "ascending" ? "descending" : "ascending"
              : column.defaultDirection;
            const sortAction = text(
              `按${column.label}${nextDirection === "ascending" ? "升序" : "降序"}排序`,
              `Sort ${column.label} ${nextDirection}`,
            );
            return (
              <span key={column.key} role="columnheader" aria-sort={direction ?? "none"}>
                <button
                  type="button"
                  className={`connection-sort-button${direction ? " is-active" : ""}`}
                  aria-label={sortAction}
                  title={sortAction}
                  onClick={() => setSort({ key: column.key, direction: nextDirection })}
                >
                  <span>{column.label}</span>
                  <span
                    className={`connection-sort-indicator${direction ? ` is-${direction}` : " is-idle"}`}
                    aria-hidden="true"
                  >
                    {direction ? "↑" : "↕"}
                  </span>
                </button>
              </span>
            );
          })}
        </div>
        <div ref={connectionListRef} className="connection-list">
          {loading ? (
            <div key="connections-loading" className="connections-empty motion-state-content"><Spinner label={text("正在读取实时连接", "Loading live connections")} /></div>
          ) : !engineRunning ? (
            <div key="connections-stopped" className="connections-empty motion-state-content">
              <AppsListDetail24Regular />
              <strong>{text("聚合引擎尚未运行", "The aggregation engine is not running")}</strong>
              <span>{text("启动聚合后，这里会实时显示本次会话正在处理的连接。", "Start aggregation to see the connections handled in this session.")}</span>
            </div>
          ) : filtered.length === 0 ? (
            <div key={hasViewFilter ? "connections-no-match" : "connections-idle"} className="connections-empty motion-state-content">
              <Globe20Regular />
              <strong>{hasViewFilter ? text("没有符合当前筛选的连接", "No connections match the current filters") : text("当前没有活动连接", "No active connections")}</strong>
              <span>{hasViewFilter
                ? text("可以更换出口策略或清空搜索内容。", "Choose another egress policy or clear the search query.")
                : text("短连接可能只会短暂出现；实时刷新会保留最新状态。", "Short-lived flows may appear briefly; live refresh keeps the view current.")}</span>
            </div>
          ) : groupedByAdapter ? groups.map((group) => (
            <section className="connection-adapter-group" key={group.adapter || "pending-adapter"}>
              <div className="connection-adapter-heading">
                <span>
                  <PlugConnected20Regular />
                  <strong>{group.adapter || text("出口待分配", "Egress pending")}</strong>
                  <small>{text(`${group.connections.length} 条连接`, `${group.connections.length} connection(s)`)}</small>
                </span>
                <span className="connection-adapter-speed">
                  <span><ArrowDownload20Regular /> {formatBytes(Math.round(group.downloadBPS))}/s</span>
                  <span><ArrowUpload20Regular /> {formatBytes(Math.round(group.uploadBPS))}/s</span>
                </span>
              </div>
              {group.connections.map(renderConnection)}
            </section>
          )) : filtered.map(renderConnection)}
        </div>
      </GlassSurface>

      <span
        ref={contextMenuTargetRef}
        className="connection-context-anchor"
        style={{ left: contextMenu?.x ?? 0, top: contextMenu?.y ?? 0 }}
        aria-hidden="true"
      />
      <Menu
        open={Boolean(contextMenu)}
        onOpenChange={(_, data) => !data.open && setContextMenu(null)}
        positioning={{ target: contextMenuTargetRef.current, position: "below", align: "start", strategy: "fixed", offset: 4 }}
      >
        <MenuPopover className="connection-rule-menu glass-surface" data-tone="primary">
          <MenuList>
            <MenuGroup>
            <MenuGroupHeader className="connection-rule-menu-heading">
              {text("快速添加分流规则", "Quick add routing rule")}
            </MenuGroupHeader>
            {(contextMenu ? connectionRuleCandidates(contextMenu.connection) : []).map((candidate) => (
              <MenuItem
                key={candidate.matchType}
                icon={candidate.matchType === "process"
                  ? <AppsListDetail24Regular />
                  : candidate.matchType === "domain"
                    ? <Globe20Regular />
                    : <PlugConnected20Regular />}
                onClick={() => contextMenu && void openQuickRule(candidate, contextMenu.connection)}
              >
                <span className="connection-rule-menu-item">
                  <span>{text(`按${matchTypeLabel(candidate.matchType)}添加`, `Add by ${matchTypeLabel(candidate.matchType).toLocaleLowerCase()}`)}</span>
                  <small title={candidate.value}>{candidate.value}</small>
                </span>
              </MenuItem>
            ))}
            </MenuGroup>
          </MenuList>
        </MenuPopover>
      </Menu>

      <Dialog
        open={quickRuleOpen}
        onOpenChange={(_, data) => {
          if (!data.open && !quickRuleSaving) {
            quickRuleRequest.current += 1;
            setQuickRuleOpen(false);
          }
        }}
      >
        <DialogSurface className="routing-batch-dialog quick-rule-dialog glass-surface" data-tone="primary">
          <DialogBody>
            <DialogTitle className="routing-batch-title">{text("添加分流规则", "Add routing rule")}</DialogTitle>
            <DialogContent>
              {quickRule && (
                <>
                  <div className="quick-rule-summary">
                    <Badge appearance="tint" color="brand">{matchTypeLabel(quickRule.matchType)}</Badge>
                    <strong title={quickRule.value}>{quickRule.value}</strong>
                    <small>{text("来自当前活动连接", "From the active connection")}</small>
                  </div>
                  <label className="quick-rule-field">
                    <span>{text("出口策略", "Egress policy")}</span>
                    <Dropdown
                      appearance="filled-darker"
                      value={outboundLabel(quickRuleOutbound)}
                      selectedOptions={quickRuleOutbound ? [quickRuleOutbound] : []}
                      disabled={quickRuleLoading || quickRuleSaving}
                      onOptionSelect={(_, data) => data.optionValue && changeQuickRuleOutbound(data.optionValue)}
                    >
                      {(quickRuleSnapshot.outbounds ?? []).map((outbound) => (
                        <Option key={outbound.id} value={outbound.id}>{outboundLabel(outbound.id)}</Option>
                      ))}
                    </Dropdown>
                  </label>
                  {quickRuleLoading ? (
                    <div className="quick-rule-status"><Spinner size="tiny" label={text("正在检查规则", "Checking rule")} /></div>
                  ) : quickRulePreview?.conflict_count ? (
                    <MessageBar intent="warning">
                      <MessageBarBody>{text(
                        `已有相同${matchTypeLabel(quickRule.matchType)}规则指向${outboundLabel(quickRulePreview.items?.[0]?.existing_outbound ?? "")}；保存时只更新这一条。`,
                        `An identical ${matchTypeLabel(quickRule.matchType).toLocaleLowerCase()} rule currently uses ${outboundLabel(quickRulePreview.items?.[0]?.existing_outbound ?? "")}; only that rule will be updated.`,
                      )}</MessageBarBody>
                    </MessageBar>
                  ) : quickRulePreview?.duplicate_count ? (
                    <MessageBar intent="info">
                      <MessageBarBody>{text("这条规则和出口已经存在，不会重复添加。", "This rule and egress already exist; no duplicate will be added.")}</MessageBarBody>
                    </MessageBar>
                  ) : quickRulePreview?.invalid_count ? (
                    <MessageBar intent="error"><MessageBarBody>{quickRulePreview.items?.[0]?.message}</MessageBarBody></MessageBar>
                  ) : (
                    <small className="routing-batch-helper">{text(
                      "保存后自动热更新；已有连接保持当前路径，新连接使用新规则。",
                      "Rules hot-reload after saving; existing connections keep their path and new connections use the new rule.",
                    )}</small>
                  )}
                </>
              )}
            </DialogContent>
            <DialogActions className="routing-batch-actions">
              <Button disabled={quickRuleSaving} onClick={() => {
                quickRuleRequest.current += 1;
                setQuickRuleOpen(false);
              }}>{text("取消", "Cancel")}</Button>
              <Button
                appearance="primary"
                icon={quickRuleSaving ? <Spinner size="tiny" /> : <Add20Regular />}
                disabled={quickRuleLoading || quickRuleSaving || !quickRuleOutbound || Boolean(quickRulePreview?.invalid_count || quickRulePreview?.duplicate_count)}
                onClick={() => void saveQuickRule()}
              >
                {quickRulePreview?.conflict_count ? text("更新规则", "Update rule") : text("添加规则", "Add rule")}
              </Button>
            </DialogActions>
          </DialogBody>
        </DialogSurface>
      </Dialog>
    </main>
  );
}
