import { useId } from "react";
import { ArrowUpload20Regular, PlugConnected20Regular } from "@fluentui/react-icons";
import { useI18n } from "../../i18n/i18n";

type ThroughputPoint = { x: number; y: number };

const CHART_TOP = 6;
const CHART_BASELINE = 41;

function niceCeiling(value: number) {
  if (!Number.isFinite(value) || value <= 0) return 1;
  const exponent = 10 ** Math.floor(Math.log10(value));
  const normalized = value / exponent;
  const step = normalized <= 1 ? 1 : normalized <= 2 ? 2 : normalized <= 5 ? 5 : 10;
  return step * exponent;
}

function smoothValues(values: number[]) {
  const sanitized = values.map((value) => Number.isFinite(value) ? Math.max(0, value) : 0);
  return sanitized.map((value, index) => {
    const previous = sanitized[index - 1] ?? value;
    const next = sanitized[index + 1] ?? value;
    return previous * 0.2 + value * 0.6 + next * 0.2;
  });
}

function createPath(points: ThroughputPoint[]) {
  if (points.length < 2) return "";
  let path = `M ${points[0].x.toFixed(2)} ${points[0].y.toFixed(2)}`;
  for (let index = 1; index < points.length; index += 1) {
    const previous = points[index - 1];
    const current = points[index];
    const controlX = (previous.x + current.x) / 2;
    path += ` C ${controlX.toFixed(2)} ${previous.y.toFixed(2)} ${controlX.toFixed(2)} ${current.y.toFixed(2)} ${current.x.toFixed(2)} ${current.y.toFixed(2)}`;
  }
  return path;
}

export function createThroughputChart(values: number[]) {
  const smoothed = smoothValues(values.length >= 2 ? values : [0, ...(values.length === 0 ? [0] : values)]);
  const scale = niceCeiling(Math.max(1, ...smoothed) * 1.12);
  const points = smoothed.map((value, index) => {
    const ratio = Math.min(1, value / scale);
    return {
      x: (index / Math.max(1, smoothed.length - 1)) * 100,
      y: CHART_BASELINE - ratio * (CHART_BASELINE - CHART_TOP),
    };
  });
  const linePath = createPath(points);
  return {
    linePath,
    areaPath: `${linePath} L 100 44 L 0 44 Z`,
  };
}

export function ThroughputDisplay({
  download,
  upload,
  connections,
  history,
  active,
}: {
  download: number;
  upload: number;
  connections: number;
  history: number[];
  active: boolean;
}) {
  const { locale, t } = useI18n();
  const chartID = useId().replace(/:/g, "");
  const { areaPath, linePath } = createThroughputChart(history);
  const fillID = `${chartID}-throughput-fill`;
  const lineID = `${chartID}-throughput-line`;
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
      {active ? (
        <svg className="throughput-graph" viewBox="0 0 100 44" preserveAspectRatio="none" aria-hidden="true">
          <defs>
            <linearGradient id={fillID} x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor="var(--hm-accent)" stopOpacity=".2" />
              <stop offset="58%" stopColor="var(--hm-accent)" stopOpacity=".055" />
              <stop offset="100%" stopColor="var(--hm-accent)" stopOpacity="0" />
            </linearGradient>
            <linearGradient id={lineID} x1="0" y1="0" x2="1" y2="0">
              <stop offset="0%" stopColor="var(--hm-accent)" stopOpacity=".16" />
              <stop offset="55%" stopColor="var(--hm-accent)" stopOpacity=".62" />
              <stop offset="100%" stopColor="var(--hm-accent-strong)" stopOpacity=".96" />
            </linearGradient>
          </defs>
          <g className="throughput-grid">
            <line x1="0" x2="100" y1="13" y2="13" />
            <line x1="0" x2="100" y1="27" y2="27" />
            <line x1="0" x2="100" y1="41" y2="41" />
          </g>
          <path className="throughput-fill" d={areaPath} fill={`url(#${fillID})`} />
          <path className="throughput-line-glow" d={linePath} />
          <path className="throughput-line" d={linePath} stroke={`url(#${lineID})`} />
        </svg>
      ) : (
        <div className="throughput-idle">
          <span>{locale === "en" ? "Live throughput appears after aggregation starts" : "启动聚合后显示实时吞吐趋势"}</span>
        </div>
      )}
    </div>
  );
}
