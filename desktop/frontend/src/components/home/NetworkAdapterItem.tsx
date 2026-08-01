import { useEffect, useState } from "react";
import { Button, Checkbox, Input, Tooltip } from "@fluentui/react-components";
import {
  Add16Regular,
  ArrowDownload20Regular,
  ArrowUpload20Regular,
  PlugConnected20Regular,
  Router24Regular,
  Subtract16Regular,
  Wifi124Regular,
} from "@fluentui/react-icons";
import type { HomeAdapter } from "../../state/useEngineState";
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
  disabled,
  onSelectedChange,
  onWeightChange,
}: {
  adapter: HomeAdapter;
  percentage: number;
  disabled: boolean;
  onSelectedChange: (checked: boolean) => void;
  onWeightChange: (value: number) => void;
}) {
  const { locale, t } = useI18n();
  const text = (zh: string, en: string) => locale === "en" ? en : zh;
  const [inputValue, setInputValue] = useState(String(adapter.weight));
  useEffect(() => setInputValue(String(adapter.weight)), [adapter.weight]);
  const commit = (next: number) => {
    const normalized = Math.max(1, Math.min(100, Math.round(next)));
    setInputValue(String(normalized));
    onWeightChange(normalized);
  };
  return (
    <article className={`network-adapter hm-card${adapter.selected ? " is-selected" : " is-muted"}`}>
      <div className="adapter-primary">
        <Checkbox
          checked={adapter.selected}
          disabled={disabled}
          onChange={(_, data) => onSelectedChange(data.checked === true)}
          aria-label={`${adapter.selected ? text("停用", "Disable") : text("启用", "Enable")} ${adapter.name}`}
        />
        <span className="adapter-icon" aria-hidden="true">
          {adapter.kind === "wifi" ? <Wifi124Regular /> : <Router24Regular />}
        </span>
        <div className="adapter-name">
          <strong>{adapter.name}</strong>
          <span>{adapter.address}</span>
        </div>
      </div>

      <div className="adapter-live" aria-label={text("实时网络状态", "Live network status")}>
        <span><ArrowDownload20Regular /><strong>{formatRate(adapter.downloadBPS)}</strong><small>{text("下载", "Down")}</small></span>
        <span><ArrowUpload20Regular /><strong>{formatRate(adapter.uploadBPS)}</strong><small>{text("上传", "Up")}</small></span>
        <span><PlugConnected20Regular /><strong>{adapter.connections}</strong><small>{text("连接", "Conn")}</small></span>
      </div>

      <div className="adapter-quality">
        <NetworkHealthBadge health={adapter.health} />
        <span>{text("延迟", "Latency")} {adapter.latencyMS === undefined ? "—" : `${adapter.latencyMS} ms`}</span>
        <span>{text("丢包", "Loss")} {adapter.lossRate === undefined ? "—" : `${adapter.lossRate}%`}</span>
      </div>

      <div className="adapter-weight">
        <div>
          <span>{t("home_bw_column")}</span>
          <strong>{percentage}% {text("份额", "share")}</strong>
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
      </div>
    </article>
  );
}
