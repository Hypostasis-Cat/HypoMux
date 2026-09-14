import { Switch, Tooltip } from "@fluentui/react-components";
import { useEffect, useRef, useState } from "react";
import { useI18n } from "../i18n/i18n";
import { appServices, type HotspotStatus } from "../platform/services";
import { hotspotDraft } from "./hotspotDraft";
import { runHotspotOperation, useHotspotOperation } from "./hotspotOperation";

export function HotspotQuickControl({ onConfigure }: { onConfigure: () => void }) {
  const { locale } = useI18n();
  const text = (zh: string, en: string) => locale === "en" ? en : zh;
  const [status, setStatus] = useState<HotspotStatus>();
  const [localPending, setPending] = useState(false);
  const operation = useHotspotOperation();
  const pending = localPending || !!operation;
  const [error, setError] = useState("");
  const busy = useRef(false);
  const revision = useRef(0);
  const mounted = useRef(false);
  useEffect(() => {
    mounted.current = true;
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout>;
    const refresh = async () => {
      const version = revision.current;
      if (!busy.current) {
        try {
          const next = await appServices.engine.hotspotStatus();
          if (!cancelled && !busy.current && version === revision.current) { setStatus(next); setError(""); }
        } catch { if (!cancelled && !busy.current && version === revision.current) setError(text("热点状态暂不可用", "Hotspot status unavailable")); }
      }
      if (!cancelled) timer = setTimeout(refresh, 3000);
    };
    void refresh();
    return () => { cancelled = true; mounted.current = false; clearTimeout(timer); };
  }, []);
  const active = status?.state === "running" || status?.state === "starting" || status?.state === "stopping";
  const toggle = async (start: boolean) => {
    if (busy.current) return;
    busy.current = true; revision.current++; setPending(true); setError("");
    try {
      await runHotspotOperation(start ? "start" : "stop", async () => {
      let next: HotspotStatus;
      if (start) {
        const config = await appServices.engine.hotspotPreferences();
        if (!config.password || !config.ssid.trim()) { if (mounted.current) onConfigure(); return; }
        // The quick switch uses saved settings. Details must show the same
        // password even when an older unsaved form draft exists in memory.
        hotspotDraft.current = config;
        next = await appServices.engine.startHotspot(config);
      } else next = await appServices.engine.stopHotspot();
      if (mounted.current) setStatus(next);
      });
    } catch (reason) {
      if (mounted.current) setError(String(reason));
      try { const next = await appServices.engine.hotspotStatus(); if (mounted.current) setStatus(next); } catch { /* retain action error */ }
    } finally { busy.current = false; if (mounted.current) setPending(false); }
  };
  const label = pending ? text("正在处理热点…", "Updating hotspot…")
    : error ? text("热点需要检查", "Hotspot needs attention")
    : !status ? text("正在读取热点状态", "Checking hotspot status")
    : status.state === "stopping" ? text("正在关闭热点", "Stopping hotspot")
    : status.state === "starting" ? text("正在开启热点", "Starting hotspot")
    : status.state === "running" ? `${text("已开启", "On")} · ${status.clients} ${text("台设备", "device(s)")}${status.sharing_verified ? "" : text(" · 出口待验证", " · egress unverified")}`
    : status.state === "failed" ? text("热点需要检查", "Hotspot needs attention")
    : !status.ready ? text("请先启动 TUN 聚合", "Start TUN aggregation first") : text("热点已关闭", "Hotspot is off");
  return <>
    <div className="toolbox-tile-switch"><Switch aria-label={text("聚合热点快捷开关", "Aggregation hotspot quick switch")} checked={!!active} disabled={pending || status?.state === "stopping" || !status || (!active && (!status.ready || !!error))} onChange={(_, data) => void toggle(data.checked)} /></div>
    <div className="toolbox-tile-status" role={error ? "alert" : "status"}>
      <span className="toolbox-state-dot" data-active={status?.state === "running" && !error} />
      <Tooltip content={error || label} relationship={error ? "description" : "label"}>
        <span className="toolbox-status-text" tabIndex={0}>{label}</span>
      </Tooltip>
    </div>
  </>;
}
