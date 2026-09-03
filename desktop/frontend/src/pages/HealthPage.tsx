import {
  Accordion,
  AccordionHeader,
  AccordionItem,
  AccordionPanel,
  Badge,
  Button,
  Checkbox,
  ProgressBar,
  Spinner,
  Tab,
  TabList,
  Tooltip,
} from "@fluentui/react-components";
import {
  ArrowSync20Regular,
  CheckmarkCircle20Regular,
  Dismiss20Regular,
  HeartPulse20Regular,
  Stop20Regular,
  Warning20Regular,
} from "@fluentui/react-icons";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { GlassSurface } from "../components/material/GlassSurface";
import {
  appServices,
  type AdapterView,
  type DiagnosticResult,
  type DiagnosticSnapshot,
  withServiceTimeout,
} from "../platform/services";
import { useI18n } from "../i18n/i18n";
import { isDesktopRuntime } from "../platform/runtime";
import { adapterSaveInput, adapterSaveQueue } from "../platform/adapterSaveQueue";
import { startSerialPoll } from "../platform/serialPoll";
import type { EnginePhase } from "../state/useEngineState";
import { adapterListKey } from "../state/adapterRuntime";
import { NATDetectionPage } from "./NATDetectionPage";
import { isNATDetectionBlocked } from "./natDetectionPolicy";
import { conciseDiagnosticMessage, type HealthNoticeIntent } from "./healthNotice";

const isBrowserPreview = () => import.meta.env.DEV && !isDesktopRuntime();

const emptySnapshot = (): DiagnosticSnapshot => ({
  state: "idle",
  target_ip: "223.5.5.5",
  total: 0,
  completed: 0,
  results: [],
});

type HealthNotice = {
  title: string;
  message: string;
  intent: HealthNoticeIntent;
};

const previewAdapters = (): AdapterView[] => [
  {
    id: "Ethernet", name: "以太网", description: "Realtek PCIe 2.5GbE",
    address: "192.168.10.24", prefix_length: 24, if_index: 12, gateway: "192.168.10.1",
    dns_servers: ["223.5.5.5", "1.1.1.1"], metric: 25, automatic_metric: true,
    selected: true, weight: 3, kind: "ethernet", operational: true,
  },
  {
    id: "WLAN", name: "WLAN", description: "Intel Wi-Fi 6E AX211",
    address: "192.168.31.108", prefix_length: 24, if_index: 18, gateway: "192.168.31.1",
    dns_servers: ["192.168.31.1"], metric: 35, automatic_metric: true,
    selected: true, weight: 2, kind: "wifi", operational: true,
  },
];

const previewResult = (adapter: AdapterView, index: number): DiagnosticResult => ({
  adapter_id: adapter.id,
  name: adapter.name,
  address: adapter.address,
  status: index === 0 ? "available" : "unstable",
  loss_rate: index === 0 ? 0 : 10,
  avg_latency_ms: index === 0 ? 18 : 64,
  jitter_ms: index === 0 ? 7 : 126,
  sent: 10,
  received: index === 0 ? 10 : 9,
  target_ip: "223.5.5.5",
  bound_tcp_ok: true,
  bound_tcp_detail: `TCP 223.5.5.5:443 via ${adapter.address} (ifIndex ${adapter.if_index})`,
  checks: [
    { key: "source_binding", level: "pass", detail: adapter.address },
    { key: "gateway", level: "pass", detail: adapter.gateway ?? "" },
    { key: "dns", level: "pass", detail: (adapter.dns_servers ?? []).join(", ") },
    { key: "metric", level: "pass", detail: String(adapter.metric), mode: "auto" },
  ],
  completed_at: new Date().toISOString(),
});

