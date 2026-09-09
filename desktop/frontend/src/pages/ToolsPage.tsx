import { Badge, Button, Switch } from "@fluentui/react-components";
import { Games24Regular } from "@fluentui/react-icons";
import { useEffect, useRef, useState } from "react";
import { GlassSurface } from "../components/material/GlassSurface";
import { SteamCDNPanel } from "../components/SteamCDNPanel";
import { useAppNotifications } from "../components/notifications/AppNotifications";
import { useI18n } from "../i18n/i18n";
import { appServices } from "../platform/services";

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

  return <main className="tools-page">
    <header className="page-heading"><div>
      <span className="section-kicker">HypoMux / {text("工具", "Tools")}</span>
      <h1>{text("工具箱", "Toolbox")}</h1>
      <p>{text("按需开启，让网络更适合你的应用。", "Optional tools for the apps you use.")}</p>
    </div></header>
    <GlassSurface className="tool-card" aria-labelledby="steam-tool-title">
      <header className="tool-card-heading">
        <span className="tool-icon" aria-hidden="true"><Games24Regular /></span>
        <div className="tool-card-copy">
          <div className="tool-title"><h2 id="steam-tool-title">{text("Steam 下载优选", "Steam download optimization")}</h2><Badge appearance="tint">{text("实验性", "Experimental")}</Badge></div>
          <p id="steam-tool-hint">{text("从真实下载中寻找更快的节点，支持系统代理与 TUN。可随时关闭，开关作用于新连接。", "Learns faster nodes from real downloads in proxy and TUN modes. Toggle any time; changes apply to new connections.")}</p>
        </div>
        <Switch aria-labelledby="steam-tool-title" aria-describedby="steam-tool-hint" checked={enabled} disabled={loading || saving || !!error} onChange={(_, data) => void toggle(data.checked)} />
      </header>
      {error ? <div role="alert" className="tool-load-error"><p>{text("无法读取工具设置：", "Unable to load tool settings: ")}{error}</p><Button onClick={() => setRevision(value => value + 1)}>{text("重试", "Retry")}</Button></div>
        : <SteamCDNPanel enabled={enabled} saving={loading || saving} />}
    </GlassSurface>
  </main>;
}
