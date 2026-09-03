import { useCallback, useEffect, useRef, useState } from "react";
import {
  Badge,
  Button,
  Dialog,
  DialogActions,
  DialogBody,
  DialogContent,
  DialogSurface,
  DialogTitle,
  Spinner,
  Tooltip,
} from "@fluentui/react-components";
import {
  ArrowSync20Regular,
  CheckmarkCircle20Regular,
  Dismiss20Regular,
  ShieldError20Regular,
  Warning20Regular,
} from "@fluentui/react-icons";
import { useEngineState, type EnginePhase, type HomeAdapter } from "../state/useEngineState";
import { EngineHero } from "../components/home/EngineHero";
import { NetworkAdapterItem } from "../components/home/NetworkAdapterItem";
import { RuntimeStatusBar } from "../components/home/RuntimeStatusBar";
import type { AppPage } from "../components/shell/CompactNavigation";
import type { TunPreflightSnapshot } from "../platform/services";
import { useI18n } from "../i18n/i18n";
import { useAppNotifications } from "../components/notifications/AppNotifications";

const formatBytes = (value: number) => {
  if (value >= 1024 ** 3) return `${(value / 1024 ** 3).toFixed(2)} GB`;
  if (value >= 1024 ** 2) return `${(value / 1024 ** 2).toFixed(1)} MB`;
  if (value >= 1024) return `${(value / 1024).toFixed(0)} KB`;
  return `${value} B`;
};