export function HealthPage({
  adapterRuntime,
  enginePhase,
}: {
  adapterRuntime?: readonly AdapterView[];
  enginePhase?: EnginePhase;
}) {
  const { locale } = useI18n();
  const text = useCallback((zh: string, en: string) => locale === "en" ? en : zh, [locale]);
  const statusMeta = useMemo(() => ({
    available: {
      label: text("可用", "Available"),
      color: "success" as const,
      description: text("绑定出口可达，链路可加入聚合池。", "The bound exit is reachable and can join the aggregation pool."),
    },
    unstable: {
      label: text("不稳定", "Unstable"),
      color: "warning" as const,
      description: text("ICMP 丢包或抖动偏高，使用时可能波动。", "ICMP loss or jitter is high and may cause fluctuations."),
    },
    unavailable: {
      label: text("不可用", "Unavailable"),
      color: "danger" as const,
      description: text("绑定 TCP 失败，该网卡当前无法作为可靠出口。", "The bound TCP check failed; this adapter is not a reliable exit."),
    },
  }), [text]);
  const checkLabels = useMemo<Record<string, string>>(() => ({
    source_binding: text("源地址与接口绑定", "Source address and interface binding"),
    gateway: text("默认网关", "Default gateway"),
    dns: text("DNS 配置", "DNS configuration"),
    metric: text("路由跃点", "Route metric"),
  }), [text]);
  const [adapters, setAdapters] = useState<AdapterView[]>([]);
  const [snapshot, setSnapshot] = useState<DiagnosticSnapshot>(emptySnapshot);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [preview, setPreview] = useState(false);
  const [engineRunning, setEngineRunning] = useState(false);
  const [healthView, setHealthView] = useState<"link" | "nat">("link");
  const [notice, setNotice] = useState<HealthNotice>();
  const adaptersRef = useRef<AdapterView[]>([]);
  const adapterRuntimeRef = useRef(adapterRuntime);
  const adapterRuntimeKeyRef = useRef<string>();
  const enginePhaseRef = useRef(enginePhase);
  const homeSettingsRef = useRef({ mode: "proxy", weighted: false });
  const stopPoller = useRef<(() => void)>();
  const diagnosticEpoch = useRef(0);
  const noticeTimer = useRef<ReturnType<typeof window.setTimeout>>();
  const mounted = useRef(true);
  adapterRuntimeRef.current = adapterRuntime;
  enginePhaseRef.current = enginePhase;

  const notify = useCallback((title: string, message: string, intent: HealthNoticeIntent = "error") => {
    if (noticeTimer.current !== undefined) window.clearTimeout(noticeTimer.current);
    const conciseMessage = intent === "success"
      ? message
      : conciseDiagnosticMessage(message, locale);
    if (conciseMessage !== message) console.error(`[${title}] ${message}`);
    setNotice({ title, message: conciseMessage, intent });
    if (intent === "success") {
      noticeTimer.current = window.setTimeout(() => {
        if (mounted.current) setNotice(undefined);
        noticeTimer.current = undefined;
      }, 3200);
    }
  }, [locale]);

  const load = useCallback(async () => {
    const runtimeTask = enginePhaseRef.current === undefined
      ? withServiceTimeout(
        appServices.engine.snapshot(),
        10_000,
        text("读取 Core 状态", "Loading Core status"),
      ).then((engine) => {
        setEngineRunning(isNATDetectionBlocked(engine.phase as EnginePhase));
      }).catch((error) => {
        if (!isBrowserPreview()) {
          notify(
            text("Core 状态暂不可用", "Core status is temporarily unavailable"),
            error instanceof Error ? error.message : String(error),
            "warning",
          );
        }
      })
      : Promise.resolve();
    try {
      const [nextAdapters, latest, settings] = await withServiceTimeout(Promise.all([
        adapterRuntimeRef.current !== undefined
          ? Promise.resolve([...adapterRuntimeRef.current])
          : appServices.adapters.list(),
        appServices.diagnostics.latest(),
        appServices.settings.get(),
      ]), 10_000, text("读取网络体检数据", "Loading network diagnostics"));
      const authoritativeAdapters = adapterRuntimeRef.current !== undefined
        ? [...adapterRuntimeRef.current]
        : nextAdapters ?? [];
      setAdapters(authoritativeAdapters);
      adaptersRef.current = authoritativeAdapters;
      setSnapshot(latest);
      homeSettingsRef.current = { mode: settings.mode, weighted: settings.weighted };
      setPreview(false);
    } catch (error) {
      if (isBrowserPreview()) {
        const fixtures = previewAdapters();
        adaptersRef.current = fixtures;
        const showResults = new URLSearchParams(window.location.search).get("diagnostic") !== "empty";
        setAdapters(fixtures);
        setSnapshot(showResults ? {
          state: "completed",
          run_id: "browser-fixture",
          target_ip: "223.5.5.5",
          total: fixtures.length,
          completed: fixtures.length,
          results: fixtures.map(previewResult),
          started_at: new Date(Date.now() - 18_000).toISOString(),
          completed_at: new Date().toISOString(),
        } : emptySnapshot());
        setPreview(true);
      } else {
        notify(text("无法读取网络体检", "Unable to load network diagnostics"), error instanceof Error ? error.message : String(error));
      }
    } finally {
      setLoading(false);
    }
    void runtimeTask;
  }, [notify, text]);

  useEffect(() => {
    if (adapterRuntime === undefined) return;
    const nextKey = adapterListKey(adapterRuntime);
    if (adapterRuntimeKeyRef.current === nextKey) return;
    adapterRuntimeKeyRef.current = nextKey;
    const next = [...adapterRuntime];
    adaptersRef.current = next;
    setAdapters(next);
    setLoading(false);
  }, [adapterRuntime]);

  useEffect(() => {
    if (enginePhase === undefined) return;
    setEngineRunning(isNATDetectionBlocked(enginePhase));
  }, [enginePhase]);

  const loadRef = useRef(load);
  loadRef.current = load;

  useEffect(() => {
    mounted.current = true;
    // 通过 ref 调用最新的 load，使语言切换重建回调时不会重新触发整页加载。
    void loadRef.current();
    return () => {
      mounted.current = false;
      diagnosticEpoch.current += 1;
      stopPoller.current?.();
      if (noticeTimer.current !== undefined) window.clearTimeout(noticeTimer.current);
    };
  }, []);

  const persistAdapters = useCallback((next: AdapterView[]) => {
    adaptersRef.current = next;
    setAdapters(next);
    if (preview) return Promise.resolve(next);
    const settings = homeSettingsRef.current;
    const handle = adapterSaveQueue.enqueue(adapterSaveInput(settings.mode, settings.weighted, next));
    void handle.done.then((saved) => {
      if (!mounted.current || !adapterSaveQueue.isCurrent(handle.revision)) return;
      const authoritative = saved ?? next;
      adaptersRef.current = authoritative;
      setAdapters(authoritative);
    }).catch((error) => {
      if (!mounted.current || !adapterSaveQueue.isCurrent(handle.revision)) return;
      notify(text("未能同步网卡选择", "Unable to sync adapter selection"), error instanceof Error ? error.message : String(error));
      void load();
    });
    return handle.done;
  }, [load, notify, preview, text]);

  const setAll = useCallback((checked: boolean) => {
    void persistAdapters(adaptersRef.current.map((adapter) => ({ ...adapter, selected: checked })));
  }, [persistAdapters]);

  const refresh = useCallback(async () => {
    setRefreshing(true);
    try {
      if (preview) {
        const next = previewAdapters();
        adaptersRef.current = next;
        setAdapters(next);
      } else {
        const next = await appServices.adapters.refresh() ?? [];
        adaptersRef.current = next;
        setAdapters(next);
      }
    } catch (error) {
      notify(text("重新扫描失败", "Rescan failed"), error instanceof Error ? error.message : String(error));
    } finally {
      setRefreshing(false);
    }
  }, [notify, preview, text]);

  const start = useCallback(async () => {
    setNotice(undefined);
    const selected = adaptersRef.current.filter((adapter) => adapter.selected);
    if (selected.length === 0) {
      notify(
        text("尚未选择网卡", "No adapter selected"),
        text("请至少选择一张拥有有效 IPv4 的活动网卡。", "Select at least one active adapter with a valid IPv4 address."),
        "warning",
      );
      return;
    }
    if (preview) {
      setSnapshot({
        state: "running", run_id: "browser-fixture", target_ip: "223.5.5.5",
        total: selected.length, completed: 0, results: [], started_at: new Date().toISOString(),
      });
      window.setTimeout(() => {
        if (!mounted.current) return;
        const final = {
          state: "completed", run_id: "browser-fixture", target_ip: "223.5.5.5",
          total: selected.length, completed: selected.length,
          results: selected.map(previewResult), started_at: new Date(Date.now() - 1500).toISOString(),
          completed_at: new Date().toISOString(),
        } satisfies DiagnosticSnapshot;
        setSnapshot(final);
        notify(
          text("网络体检完成", "Network diagnostics complete"),
          text(
            `已完成 ${final.completed} 张网卡的绑定链路检查。`,
            `Bound-path checks completed for ${final.completed} adapters.`,
          ),
          "success",
        );
      }, 850);
      return;
    }
    try {
      await adapterSaveQueue.flush();
    } catch (error) {
      notify(
        text("网卡选择尚未保存", "Adapter selection is not saved"),
        error instanceof Error ? error.message : String(error),
      );
      return;
    }
    setSnapshot({
      state: "running", target_ip: "223.5.5.5", total: selected.length,
      completed: 0, results: [], started_at: new Date().toISOString(),
    });
    const runEpoch = ++diagnosticEpoch.current;
    stopPoller.current?.();
    stopPoller.current = startSerialPoll(async () => {
      const latest = await appServices.diagnostics.latest();
      if (mounted.current && diagnosticEpoch.current === runEpoch) setSnapshot(latest);
    }, 400);
    try {
      const final = await appServices.diagnostics.run(selected.map((adapter) => adapter.id));
      if (!mounted.current || diagnosticEpoch.current !== runEpoch) return;
      diagnosticEpoch.current += 1;
      stopPoller.current?.();
      stopPoller.current = undefined;
      setSnapshot(final);
      if (final.state === "completed") {
        notify(
          text("网络体检完成", "Network diagnostics complete"),
          text(
            `已完成 ${final.completed} 张网卡的绑定链路检查。`,
            `Bound-path checks completed for ${final.completed} adapters.`,
          ),
          "success",
        );
      }
    } catch (error) {
      if (!mounted.current || diagnosticEpoch.current !== runEpoch) return;
      notify(text("网络体检失败", "Network diagnostics failed"), error instanceof Error ? error.message : String(error));
      const latest = await appServices.diagnostics.latest().catch(() => undefined);
      if (latest && mounted.current && diagnosticEpoch.current === runEpoch) setSnapshot(latest);
    } finally {
      if (diagnosticEpoch.current === runEpoch) {
        diagnosticEpoch.current += 1;
        stopPoller.current?.();
        stopPoller.current = undefined;
      }
    }
  }, [notify, preview, text]);

  const cancel = useCallback(async () => {
    if (preview) {
      setSnapshot((current) => ({ ...current, state: "cancelled", completed_at: new Date().toISOString() }));
      return;
    }
    try {
      await appServices.diagnostics.cancel();
    } catch (error) {
      notify(text("取消失败", "Cancel failed"), error instanceof Error ? error.message : String(error));
    }
  }, [notify, preview, text]);

  const results = snapshot.results ?? [];
  const selectedCount = adapters.filter((adapter) => adapter.selected).length;
  const running = snapshot.state === "running";
  const progress = snapshot.total > 0 ? snapshot.completed / snapshot.total : 0;
  const resultByID = useMemo(() => new Map(results.map((result) => [result.adapter_id, result])), [results]);

  return (
    <main className="health-page">
      <header className="health-heading">
        <div key={healthView} className="health-heading-copy">
          <span className="section-kicker">{healthView === "link"
            ? text("逐接口真实出口验证", "Per-interface exit verification")
            : text("UDP 映射与过滤行为", "UDP mapping and filtering behavior")}</span>
          <h1>{healthView === "link" ? text("网络体检", "Network diagnostics") : text("NAT 类型检测", "NAT type detection")}</h1>
          <p>{healthView === "link" ? text(
            "ICMP 负责质量数据，绑定 TCP 负责确认流量确实从所选网卡发出。",
            "ICMP measures link quality while bound TCP confirms traffic actually leaves through the selected adapter.",
          ) : text(
            "使用 RFC 5780 分析所选出口的 UDP 映射与过滤行为，并给出经典 NAT 类型。",
            "Analyze UDP mapping and filtering behavior over the selected egress using RFC 5780.",
          )}</p>
        </div>
        <div key={`actions-${healthView}`} className="health-heading-actions health-heading-actions-enter">
          {healthView === "nat" ? <span>RFC 5780 · UDP</span> : <span>{text("目标", "Target")} {snapshot.target_ip || "223.5.5.5"}</span>}
          {healthView === "link" && (running ? (
            <Button appearance="secondary" icon={<Stop20Regular />} onClick={cancel}>{text("取消体检", "Cancel")}</Button>
          ) : (
            <Button
              appearance="primary"
              icon={<HeartPulse20Regular />}
              disabled={loading || selectedCount === 0}
              onClick={start}
            >
              {text("开始体检", "Start diagnostics")}
            </Button>
          ))}
        </div>
      </header>

      <nav className="health-subnav" aria-label={text("网络体检页面", "Network diagnostic pages")}>
        <TabList
          selectedValue={healthView}
          onTabSelect={(_, data) => setHealthView(data.value as "link" | "nat")}
        >
          <Tab value="link">{text("链路体检", "Link diagnostics")}</Tab>
          <Tab value="nat">{text("NAT 类型检测", "NAT type detection")}</Tab>
        </TabList>
        {notice && (
          <div
            className={`health-inline-notice is-${notice.intent}`}
            role={notice.intent === "error" ? "alert" : "status"}
            aria-live={notice.intent === "error" ? "assertive" : "polite"}
          >
            <span className="health-inline-notice-icon" aria-hidden="true">
              {notice.intent === "success" ? <CheckmarkCircle20Regular /> : <Warning20Regular />}
            </span>
            <span className="health-inline-notice-copy">
              <strong>{notice.title}</strong>
              <span>{notice.message}</span>
            </span>
            <Button
              appearance="subtle"
              size="small"
              icon={<Dismiss20Regular />}
              aria-label={text("关闭提示", "Dismiss notification")}
              onClick={() => setNotice(undefined)}
            />
          </div>
        )}
      </nav>

      {healthView === "link" ? (
        <div className="health-link-page health-view-enter">
          <GlassSurface className="health-adapter-surface" tone="secondary">
        <div className="health-adapter-toolbar">
          <div>
            <strong>{text("参与体检的网卡", "Adapters to diagnose")}</strong>
            <span>{text(
              `${selectedCount} / ${adapters.length} 已选择，与首页保持同步`,
              `${selectedCount} / ${adapters.length} selected, synced with Home`,
            )}</span>
          </div>
          <Button size="small" appearance="subtle" icon={<CheckmarkCircle20Regular />}
            disabled={running || engineRunning || adapters.length === 0} onClick={() => setAll(true)}>
            {text("全选", "Select all")}
          </Button>
          <Button size="small" appearance="subtle" icon={<Dismiss20Regular />}
            disabled={running || engineRunning || selectedCount === 0} onClick={() => setAll(false)}>
            {text("取消全选", "Clear selection")}
          </Button>
          <Button size="small" appearance="subtle"
            icon={refreshing ? <Spinner size="tiny" /> : <ArrowSync20Regular />}
            disabled={running || engineRunning || refreshing} onClick={refresh}>
            {text("重新扫描", "Rescan")}
          </Button>
        </div>
        <div className="health-adapter-list">
          {loading ? <Spinner label={text("正在读取活动网卡", "Loading active adapters")} /> : adapters.length === 0 ? (
            <div className="health-empty-inline">{text("未发现拥有有效 IPv4 的活动网卡。", "No active adapter with a valid IPv4 address was found.")}</div>
          ) : adapters.map((adapter) => {
            const result = resultByID.get(adapter.id);
            return (
              <label className={`health-adapter-choice hm-card${adapter.selected ? " is-selected" : ""}`} key={adapter.id}>
                <Checkbox
                  checked={adapter.selected}
                  disabled={running || engineRunning}
                  onChange={(_, data) => void persistAdapters(adaptersRef.current.map((item) =>
                    item.id === adapter.id ? { ...item, selected: data.checked === true } : item))}
                  aria-label={text(
                    `${adapter.selected ? "取消" : "选择"} ${adapter.name}`,
                    `${adapter.selected ? "Deselect" : "Select"} ${adapter.name}`,
                  )}
                />
                <span>
                  <strong>{adapter.name}</strong>
                  <small>{adapter.address} · IF {adapter.if_index}</small>
                </span>
                {result && (
                  <Badge appearance="tint" color={statusMeta[result.status as keyof typeof statusMeta]?.color ?? "informative"}>
                    {statusMeta[result.status as keyof typeof statusMeta]?.label ?? result.status}
                  </Badge>
                )}
              </label>
            );
          })}
        </div>
        {engineRunning && (
          <div className="health-lock-note">{text(
            "聚合引擎运行期间保持网卡选择不变；仍可对当前选择执行体检。",
            "Adapter selection is locked while aggregation is running; diagnostics can still run against the current selection.",
          )}</div>
        )}
          </GlassSurface>

          <section className={`health-results${results.length === 0 ? " is-empty" : ""}`} aria-live="polite">
        <div className="health-results-heading">
          <div>
            <strong>{text("链路体检报告", "Link diagnostic report")}</strong>
            <span key={`${snapshot.state}-${snapshot.completed}-${results.length}`} className="motion-inline-swap">{running
              ? text(`正在检查 ${snapshot.completed + 1} / ${snapshot.total}`, `Checking ${snapshot.completed + 1} / ${snapshot.total}`)
              : text(`${results.length} 项结果`, `${results.length} results`)}</span>
          </div>
          {running && <ProgressBar value={progress} />}
          {snapshot.state === "cancelled" && <Badge appearance="tint" color="warning">{text("体检已取消", "Diagnostics cancelled")}</Badge>}
        </div>
        <div className="health-result-list">
          {running && results.length === 0 ? (
            <GlassSurface key="health-running-empty" className="health-empty-result motion-state-content" tone="secondary">
              <Spinner size="medium" />
              <strong>{text("正在建立第一条绑定探测", "Starting the first bound probe")}</strong>
              <span>{text(
                "每张网卡最多发送 10 个 ICMP 探针，并依次尝试三个 TCP 目标。",
                "Each adapter sends up to 10 ICMP probes and tries three TCP targets in sequence.",
              )}</span>
            </GlassSurface>
          ) : results.length === 0 ? (
            <GlassSurface key="health-idle-empty" className="health-empty-result motion-state-content" tone="secondary">
              <HeartPulse20Regular />
              <strong>{text("尚无体检结果", "No diagnostic results")}</strong>
              <span>{text(
                "选择网卡后开始体检；结果会同步回首页的健康状态、延迟与丢包。",
                "Select adapters and start diagnostics. Health, latency, and loss results are also shown on Home.",
              )}</span>
              <Button
                appearance="primary"
                icon={<HeartPulse20Regular />}
                disabled={loading || selectedCount === 0}
                onClick={start}
              >
                {text("开始体检", "Start diagnostics")}
              </Button>
            </GlassSurface>
          ) : results.map((result) => {
            const meta = statusMeta[result.status as keyof typeof statusMeta] ?? statusMeta.unavailable;
            return (
              <GlassSurface as="article" className="health-result-row" tone="secondary" key={result.adapter_id}>
                <div className="health-result-identity">
                  <Badge appearance="tint" color={meta.color}>{meta.label}</Badge>
                  <div><strong>{result.name}</strong><span>{result.address} → {result.target_ip}</span></div>
                </div>
                <div className="health-metrics">
                  <span><small>{text("丢包", "Loss")}</small><strong>{result.loss_rate}%</strong></span>
                  <span><small>{text("平均延迟", "Average latency")}</small><strong>{result.avg_latency_ms} ms</strong></span>
                  <span><small>{text("抖动", "Jitter")}</small><strong>{result.jitter_ms} ms</strong></span>
                </div>
                <div className="health-result-summary">
                  <strong>{meta.description}</strong>
                  <span>{result.bound_tcp_detail}</span>
                </div>
                <Accordion collapsible className="health-checks">
                  <AccordionItem value={result.adapter_id}>
                    <AccordionHeader size="small">{text("配置检查与探测证据", "Configuration checks and probe evidence")}</AccordionHeader>
                    <AccordionPanel>
                      {(result.checks ?? []).map((check) => (
                        <div className={`health-check health-check-${check.level}`} key={check.key}>
                          <span>{check.level === "pass" ? "✓" : check.level === "fail" ? "✕" : "!"}</span>
                          <strong>{checkLabels[check.key] ?? check.key}</strong>
                          <span>
                            {check.detail || text("未配置或未读取到", "Not configured or unavailable")}
                            {check.key === "metric" && check.detail
                              ? ` · ${check.mode === "auto" ? text("自动跃点", "Automatic metric") : text("固定跃点", "Fixed metric")}`
                              : ""}
                          </span>
                        </div>
                      ))}
                      {result.note && <div className="health-probe-note">ICMP: {result.note}</div>}
                    </AccordionPanel>
                  </AccordionItem>
                </Accordion>
              </GlassSurface>
            );
          })}
        </div>
          </section>
        </div>
      ) : (
        <NATDetectionPage
          adapters={adapters}
          enginePhase={enginePhase}
          loading={loading}
          preview={preview}
          text={text}
          notify={notify}
        />
      )}
      {preview && (
        <Tooltip content={text("浏览器预览不会发送真实网络探针", "Browser preview does not send real network probes")} relationship="description">
          <span className="health-preview-label">{text("视觉容量夹具", "Visual capacity fixture")}</span>
        </Tooltip>
      )}
    </main>
  );
}
