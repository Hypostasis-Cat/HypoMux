import { Badge, Button, Spinner, Switch, Tab, TabList } from "@fluentui/react-components";
import {
  Navigation20Regular,
  Play20Filled,
  PlugConnected20Regular,
  Stop20Filled,
} from "@fluentui/react-icons";
import type { EngineMode, EnginePhase } from "../../state/useEngineState";
import { GlassSurface } from "../material/GlassSurface";
import { ThroughputDisplay } from "./ThroughputDisplay";
import { useI18n } from "../../i18n/i18n";

export function EngineHero({
  phase,
  mode,
  selectedCount,
  download,
  upload,
  connections,
  history,
  transitioning,
  weighted,
  socksPort,
  httpPort,
  onModeChange,
  onWeightedChange,
  onToggle,
}: {
  phase: EnginePhase;
  mode: EngineMode;
  selectedCount: number;
  download: number;
  upload: number;
  connections: number;
  history: number[];
  transitioning: boolean;
  weighted: boolean;
  socksPort: number;
  httpPort: number;
  onModeChange: (mode: EngineMode) => void;
  onWeightedChange: (value: boolean) => void;
  onToggle: () => void;
}) {
  const { locale, t } = useI18n();
  const text = (zh: string, en: string) => locale === "en" ? en : zh;
  const macOS = navigator.platform.toLowerCase().includes("mac");
  const active = phase === "running" || phase === "degraded" || phase === "starting";
  const actionLabel = phase === "starting"
    ? text("正在启动", "Starting")
    : phase === "stopping"
      ? text("正在停止", "Stopping")
      : phase === "running" || phase === "degraded"
        ? text("停止聚合", "Stop aggregation")
        : text("启动聚合", "Start aggregation");
  const phaseLabel = phase === "running"
    ? text("运行中", "Running")
    : phase === "degraded"
      ? text("降级运行", "Degraded")
    : phase === "starting"
      ? text("正在启动", "Starting")
      : phase === "stopping"
        ? text("正在停止", "Stopping")
        : phase === "failed"
          ? text("启动失败", "Failed")
          : text("未运行", "Not running");
  return (
    <GlassSurface className={`engine-hero phase-${phase}`} aria-label={t("home_engine_control")}>
      <div className="engine-copy">
        <div className="engine-heading">
          <div className="engine-state">
            <span className="section-kicker">{t("home_engine_title")}</span>
            <Badge key={phase} className="engine-state-badge motion-status-swap" appearance="outline">
              <i className="state-dot" />
              {phaseLabel}
            </Badge>
          </div>
        </div>
        <p className="engine-summary">
          {text(`${selectedCount} 张网卡参与调度`, `${selectedCount} NIC(s) selected`)}
          <span aria-hidden="true">·</span>
          {text(`${connections} 个连接`, `${connections} connection(s)`)}
        </p>
        <TabList
          className="mode-tabs"
          selectedValue={mode}
          onTabSelect={(_, data) => onModeChange(data.value as EngineMode)}
          size="small"
          disabled={transitioning || phase === "running" || phase === "degraded"}
        >
          <Tab value="proxy" icon={<PlugConnected20Regular />}>{t("mode_proxy")}</Tab>
          <Tab value="tun" icon={<Navigation20Regular />} disabled={macOS}>
            {macOS ? text("TUN 模式（开发中）", "TUN mode (planned)") : text("TUN 模式", "TUN mode")}
          </Tab>
        </TabList>
        <span key={mode} className="engine-mode-note motion-inline-swap">
          {mode === "proxy"
            ? text(
              `接管遵循${macOS ? " macOS" : " Windows"}系统代理的应用流量 · HTTP ${httpPort} · SOCKS5 ${socksPort}`,
              `Manages apps that follow the ${macOS ? "macOS" : "Windows"} system proxy · HTTP ${httpPort} · SOCKS5 ${socksPort}`,
            )
            : text(
              "启动前执行只读路由、WFP 与权限检查；系统级资源由独立 Go Core 管理",
              "Performs read-only route, WFP, and permission checks before start; system resources are managed by the independent Go Core.",
            )}
        </span>
        <div className="engine-control-row">
          <Button
            className="engine-action"
            appearance="primary"
            icon={transitioning ? <Spinner size="tiny" /> : active ? <Stop20Filled /> : <Play20Filled />}
            disabled={transitioning || (!active && selectedCount === 0)}
            onClick={onToggle}
          >
            <span key={phase} className="engine-action-label motion-inline-swap">{actionLabel}</span>
          </Button>
          <Switch
            className="weighted-switch"
            checked={weighted}
            disabled={transitioning || phase === "running" || phase === "degraded"}
            onChange={(_, data) => onWeightedChange(data.checked)}
            label={weighted ? text("权重调度", "Weighted") : text("轮询调度", "Round-robin")}
          />
        </div>
      </div>
      <ThroughputDisplay download={download} upload={upload} connections={connections} history={history} active={active} />
    </GlassSurface>
  );
}
