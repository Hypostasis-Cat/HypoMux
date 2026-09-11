import { Badge, Button, Switch } from "@fluentui/react-components";
import { ArrowLeft20Regular, ArrowRight20Regular, Games24Regular } from "@fluentui/react-icons";
import { useEffect, useRef, useState } from "react";
import { GlassSurface } from "../components/material/GlassSurface";
import { SteamCDNPanel } from "../components/SteamCDNPanel";
import { useAppNotifications } from "../components/notifications/AppNotifications";
import { useI18n } from "../i18n/i18n";
import { appServices, type SteamCDNStatus } from "../platform/services";

export function ToolsPage() {
  const { locale } = useI18n();
  const text = (zh: string, en: string) => locale === "en" ? en : zh;
  const { notify } = useAppNotifications();
  const [enabled, setEnabled] = useState(false);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [revision, setRevision] = useState(0);
  const busy = useRef(false);
  const [detail, setDetail] = useState(false);
  const [status, setStatus] = useState<SteamCDNStatus>();
  const [statusError, setStatusError] = useState(false);
  const entryRef = useRef<HTMLButtonElement>(null);
  const titleRef = useRef<HTMLHeadingElement>(null);
  const navigated = useRef(false);
  useEffect(() => {
    if (!navigated.current) return;
    if (detail) titleRef.current?.focus(); else entryRef.current?.focus();
  }, [detail]);
  useEffect(() => {
    if (detail || loading || saving) return;
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout>;
    const poll = async () => {
      try {
        const next = await appServices.engine.steamCDNStatus();
        if (!cancelled) { setStatus(next); setStatusError(false); }
      } catch { if (!cancelled) setStatusError(true); }
      finally { if (!cancelled) timer = setTimeout(poll, 5000); }
    };
    void poll();
    return () => { cancelled = true; clearTimeout(timer); };
  }, [detail, loading, saving, enabled, revision]);
  const navigate = (next: boolean) => { navigated.current = true; setDetail(next); };
  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    void appServices.settings.get().then(settings => {
      if (!cancelled) { setEnabled(settings.steam_cdn_enabled ?? false); setError(""); }
    }).catch(reason => { if (!cancelled) setError(String(reason)); })
      .finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; };
  }, [revision]);

  const toggle = async (checked: boolean) => {
    if (busy.current) return;
    busy.current = true;
    setSaving(true);
    try {
      const settings = await appServices.engine.setSteamCDNEnabled(checked);
      setEnabled(settings.steam_cdn_enabled ?? false);
      notify({ title: text("下载优选已更新", "Download optimization updated"), message: text("已保存，作用于新连接。", "Saved; applies to new connections."), intent: "success" });
    } catch (reason) {
      notify({ title: text("下载优选设置失败", "Failed to update download optimization"), message: String(reason), intent: "error" });
      setRevision(value => value + 1);
    } finally { busy.current = false; setSaving(false); }
  };

  const name = text("Steam 下载优选", "Steam download optimization");
  const stateText = loading ? text("正在读取设置", "Loading settings")
    : saving ? text("正在保存", "Saving")
    : error ? text("设置读取失败", "Settings unavailable")
    : !enabled ? text("已关闭", "Off")
    : statusError ? text("已开启 · 状态暂不可用", "Enabled · status unavailable")
    : !status ? text("已开启 · 正在读取状态", "Enabled · checking status")
    : !status.enabled ? text("已开启 · 等待引擎", "Enabled · waiting for engine")
    : status.probing > 0 ? text("正在验证节点", "Checking candidates")
    : status.entries.some(entry => entry.preferred) ? text("正在优选 · 已有优先节点", "Optimizing · preferred nodes available")
    : status.entries.some(entry => (entry.switched_active ?? 0) > 0) ? text("正在优选 · 试用候选", "Optimizing · trying candidates")
    : (status.recognized ?? 0) > 0 ? text("已识别下载 · 沿用原节点", "Download recognized · using original nodes")
    : text("已开启 · 等待下载", "Enabled · waiting for download");
  const control = <Switch aria-label={name} checked={enabled} disabled={loading || saving || !!error} onChange={(_, data) => void toggle(data.checked)} />;
  const loadError = error && <div role="alert" className="tool-load-error"><p>{text("无法读取工具设置：", "Unable to load tool settings: ")}{error}</p><Button onClick={() => setRevision(value => value + 1)}>{text("重试", "Retry")}</Button></div>;

  return <main className="tools-page">
    {detail ? <div key="detail" className="tool-view page-transition-layer is-entering">
      <header className="page-heading"><div>
        <Button appearance="subtle" icon={<ArrowLeft20Regular />} onClick={() => navigate(false)}>{text("返回工具箱", "Back to toolbox")}</Button>
        <h1 ref={titleRef} tabIndex={-1}>{name}</h1>
        <p>{text("查看下载表现、候选节点与诊断。开关作用于新连接。", "Inspect downloads, candidate nodes and diagnostics. Changes apply to new connections.")}</p>
      </div></header>
      <GlassSurface className="tool-card" aria-label={name}>
        <header className="tool-card-heading">
          <span className="tool-icon" aria-hidden="true"><Games24Regular /></span>
          <div className="tool-card-copy"><div className="tool-title"><h2>{text("下载节点优选", "Download node optimization")}</h2><Badge appearance="tint">{text("实验性", "Experimental")}</Badge></div><p>{text("根据实际下载表现，学习适合当前网络的节点。", "Learn suitable nodes from actual download performance.")}</p></div>
          {control}
        </header>
        {loadError || <SteamCDNPanel enabled={enabled} saving={loading || saving} />}
      </GlassSurface>
    </div> : <div key="overview" className={`tool-view${navigated.current ? " page-transition-layer is-entering" : ""}`} data-direction="backward">
      <header className="page-heading"><div>
        <span className="section-kicker">HypoMux / {text("工具", "Tools")}</span>
        <h1>{text("工具箱", "Toolbox")}</h1>
        <p>{text("按需开启，让网络更适合你的应用。", "Optional tools for the apps you use.")}</p>
      </div></header>
      <div className="toolbox-grid">
        <GlassSurface className="toolbox-tile" aria-labelledby="steam-tool-title">
          <button ref={entryRef} className="toolbox-tile-open" aria-label={text("查看 Steam 下载优选详情", "View Steam download optimization details")} onClick={() => navigate(true)}>
            <span className="tool-icon" aria-hidden="true"><Games24Regular /></span>
            <h2 id="steam-tool-title">{name}</h2>
            <span className="toolbox-tile-description">{text("为 Steam 下载寻找更合适的节点。", "Find suitable nodes for your Steam downloads.")}</span>
            <span className="toolbox-tile-footer"><Badge appearance="tint">{text("实验性", "Experimental")}</Badge><span>{text("查看详情", "View details")} <ArrowRight20Regular /></span></span>
          </button>
          <div className="toolbox-tile-switch">{control}</div>
          <div className="toolbox-tile-status" role="status"><span className="toolbox-state-dot" data-active={enabled && !!status?.enabled && !statusError} />{stateText}</div>
          {loadError}
        </GlassSurface>
      </div>
    </div>}
  </main>;
}