export function HomePage({
  onNavigate,
  onAdapterRuntimeChange,
  onEnginePhaseChange,
}: {
  onNavigate?: (page: AppPage, adapterName?: string) => void;
  onAdapterRuntimeChange?: (adapters: HomeAdapter[] | undefined) => void;
  onEnginePhaseChange?: (phase: EnginePhase) => void;
}) {
  const { locale, t } = useI18n();
  const text = (zh: string, en: string) => locale === "en" ? en : zh;
  const [preflightDialog, setPreflightDialog] = useState<TunPreflightSnapshot | null>(null);
  const [preflightDialogOpen, setPreflightDialogOpen] = useState(false);
  const [showPreflightDetails, setShowPreflightDetails] = useState(false);
  const preflightResolver = useRef<((confirmed: boolean) => void) | null>(null);
  const { notify } = useAppNotifications();
  const notifyError = useCallback((message: string, retry?: () => void) => {
    const informational = message.includes("Steam") || message.startsWith("提示：");
    notify({
      title: informational ? t("infobar_info") : text("操作未完成", "Operation not completed"),
      message,
      intent: informational ? "info" : "error",
      action: retry ? { label: text("重试", "Retry"), onClick: retry } : undefined,
      dedupeKey: informational ? "home:engine-info" : "home:engine-error",
    });
  }, [locale, notify, t]);
  const handleTunPreflight = useCallback((snapshot: TunPreflightSnapshot) => {
    return new Promise<boolean>((resolve) => {
      preflightResolver.current?.(false);
      preflightResolver.current = resolve;
      setShowPreflightDetails(false);
      setPreflightDialog(snapshot);
      setPreflightDialogOpen(true);
    });
  }, []);
  const closeTunPreflight = useCallback((confirmed: boolean) => {
    const resolve = preflightResolver.current;
    preflightResolver.current = null;
    setPreflightDialogOpen(false);
    resolve?.(confirmed);
  }, []);
  useEffect(() => () => preflightResolver.current?.(false), []);
  const engine = useEngineState(notifyError, handleTunPreflight);
  useEffect(
    () => onAdapterRuntimeChange?.(engine.loading ? undefined : engine.adapters),
    [engine.adapters, engine.loading, onAdapterRuntimeChange],
  );
  useEffect(() => onEnginePhaseChange?.(engine.phase), [engine.phase, onEnginePhaseChange]);
  const previousEnginePhase = useRef<EnginePhase>();
  useEffect(() => {
    const previous = previousEnginePhase.current;
    previousEnginePhase.current = engine.phase;
    if (previous === "starting" && engine.phase === "running") {
      notify({
        title: text("加速已启动", "Acceleration started"),
        message: text(
          `${engine.selected.length} 条链路已加入${engine.mode === "tun" ? "虚拟网卡" : "系统代理"}加速。`,
          `${engine.selected.length} link(s) joined ${engine.mode === "tun" ? "Virtual NIC" : "System Proxy"} acceleration.`,
        ),
        intent: "success",
        dedupeKey: "home:engine-started",
      });
    } else if (previous === "stopping" && engine.phase === "stopped") {
      notify({
        title: text("加速已停止", "Acceleration stopped"),
        message: text("系统网络设置已安全恢复。", "System network settings were restored safely."),
        intent: "info",
        dedupeKey: "home:engine-stopped",
      });
    } else if (previous === "running" && engine.phase === "degraded") {
      notify({
        title: text("加速链路状态波动", "Acceleration link degraded"),
        message: text("部分链路暂时不可用，正在自动调整流量。", "Some links are unavailable; traffic is being adjusted automatically."),
        intent: "warning",
        dedupeKey: "home:engine-degraded",
      });
    }
  }, [engine.mode, engine.phase, engine.selected.length, locale, notify]);
  const preflightIssues = preflightDialog?.issues ?? [];
  const blockerCount = preflightIssues.filter((issue) => issue.level === "blocker").length;
  const warningCount = preflightIssues.filter((issue) => issue.level === "warning").length;

  return (
    <main className="home-page">
      <EngineHero
        phase={engine.phase}
        mode={engine.mode}
        selectedCount={engine.selected.length}
        download={engine.totalDownload / (1024 * 1024)}
        upload={engine.totalUpload / (1024 * 1024)}
        connections={engine.totalConnections}
        history={engine.history}
        transitioning={engine.transitioning}
        weighted={engine.weighted}
        socksPort={engine.ports.socks}
        httpPort={engine.ports.http}
        systemProxyTakeover={engine.systemProxyTakeover}
        onModeChange={engine.setMode}
        onWeightedChange={engine.setWeighted}
        onToggle={engine.toggleEngine}
      />

      <section className="network-section" aria-labelledby="network-section-title">
        <div className="network-section-heading">
          <div>
            <span className="section-kicker">{text("参与聚合的链路", "Links participating in aggregation")}</span>
            <h1 id="network-section-title">{text("网络适配器", "Network adapters")}</h1>
          </div>
          <div className="network-section-actions">
            <span>{engine.selected.length} / {engine.adapters.length} {text("已启用", "enabled")}</span>
            <Button
              size="small"
              appearance="subtle"
              icon={<CheckmarkCircle20Regular />}
              disabled={engine.loading || engine.transitioning || engine.adapters.length === 0}
              onClick={() => engine.selectAll(true)}
            >
              {t("home_select_all")}
            </Button>
            <Button
              size="small"
              appearance="subtle"
              icon={<Dismiss20Regular />}
              disabled={engine.loading || engine.transitioning || engine.selected.length === 0}
              onClick={() => engine.selectAll(false)}
            >
              {t("home_deselect_all")}
            </Button>
            <Button
              size="small"
              appearance="subtle"
              icon={engine.refreshing ? <Spinner size="tiny" /> : <ArrowSync20Regular />}
              disabled={engine.refreshing || engine.transitioning}
              onClick={engine.refreshAdapters}
            >
              {t("home_refresh_tip")}
            </Button>
          </div>
        </div>
        <div className="network-adapter-list">
          {engine.loading ? (
            <div className="adapter-empty hm-card"><Spinner label={text("正在扫描活动网络适配器", "Scanning active network adapters")} /></div>
          ) : engine.adapters.length === 0 ? (
            <div className="adapter-empty hm-card">
              <strong>{text("未发现可参与聚合的活动网卡", "No active adapters can participate in aggregation")}</strong>
              <span>{text(
                "请检查网卡是否已连接并具有可用 IPv4 地址，然后重新扫描。",
                "Check that an adapter is connected and has a usable IPv4 address, then scan again.",
              )}</span>
              <Button appearance="primary" icon={<ArrowSync20Regular />} onClick={engine.refreshAdapters}>
                {t("home_refresh_tip")}
              </Button>
            </div>
          ) : engine.adapters.map((adapter) => (
            <NetworkAdapterItem
              key={adapter.id}
              adapter={adapter}
              percentage={adapter.selected ? Math.round((adapter.weight / engine.totalWeight) * 100) || 0 : 0}
              disabled={engine.transitioning || engine.phase === "running"}
              onOpenConnections={() => onNavigate?.("connections", adapter.name)}
              onSelectedChange={(checked) => engine.toggleAdapter(adapter.id, checked)}
              onWeightChange={(value) => engine.updateWeight(adapter.id, value)}
            />
          ))}
        </div>
      </section>

      <RuntimeStatusBar
        phase={engine.phase}
        connections={engine.totalConnections}
        sessionTraffic={engine.sessionBytes > 0 ? formatBytes(engine.sessionBytes) : "—"}
        weighted={engine.weighted}
        coreVersion={engine.coreVersion}
        preview={engine.preview}
        onOpenConnections={() => onNavigate?.("connections")}
      />

      <Dialog
        open={preflightDialogOpen}
        onOpenChange={(_, data) => !data.open && closeTunPreflight(false)}
      >
        <DialogSurface className="tun-preflight-dialog">
          <DialogBody>
            <DialogTitle>
              <span className={`tun-preflight-title${blockerCount > 0 ? " is-blocked" : ""}`}>
                {blockerCount > 0 ? <ShieldError20Regular /> : <Warning20Regular />}
                {blockerCount > 0
                  ? text("虚拟网卡暂时无法启动", "Virtual NIC cannot start yet")
                  : text("检测到启动风险", "Startup risks detected")}
              </span>
            </DialogTitle>
            <DialogContent>
              {preflightDialog && (
                <div className="tun-preflight-content">
                  <div className="tun-preflight-summary">
                    <Badge appearance="filled" color={blockerCount > 0 ? "danger" : "warning"}>
                      {blockerCount > 0
                        ? text(`${blockerCount} 项阻止启动`, `${blockerCount} blocking issue(s)`)
                        : text(`${warningCount} 项风险提示`, `${warningCount} risk warning(s)`)}
                    </Badge>
                    <span>
                      {blockerCount > 0
                        ? text(
                          "检查已在网络接管前完成；系统代理、路由、WFP 和虚拟网卡均未修改。",
                          "Checks completed before network takeover. System proxy, routes, WFP, and the virtual NIC were not changed.",
                        )
                        : text(
                          "可以继续，但当前网络环境可能影响独立出口、兼容性或聚合效果。",
                          "You may continue, but the current network may affect independent egress, compatibility, or aggregation.",
                        )}
                    </span>
                  </div>
                  {showPreflightDetails && (
                    <div className="tun-preflight-evidence" aria-label={text("预检详情", "Preflight details")}>
                      <span>{text("聚合核心", "Aggregation Core")} <strong>{preflightDialog.engine_available ? text("已找到", "Found") : text("缺失", "Missing")}</strong></span>
                      <span>{text("TUN 侧车", "TUN sidecar")} <strong>{preflightDialog.sing_box_available ? text("已找到", "Found") : text("缺失", "Missing")}</strong></span>
                      <span>WFP <strong>{preflightDialog.wfp_ready ? text("可用", "Available") : text("兼容模式", "Compatibility mode")}</strong></span>
                      <span>{text("核心通道", "Core channel")} <strong>{preflightDialog.privilege_broker_available ? text("可用", "Available") : text("不可用", "Unavailable")}</strong></span>
                    </div>
                  )}
                  <div className="tun-preflight-issues">
                    {preflightIssues.map((issue, index) => (
                      <div className={`tun-preflight-issue is-${issue.level}`} key={`${issue.code}-${index}`}>
                        <span className="tun-preflight-issue-icon">
                          {issue.level === "blocker"
                            ? <ShieldError20Regular />
                            : <Warning20Regular />}
                        </span>
                        <div>
                          <strong>{issue.title}</strong>
                          {showPreflightDetails && <p>{issue.detail}</p>}
                        </div>
                      </div>
                    ))}
                  </div>
                </div>
              )}
            </DialogContent>
            <DialogActions>
              <Button appearance="subtle" onClick={() => setShowPreflightDetails((value) => !value)}>
                {showPreflightDetails ? text("收起详情", "Hide details") : text("查看详情", "View details")}
              </Button>
              <Button appearance="secondary" onClick={() => closeTunPreflight(false)}>
                {text("返回修改", "Go back")}
              </Button>
              {blockerCount > 0 ? (
                <Tooltip content={text("存在必须先处理的阻断项", "Blocking issues must be resolved first")} relationship="description">
                  <span>
                    <Button appearance="primary" disabled>
                      {text("继续", "Continue")}
                    </Button>
                  </span>
                </Tooltip>
              ) : (
                <Button appearance="primary" onClick={() => closeTunPreflight(true)}>
                  {text("继续", "Continue")}
                </Button>
              )}
            </DialogActions>
          </DialogBody>
        </DialogSurface>
      </Dialog>
    </main>
  );
}
