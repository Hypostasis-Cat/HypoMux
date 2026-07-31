import { Button } from "@fluentui/react-components";
import {
  AppsListDetail20Regular,
  ArrowRouting20Regular,
  DataUsage20Regular,
  PlugConnected20Regular,
  ShieldCheckmark20Regular,
} from "@fluentui/react-icons";
import type { EnginePhase } from "../../state/useEngineState";
import { GlassSurface } from "../material/GlassSurface";
import { useI18n } from "../../i18n/i18n";

const StatusItem = ({
  icon,
  label,
  value,
}: {
  icon: React.ReactNode;
  label: string;
  value: React.ReactNode;
}) => (
  <div className="runtime-item">
    <span className="runtime-icon">{icon}</span>
    <span>
      <small>{label}</small>
      <strong>{value}</strong>
    </span>
  </div>
);

export function RuntimeStatusBar({
  phase,
  connections,
  sessionTraffic,
  weighted,
  coreVersion,
  preview,
  onOpenConnections,
}: {
  phase: EnginePhase;
  connections: number;
  sessionTraffic: string;
  weighted: boolean;
  coreVersion: string;
  preview: boolean;
  onOpenConnections?: () => void;
}) {
  const { locale, t } = useI18n();
  const text = (zh: string, en: string) => locale === "en" ? en : zh;
  const phaseLabel = phase === "running"
    ? text("运行中", "Running")
    : phase === "starting"
      ? text("正在启动", "Starting")
      : phase === "stopping"
        ? text("正在停止", "Stopping")
        : phase === "failed"
          ? text("失败", "Failed")
          : text("待命", "Idle");
  return (
    <GlassSurface as="footer" tone="secondary" className="runtime-status" aria-label={text("会话运行状态", "Session runtime status")}>
      <StatusItem
        icon={<ShieldCheckmark20Regular />}
        label={text("核心状态", "Core status")}
        value={preview ? text("浏览器容量预览", "Browser capacity preview") : `${phaseLabel} · ${coreVersion}`}
      />
      <StatusItem icon={<PlugConnected20Regular />} label={t("home_metric_connections")} value={connections} />
      <StatusItem icon={<DataUsage20Regular />} label={text("会话流量", "Session traffic")} value={sessionTraffic} />
      <StatusItem icon={<ArrowRouting20Regular />} label={text("调度策略", "Scheduling")} value={weighted ? text("动态加权", "Weighted") : text("轮询", "Round-robin")} />
      <Button appearance="primary" size="small" icon={<AppsListDetail20Regular />} onClick={onOpenConnections}>
        {text("活动连接", "Connections")}
      </Button>
    </GlassSurface>
  );
}
