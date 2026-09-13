import { Badge, Button, Field, Input, Select, Spinner, Switch } from "@fluentui/react-components";
import { useEffect, useRef, useState } from "react";
import { useI18n } from "../i18n/i18n";
import { appServices, type HotspotConfig, type HotspotStatus } from "../platform/services";
import { hotspotDraft } from "./hotspotDraft";

export function HotspotPanel() {
  const { locale } = useI18n();
  const text = (zh: string, en: string) => locale === "en" ? en : zh;
  const [config, updateConfig] = useState<HotspotConfig>(() => hotspotDraft.current ?? { ssid: "HypoMux", password: "", band: "auto" });
  const [loadingConfig, setLoadingConfig] = useState(!hotspotDraft.current);
  const setConfig = (update: (value: HotspotConfig) => HotspotConfig) => {
    const next = update(hotspotDraft.current ?? config);
    hotspotDraft.current = next;
    updateConfig(next);
  };
  const [status, setStatus] = useState<HotspotStatus>();
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const [pollError, setPollError] = useState("");
  const [notice, setNotice] = useState("");
  const [action, setAction] = useState<"start" | "stop" | "save">();
  const [showPassword, setShowPassword] = useState(false);
  const busy = useRef(false);
  const revision = useRef(0);
  const mounted = useRef(false);
  useEffect(() => {
    mounted.current = true;
    let cancelled = false;
    if (!hotspotDraft.current) {
      void appServices.engine.hotspotPreferences().then(saved => {
        if (!cancelled && !hotspotDraft.current) {
          hotspotDraft.current = saved;
          updateConfig(saved);
        }
      }).catch(reason => {
        if (!cancelled) setError(String(reason));
      }).finally(() => { if (!cancelled) setLoadingConfig(false); });
    }
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
  const save = async () => {
    if (busy.current) return;
    busy.current = true; revision.current++; setPending(true); setAction("save"); setError(""); setNotice("");
    try {
      await appServices.engine.saveHotspotPreferences(config);
      if (mounted.current) setNotice(text("设置已加密保存，下次打开软件自动恢复。", "Settings saved encrypted and restored next time you open the app."));
    } catch (reason) { if (mounted.current) setError(String(reason)); }
    finally { busy.current = false; if (mounted.current) { setPending(false); setAction(undefined); } }
  };
  const copy = async (value: string) => {
    try {
      await navigator.clipboard.writeText(value);
      if (mounted.current) setNotice(text("已复制", "Copied"));
    } catch { if (mounted.current) setError(text("复制失败，请手动选择并复制。", "Copy failed. Select and copy the text manually.")); }
  };
  const generatePassword = () => {
    const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789-_";
    let password = "";
    while (password.length < 16) {
      const value = crypto.getRandomValues(new Uint8Array(1))[0];
      if (value < Math.floor(256 / alphabet.length) * alphabet.length) password += alphabet[value % alphabet.length];
    }
    setConfig(value => ({ ...value, password }));
    setNotice(text("新密码尚未保存。保存设置或开启热点后生效。", "New password is not saved yet. Save settings or enable the hotspot to apply it."));
  };
  const change = async (start: boolean) => {
    if (busy.current) return;
    revision.current++;
    busy.current = true; setPending(true); setAction(start ? "start" : "stop"); setError(""); setNotice("");
    try {
      const next = start ? await appServices.engine.startHotspot(config) : await appServices.engine.stopHotspot();
      if (mounted.current) { setStatus(next); setPollError(""); }
    } catch (reason) {
      if (mounted.current) setError(String(reason));
      try {
        const next = await appServices.engine.hotspotStatus();
        if (mounted.current) setStatus(next);
      } catch { /* Preserve the action error; the next poll retries status. */ }
    } finally { busy.current = false; if (mounted.current) { setPending(false); setAction(undefined); } }
  };
  const stateText = pending ? (action === "start" ? text("正在开启热点…", "Starting hotspot…") : action === "stop" ? text("正在关闭热点…", "Stopping hotspot…") : text("正在保存设置…", "Saving settings…"))
    : pollError ? text("状态暂不可用", "Status unavailable")
    : !status ? text("正在读取状态", "Checking status")
    : status.state === "running" ? (status.sharing_verified ? text("热点已开启", "Hotspot is on") : text("热点已开启 · 出口待验证", "Hotspot is on · egress unverified"))
    : status.state === "starting" ? text("正在启动", "Starting")
    : status.state === "failed" ? text("热点需要检查", "Hotspot needs attention")
    : text("热点已关闭", "Hotspot is off");
  return <section className="hotspot-panel" aria-label={text("聚合热点设置", "Aggregation hotspot settings")}>
    <div className="hotspot-control">
      <div><strong>{text("共享聚合网络", "Share aggregation network")}</strong><p>{text("手机连接 Wi-Fi，即可使用电脑的聚合网络。", "Connect your phone over Wi-Fi to use the aggregated network.")}</p></div>
      <Switch label={text("聚合热点", "Aggregation hotspot")} checked={!!active}
        disabled={pending || !status || (!active && (loadingConfig || !!pollError || !status.ready || !validName || !validPassword))}
        onChange={(_, data) => void change(data.checked)} />
    </div>
    <div className="hotspot-summary" role="status">
      <Badge appearance="tint" color={status?.state === "running" && status.sharing_verified && !pollError ? "success" : "informative"}>{stateText}</Badge>
      {pending && <Spinner size="tiny" />}
      {status?.state === "running" && !pollError && <span>{status.ssid} · {text("已连接设备", "Connected devices")}: {status.clients}</span>}
    </div>
    {status && !status.ready && !active && <p>{text("请先在首页选择网卡，以 TUN 模式启动聚合，然后在这里开启热点。", "Select your adapters and start aggregation in TUN mode on Home, then enable the hotspot here.")}</p>}
    <div className="hotspot-fields">
      <Field label={text("热点名称", "Network name")} validationState={!active && !validName ? "error" : "none"} validationMessage={!active && !validName ? text("请输入 1–32 个 UTF-8 字节，不能包含换行。", "Enter 1–32 UTF-8 bytes without line breaks.") : undefined} hint={text("最多 32 个 UTF-8 字节", "Up to 32 UTF-8 bytes")}>
        <Input value={active ? status?.ssid : config.ssid} disabled={loadingConfig || pending || active} onChange={(_, data) => setConfig(value => ({ ...value, ssid: data.value }))} />
      </Field>
      <Field label={text("热点密码", "Network password")} hint={text("8–63 个英文、数字或符号；手机连接时使用此密码。", "8–63 printable ASCII characters. Use this password when connecting your phone.")}>
        <Input type={showPassword ? "text" : "password"} autoComplete="new-password" value={config.password} disabled={loadingConfig || pending || active}
          onChange={(_, data) => setConfig(value => ({ ...value, password: data.value }))}
          contentAfter={<Button size="small" appearance="transparent" aria-pressed={showPassword} onClick={() => setShowPassword(value => !value)}>{showPassword ? text("隐藏", "Hide") : text("显示", "Show")}</Button>} />
      </Field>
      <Field label={text("Wi-Fi 频段", "Wi-Fi band")}>
        <Select value={active ? status?.band : config.band} disabled={loadingConfig || pending || active} onChange={(_, data) => setConfig(value => ({ ...value, band: data.value as HotspotConfig["band"] }))}>
          <option value="auto">{text("自动", "Automatic")}</option><option value="5">5 GHz</option><option value="2.4">2.4 GHz</option>
        </Select>
      </Field>
    </div>
    <div className="hotspot-actions">
      {!active && <><Button disabled={loadingConfig || pending || !validName || !validPassword} onClick={() => void save()}>{text("保存设置", "Save settings")}</Button><Button disabled={loadingConfig || pending} onClick={generatePassword}>{text("生成密码", "Generate password")}</Button></>}
      <Button disabled={loadingConfig || !validPassword} onClick={() => void copy(config.password)}>{text("复制密码", "Copy password")}</Button>
    </div>
    <p className="hotspot-save-hint">{active ? text("修改名称、密码或频段前，请先关闭热点。", "Turn off the hotspot before editing its name, password or band.") : text("可提前保存设置；开启时也会自动加密保存。", "You can save settings in advance. Enabling also saves them encrypted.")}</p>
    {notice && <p role="status">{notice}</p>}
    {status?.state === "failed" && !status.cleanup_complete && <Button disabled={pending} onClick={() => void change(false)}>{text("重试关闭热点", "Retry hotspot cleanup")}</Button>}
    {(error || pollError || status?.message) && <p className="hotspot-error" role={error || pollError || status?.state === "failed" ? "alert" : "status"}>{error || pollError || status?.message}</p>}
    {active && !pollError && <div className="hotspot-devices">
      <strong>{text("连接设备", "Connected devices")}</strong>
      {!status?.devices_available ? <p>{text("Windows 暂未提供设备明细，不影响热点使用。", "Windows device details are unavailable. The hotspot can still be used.")}</p>
        : !status.devices?.length ? <p>{text("等待设备连接，请在手机 WLAN 设置中选择上面的热点名称。", "Waiting for devices. Select the network above in your phone’s Wi-Fi settings.")}</p>
        : <ul>{status.devices.map((device, index) => <li key={`${device.mac}-${index}`}><span>{device.hosts?.join(" · ") || text("未命名设备", "Unnamed device")}</span><code>{device.mac}</code></li>)}</ul>}
    </div>}
    {status?.diagnostics && <details className="hotspot-error"><summary>{text("共享诊断", "Sharing diagnostics")}</summary>
      {status.updated_at && <p>{text("状态采样时间", "Status sampled at")}: {new Date(status.updated_at).toLocaleString()}</p>}
      <p>{status.diagnostics}</p>
      <Button onClick={() => void copy(JSON.stringify({ state: status.state, sampled_at: status.updated_at, sharing_verified: status.sharing_verified, gateway: status.gateway_address, diagnostics: status.diagnostics }, null, 2))}>{text("复制诊断", "Copy diagnostics")}</Button>
    </details>}
    <div className="hotspot-help">
      {active && status?.gateway_address && <p>{text("热点网关", "Hotspot gateway")}: {status.gateway_address}</p>}
      <p>{text("手机连接上面的 Wi-Fi 即可，无需安装客户端或设置代理。停止聚合或退出 HypoMux 时，热点会自动关闭。", "Connect your phone to this Wi-Fi network. No client or proxy settings are needed. The hotspot closes when aggregation stops or HypoMux exits.")}</p>
      <p>{text("沿用当前聚合线路和分流规则，多连接可利用多条线路，单连接速度不保证叠加。首次使用请在手机上验证网页、视频和下载。", "Uses your current aggregation links and routing rules. Multiple connections can use multiple links; single-connection bonding is not guaranteed. Verify browsing, video and downloads on your phone on first use.")}</p>
      {status?.sharing_verified && !pollError && <p>{text("共享出口已校验", "Shared egress verified")}: {status.shared_adapter}</p>}
    </div>
  </section>;
}
