import { Badge } from "@fluentui/react-components";
import { CheckmarkCircle16Filled, Clock16Regular, ErrorCircle16Filled, Warning16Filled } from "@fluentui/react-icons";
import type { AdapterHealth } from "../../state/useEngineState";
import { useI18n } from "../../i18n/i18n";

export function NetworkHealthBadge({ health }: { health: AdapterHealth }) {
  const { locale } = useI18n();
  const labels: Record<AdapterHealth, string> = locale === "en" ? {
    idle: "Not checked",
    healthy: "Available",
    unstable: "Unstable",
    cooldown: "Cooldown",
    probing: "Probing",
    failed: "Unavailable",
  } : {
    idle: "未体检",
    healthy: "可用",
    unstable: "不稳定",
    cooldown: "冷却中",
    probing: "探测中",
    failed: "不可用",
  };
  const icon = health === "failed"
    ? <ErrorCircle16Filled />
    : health === "cooldown" || health === "unstable"
      ? <Warning16Filled />
      : health === "probing" || health === "idle"
        ? <Clock16Regular />
        : <CheckmarkCircle16Filled />;
  return (
    <Badge
      className={`health-badge health-${health}`}
      appearance="tint"
      icon={icon}
    >
      {labels[health]}
    </Badge>
  );
}
