import { ArrowUpload20Regular, PlugConnected20Regular } from "@fluentui/react-icons";
import { useI18n } from "../../i18n/i18n";

function createPath(values: number[]) {
  const max = Math.max(1, ...values);
  const points = values.map((value, index) => {
      const x = (index / Math.max(1, values.length - 1)) * 100;
      const y = 42 - (value / max) * 36;
      return { x, y };
    });
  if (points.length < 2) return "";

  let path = `M ${points[0].x.toFixed(2)} ${points[0].y.toFixed(2)}`;
  for (let index = 1; index < points.length - 1; index += 1) {
    const next = points[index + 1];
    const midpointX = (points[index].x + next.x) / 2;
    const midpointY = (points[index].y + next.y) / 2;
    path += ` Q ${points[index].x.toFixed(2)} ${points[index].y.toFixed(2)} ${midpointX.toFixed(2)} ${midpointY.toFixed(2)}`;
  }
  const last = points[points.length - 1];
  path += ` T ${last.x.toFixed(2)} ${last.y.toFixed(2)}`;
  return path;
}

export function ThroughputDisplay({
  download,
  upload,
  connections,
  history,
}: {
  download: number;
  upload: number;
  connections: number;
  history: number[];
}) {
  const { locale, t } = useI18n();
  const path = createPath(history);
  return (
    <div className="throughput-display">
      <div className="throughput-label">{t("home_total_speed")}</div>
      <div className="throughput-value">
        <strong>{download.toFixed(1)}</strong>
        <span>MB/s</span>
      </div>
      <div className="throughput-meta">
        <span><ArrowUpload20Regular /> {upload.toFixed(1)} MB/s</span>
        <span><PlugConnected20Regular /> {locale === "en" ? `${connections} active connections` : `${connections} 个活动连接`}</span>
      </div>
      <svg className="throughput-graph" viewBox="0 0 100 44" preserveAspectRatio="none" aria-hidden="true">
        <defs>
          <linearGradient id="throughputFill" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor="var(--hm-accent)" stopOpacity=".28" />
            <stop offset="100%" stopColor="var(--hm-accent)" stopOpacity="0" />
          </linearGradient>
        </defs>
        <path className="throughput-fill" d={`${path} L 100 44 L 0 44 Z`} />
        <path className="throughput-line" d={path} />
      </svg>
    </div>
  );
}
