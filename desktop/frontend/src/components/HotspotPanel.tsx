import { Badge, Button, Field, Input, Select, Spinner } from "@fluentui/react-components";
import { useEffect, useRef, useState } from "react";
import { useI18n } from "../i18n/i18n";
import { appServices, type HotspotConfig, type HotspotStatus } from "../platform/services";

export function HotspotPanel() {
  const { locale } = useI18n();
  const text = (zh: string, en: string) => locale === "en" ? en : zh;
  const [config, setConfig] = useState<HotspotConfig>({ ssid: "HypoMux", password: "", band: "auto" });
  const [status, setStatus] = useState<HotspotStatus>();
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const [pollError, setPollError] = useState("");
  const [showPassword, setShowPassword] = useState(false);
  const busy = useRef(false);
  const revision = useRef(0);
  const mounted = useRef(false);
  useEffect(() => {
    mounted.current = true;
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout>;
    const refresh = async () => {
      if (!busy.current) {
        const requestRevision = revision.current;
        try {
          const next = await appServices.engine.hotspotStatus();
          if (!cancelled && !busy.current && requestRevision === revision.current) { setStatus(next); setPollError(""); }
        } catch (reason) {
          if (!cancelled && !busy.current && requestRevision === revision.current) {
            const detail = String(reason);
            setPollError(detail === "Error" ? text("无法连接热点服务，请在桌面客户端中重试。", "Cannot connect to the hotspot service. Retry in the desktop app.") : detail);
          }
        }
      }
      if (!cancelled) timer = setTimeout(refresh, 3000);
    };
    void refresh();
    return () => { cancelled = true; mounted.current = false; clearTimeout(timer); };
  }, []);
  const active = status?.state === "running" || status?.state === "starting";
  const validName = config.ssid.trim().length > 0 && new TextEncoder().encode(config.ssid).length <= 32 && !/[\0\r\n]/.test(config.ssid);
  const validPassword = /^[\x20-\x7e]{8,63}$/.test(config.password);
  const change = async (start: boolean) => {
    if (busy.current) return;
    revision.current++;
    busy.current = true; setPending(true); setError("");
    try {
      const next = start ? await appServices.engine.startHotspot(config) : await appServices.engine.stopHotspot();
      if (mounted.current) { setStatus(next); setPollError(""); if (!start) setConfig(value => ({ ...value, password: "" })); }
    } catch (reason) {
      if (mounted.current) setError(String(reason));
      try {
        const next = await appServices.engine.hotspotStatus();
        if (mounted.current) setStatus(next);
      } catch { /* Preserve the action error; the next poll retries status. */ }
    } finally { busy.current = false; if (mounted.current) setPending(false); }
  };
  const stateText = pending ? text("正在处理…", "Working…")
    : pollError ? text("状态暂不可用", "Status unavailable")
    : !status ? text("正在读取状态", "Checking status")
    : status.state === "running" ? text("热点已开启", "Hotspot is on")
    : status.state === "starting" ? text("正在启动", "Starting")
    : status.state === "failed" ? text("热点需要检查", "Hotspot needs attention")
    : text("热点已关闭", "Hotspot is off");
  return <section className="hotspot-panel" aria-label={text("聚合热点设置", "Aggregation hotspot settings")}>
    <div className="hotspot-summary" role="status">
      <Badge appearance="tint" color={status?.state === "running" && !pollError ? "success" : "informative"}>{stateText}</Badge>
      {pending && <Spinner size="tiny" />}
      {status?.state === "running" && !pollError && <span>{status.ssid} · {text("已连接设备", "Connected devices")}: {status.clients}</span>}
    </div>
    {status && !status.ready && !active && <p>{text("请先在首页选择网卡，以 TUN 模式启动聚合，然后在这里开启热点。", "Select your adapters and start aggregation in TUN mode on Home, then enable the hotspot here.")}</p>}
    <div className="hotspot-fields">
      <Field label={text("热点名称", "Network name")} hint={text("最多 32 个 UTF-8 字节", "Up to 32 UTF-8 bytes")}>
        <Input value={active ? status?.ssid : config.ssid} disabled={pending || active} onChange={(_, data) => setConfig(value => ({ ...value, ssid: data.value }))} />
      </Field>
      <Field label={text("热点密码", "Network password")} hint={text("8–63 个英文、数字或符号；手机连接时使用此密码。", "8–63 printable ASCII characters. Use this password when connecting your phone.")}>
        <Input type={showPassword ? "text" : "password"} autoComplete="new-password" value={config.password} disabled={pending || active}
          onChange={(_, data) => setConfig(value => ({ ...value, password: data.value }))}
          contentAfter={<Button size="small" appearance="transparent" aria-pressed={showPassword} onClick={() => setShowPassword(value => !value)}>{showPassword ? text("隐藏", "Hide") : text("显示", "Show")}</Button>} />
      </Field>
      <Field label={text("Wi-Fi 频段", "Wi-Fi band")}>
        <Select value={active ? status?.band : config.band} disabled={pending || active} onChange={(_, data) => setConfig(value => ({ ...value, band: data.value as HotspotConfig["band"] }))}>
          <option value="auto">{text("自动", "Automatic")}</option><option value="5">5 GHz</option><option value="2.4">2.4 GHz</option>
        </Select>
      </Field>
    </div>
    <div className="hotspot-actions">
      <Button appearance="primary" disabled={pending || !!pollError || !status?.ready || active || !validName || !validPassword} onClick={() => void change(true)}>{text("开启聚合热点", "Enable aggregation hotspot")}</Button>
      <Button disabled={pending || !status || (!active && status.state !== "failed" && !pollError)} onClick={() => void change(false)}>{text("关闭热点", "Turn off hotspot")}</Button>
    </div>
    {(error || pollError || status?.message) && <p className="hotspot-error" role="alert">{error || pollError || status?.message}</p>}
    {status?.diagnostics && <details className="hotspot-error"><summary>{text("共享诊断", "Sharing diagnostics")}</summary><p>{status.diagnostics}</p></details>}
    <div className="hotspot-help">
      <p>{text("手机连接上面的 Wi-Fi 即可，无需安装客户端或设置代理。停止聚合或退出 HypoMux 时，热点会自动关闭。", "Connect your phone to this Wi-Fi network. No client or proxy settings are needed. The hotspot closes when aggregation stops or HypoMux exits.")}</p>
      <p>{text("沿用当前聚合线路和分流规则，多连接可利用多条线路，单连接速度不保证叠加。首次使用请在手机上验证网页、视频和下载。", "Uses your current aggregation links and routing rules. Multiple connections can use multiple links; single-connection bonding is not guaranteed. Verify browsing, video and downloads on your phone on first use.")}</p>
      {status?.sharing_verified && !pollError && <p>{text("共享出口已校验", "Shared egress verified")}: {status.shared_adapter}</p>}
    </div>
  </section>;
}
