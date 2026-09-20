import { useEffect, useState } from "react";
import { Button, Checkbox, Input, Tooltip } from "@fluentui/react-components";
import {
  Add16Regular,
  Warning16Regular,
  ArrowDownload20Regular,
  ArrowUpload20Regular,
  PlugConnected20Regular,
  Router24Regular,
  Subtract16Regular,
  Wifi124Regular,
} from "@fluentui/react-icons";
import type { AdapterFeedback, HomeAdapter } from "../../state/useEngineState";
import { NetworkHealthBadge } from "./NetworkHealthBadge";
import { useI18n } from "../../i18n/i18n";

const formatRate = (bytesPerSecond: number) => {
  if (bytesPerSecond >= 1024 * 1024) return `${(bytesPerSecond / (1024 * 1024)).toFixed(1)} MB/s`;
  if (bytesPerSecond >= 1024) return `${(bytesPerSecond / 1024).toFixed(0)} KB/s`;
  return `${Math.round(bytesPerSecond)} B/s`;
};

export function NetworkAdapterItem({
  adapter,
  percentage,
  weighted,
  disabled,
  aggregating = false,
  feedback,
  feedbackToken,
  onOpenConnections,
  onSelectedChange,
  onWeightChange,
}: {
  adapter: HomeAdapter;
  percentage: number;
  weighted: boolean;
  disabled: boolean;
  aggregating?: boolean;
  feedback?: AdapterFeedback["status"];
  feedbackToken?: AdapterFeedback;
  onOpenConnections: () => void;
  onSelectedChange: (checked: boolean) => void;
  onWeightChange: (value: number) => void;
}) {
  const { locale, t } = useI18n();
  const text = (zh: string, en: string) => locale === "en" ? en : zh;
  const [inputValue, setInputValue] = useState(String(adapter.weight));
  useEffect(() => setInputValue(String(adapter.weight)), [adapter.weight]);
  const [resultSignal, setResultSignal] = useState<{ status?: "success" | "error"; sequence: number }>({ sequence: 0 });
  useEffect(() => {
    setResultSignal((previous) => ({
      status: feedback === "success" || feedback === "error" ? feedback : undefined,
      sequence: previous.sequence + 1,
    }));
    if (feedback !== "success") return;
    const timeout = window.setTimeout(() => setResultSignal((previous) => ({ ...previous, status: undefined })), 1800);
    return () => window.clearTimeout(timeout);
  }, [feedback, feedbackToken]);
  const visualFeedback = feedback === "pending" ? "pending" : resultSignal.status;
  const feedbackLabel = feedback === "pending"
    ? text("正在应用网卡变更", "Applying adapter changes")
    : feedback === "error"
      ? text("网卡变更失败，请重试", "Adapter changes failed; retry")
      : text(aggregating ? "网卡变更已应用于新连接" : "网卡选择已保存", aggregating ? "Adapter changes applied to new connections" : "Adapter selection saved");
  const toggleSelection = () => {
    if (!disabled) onSelectedChange(!adapter.selected);
  };
  const commit = (next: number) => {
    const normalized = Math.max(1, Math.min(100, Math.round(next)));
    setInputValue(String(normalized));
    onWeightChange(normalized);
  };
  return (
    <article
      className={`network-adapter hm-card${adapter.selected ? " is-selected" : " is-muted"}${disabled ? " is-selection-disabled" : " is-selectable"}${visualFeedback ? ` has-${visualFeedback}` : ""}`}
      aria-busy={feedback === "pending"}
      onClick={toggleSelection}
    >
      {visualFeedback && <span key={resultSignal.sequence} className={`adapter-operation-glow is-${visualFeedback}`} aria-hidden="true" />}
      <span className="visually-hidden" role="status" aria-live="polite" aria-atomic="true">{feedback ? `${adapter.name}: ${feedbackLabel}` : ""}</span>
      <div className="adapter-primary">
        <span className="adapter-selection-control" onClick={(event) => event.stopPropagation()}>
          <Checkbox
            checked={adapter.selected}
            disabled={disabled}
            onChange={(_, data) => onSelectedChange(data.checked === true)}
            aria-label={`${adapter.selected ? text("停用", "Disable") : text("启用", "Enable")} ${adapter.name}`}
          />
        </span>
        <Tooltip content={feedback ? feedbackLabel : text("网络适配器", "Network adapter")} relationship="description">
          <span className="adapter-icon">
            <span className="adapter-device-icon" aria-hidden="true">{adapter.kind === "wifi" ? <Wifi124Regular /> : <Router24Regular />}</span>
            {visualFeedback && (
              <span key={resultSignal.sequence} className={`adapter-operation-icon is-${visualFeedback}`} aria-hidden="true">
                {visualFeedback === "pending" ? <span className="adapter-operation-ring" />
                  : visualFeedback === "error" ? <Warning16Regular />
                    : <svg viewBox="0 0 24 24" fill="none"><path d="m5 12 4 4 10-10" /></svg>}
              </span>
            )}
          </span>
        </Tooltip>
        <span className="adapter-name">
          <strong title={adapter.name}>{adapter.name}</strong>
          <span>{adapter.address}</span>
        </span>
      </div>

      <Tooltip
        content={text(`查看 ${adapter.name} 的活动连接`, `View active connections for ${adapter.name}`)}
        relationship="description"
      >
        <button
          type="button"
          className="adapter-live"
          aria-label={text(`查看 ${adapter.name} 的活动连接`, `View active connections for ${adapter.name}`)}
          onClick={(event) => {
            event.stopPropagation();
            onOpenConnections();
          }}
        >
          <span><ArrowDownload20Regular /><strong>{formatRate(adapter.downloadBPS)}</strong><small>{text("下载", "Down")}</small></span>
          <span><ArrowUpload20Regular /><strong>{formatRate(adapter.uploadBPS)}</strong><small>{text("上传", "Up")}</small></span>
          <span><PlugConnected20Regular /><strong>{adapter.connections}</strong><small>{text("连接", "Conn")}</small></span>
        </button>
      </Tooltip>

      <div className="adapter-quality">
        <NetworkHealthBadge health={adapter.health} />
        <span>{text("延迟", "Latency")} {adapter.latencyMS === undefined ? "—" : `${adapter.latencyMS} ms`}</span>
        <span>{text("丢包", "Loss")} {adapter.lossRate === undefined ? "—" : `${adapter.lossRate}%`}</span>
      </div>

      <div className={`adapter-weight${weighted ? "" : " is-automatic"}`} onClick={(event) => event.stopPropagation()}>
        {weighted ? <>
        <div>
          <span>{t("home_bw_column")}</span>
          <strong>{percentage}% {text("新连接占比", "of new connections")}</strong>
        </div>
        <Tooltip content={`${t("home_bw_column_hint")}${text("（可用 ↑↓ 键调整）", " (Use ↑↓ keys to adjust)")}`} relationship="description">
          <Input
            className="weight-stepper"
            appearance="outline"
            size="small"
            value={inputValue}
            inputMode="numeric"
            disabled={!adapter.selected || disabled}
            aria-label={`${adapter.name} ${t("home_bw_column")}`}
            contentBefore={(
              <Button
                appearance="subtle"
                size="small"
                icon={<Subtract16Regular />}
                aria-label={`${text("降低", "Decrease")} ${adapter.name} ${t("home_bw_column")}`}
                disabled={!adapter.selected || disabled || adapter.weight <= 1}
                onClick={() => commit(adapter.weight - 1)}
              />
            )}
            contentAfter={(
              <Button
                appearance="subtle"
                size="small"
                icon={<Add16Regular />}
                aria-label={`${text("提高", "Increase")} ${adapter.name} ${t("home_bw_column")}`}
                disabled={!adapter.selected || disabled || adapter.weight >= 100}
                onClick={() => commit(adapter.weight + 1)}
              />
            )}
            onWheel={(event) => event.currentTarget.querySelector("input")?.blur()}
            onChange={(_, data) => {
              if (!/^\d{0,3}$/.test(data.value)) return;
              setInputValue(data.value);
              const value = Number(data.value);
              if (value >= 1 && value <= 100) onWeightChange(value);
            }}
            onBlur={() => commit(Number(inputValue) || adapter.weight)}
            onKeyDown={(event) => {
              if (event.key === "ArrowUp") {
                event.preventDefault();
                commit(adapter.weight + 1);
              } else if (event.key === "ArrowDown") {
                event.preventDefault();
                commit(adapter.weight - 1);
              }
            }}
          />
        </Tooltip>
        </> : <div>
          <span>{text("自动分配连接", "Automatic connection distribution")}</span>
          <span>{text("无需设置权重", "No weights needed")}</span>
        </div>}
      </div>
    </article>
  );
}
