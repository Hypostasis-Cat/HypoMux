import { Badge, Button, Dropdown, Field, Input, Option, Spinner, Switch } from "@fluentui/react-components";
import { Wifi124Regular, Phone24Regular } from "@fluentui/react-icons";
import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { useI18n } from "../i18n/i18n";
import { appServices, type HotspotConfig, type HotspotStatus } from "../platform/services";
import { hotspotDraft } from "./hotspotDraft";
import { QRCodeSVG } from "qrcode.react";
import { createHotspotPassword, hotspotQRPayload } from "./hotspotAccess";
import { runHotspotOperation, useHotspotOperation } from "./hotspotOperation";

// Keep inputs mounted and animate only changed content, never the card shell.
function useContentTransition(state: string) {
  const ref = useRef<HTMLDivElement>(null);
  const previous = useRef(state);
  useLayoutEffect(() => {
    if (previous.current === state) return;
    previous.current = state;
    if (!ref.current?.animate || window.matchMedia?.("(prefers-reduced-motion: reduce)").matches) return;
    const animation = ref.current.animate(
      [{ opacity: 0.35, transform: "translateY(5px)" }, { opacity: 1, transform: "translateY(0)" }],
      { duration: 220, easing: "cubic-bezier(0.2, 0, 0, 1)" },
    );
    return () => animation.cancel();
  }, [state]);
  return ref;
}

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
  const [sessionConfig, setSessionConfig] = useState<{ id: string; config: HotspotConfig }>();
  const [credentialsError, setCredentialsError] = useState("");
  const [credentialsRevision, setCredentialsRevision] = useState(0);
  useEffect(() => {
    let cancelled = false;
    setSessionConfig(undefined); setCredentialsError("");
    const id = status?.session_id;
    if (status?.state === "running" && id) {
      void appServices.engine.hotspotSessionConfig(id).then(config => {
        if (!cancelled) setSessionConfig({ id, config });
      }).catch(reason => { if (!cancelled) setCredentialsError(String(reason)); });
    }
    return () => { cancelled = true; };
  }, [status?.session_id, status?.state, credentialsRevision]);
  const [localPending, setPending] = useState(false);
  const sharedOperation = useHotspotOperation();
  const pending = localPending || !!sharedOperation;
  const [showQR, setShowQR] = useState(false);
  const [error, setError] = useState("");
  const [pollError, setPollError] = useState("");
  const changingHotspot = pending && (sharedOperation === "start" || sharedOperation === "stop");
  useEffect(() => { if (status?.state !== "running" || pollError || changingHotspot) setShowQR(false); }, [status?.state, pollError, changingHotspot]);
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
          const initial = saved.password ? saved : { ...saved, password: createHotspotPassword() };
          hotspotDraft.current = initial;
          updateConfig(initial);
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
  const active = status?.state === "running" || status?.state === "starting" || status?.state === "stopping";
  const liveConfig = sessionConfig?.id === status?.session_id ? sessionConfig?.config : undefined;
  const displayedConfig = active ? liveConfig : config;
  const canShowQR = status?.state === "running" && !pollError && !changingHotspot && !!liveConfig?.password && liveConfig.ssid === status.ssid;
  const qrVisible = showQR && canShowQR;
  const settingsRef = useContentTransition(loadingConfig ? "loading" : active ? "active" : "editable");
  const connectionState = qrVisible ? "qr" : pollError ? "unavailable" : !status ? "loading" : `${status.state}-${status.ready}-${!!status.devices_available}-${!!status.devices?.length}`;
  const connectionRef = useContentTransition(connectionState);
  const band = (active ? status?.band : config.band) ?? "auto";
  const bandLabel = band === "5" ? "5 GHz" : band === "2.4" ? "2.4 GHz" : text("自动", "Automatic");
  const validName = config.ssid.trim().length > 0 && new TextEncoder().encode(config.ssid).length <= 32 && !/[\0\r\n]/.test(config.ssid);
  const validPassword = /^[\x20-\x7e]{8,63}$/.test(config.password);
  const save = async () => {
    if (busy.current) return;
    busy.current = true; revision.current++; setPending(true); setAction("save"); setError(""); setNotice("");
    try {
      if (!displayedConfig) return;
      await runHotspotOperation("save", () => appServices.engine.saveHotspotPreferences(displayedConfig));
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
    setConfig(value => ({ ...value, password: createHotspotPassword() }));
    setNotice(text("新密码尚未保存。保存设置或开启热点后生效。", "New password is not saved yet. Save settings or enable the hotspot to apply it."));
  };
  const change = async (start: boolean) => {
    if (busy.current) return;
    revision.current++;
    setShowQR(false);
    busy.current = true; setPending(true); setAction(start ? "start" : "stop"); setError(""); setNotice("");
    try {
      const next = await runHotspotOperation(start ? "start" : "stop", () => start ? appServices.engine.startHotspot(config) : appServices.engine.stopHotspot());
      if (mounted.current) { setStatus(next); setPollError(""); }
    } catch (reason) {
      if (mounted.current) setError(String(reason));
      try {
        const next = await appServices.engine.hotspotStatus();
        if (mounted.current) setStatus(next);
      } catch { /* Preserve the action error; the next poll retries status. */ }
    } finally { busy.current = false; if (mounted.current) { setPending(false); setAction(undefined); } }
  };
  const stateText = pending ? ((action ?? sharedOperation) === "start" ? text("正在开启热点…", "Starting hotspot…") : (action ?? sharedOperation) === "stop" ? text("正在关闭热点…", "Stopping hotspot…") : text("正在保存设置…", "Saving settings…"))
    : pollError ? text("状态暂不可用", "Status unavailable")
    : !status ? text("正在读取状态", "Checking status")
    : status.state === "running" ? (status.sharing_verified ? text("热点已开启", "Hotspot is on") : text("热点已开启 · 出口待验证", "Hotspot is on · egress unverified"))
    : status.state === "stopping" ? text("正在关闭热点…", "Stopping hotspot…")
    : status.state === "starting" ? text("正在启动", "Starting")
    : status.state === "failed" ? text("热点需要检查", "Hotspot needs attention")
    : text("热点已关闭", "Hotspot is off");
  return <section className="hotspot-panel" aria-label={text("聚合热点设置", "Aggregation hotspot settings")}>
    <div className={`hotspot-control glass-surface${status?.state === "running" && !pollError ? " is-running" : ""}`}>
      <span className="hotspot-hero-icon" aria-hidden="true"><Wifi124Regular /></span>
      <div className="hotspot-control-copy"><span className="hotspot-eyebrow">{text("网络共享", "NETWORK SHARING")}</span><strong>{text("共享你的聚合网络", "Share your aggregated connection")}</strong><p>{text("一台电脑，多设备连接。无需额外安装或代理设置。", "One PC, more devices. No extra apps or proxy setup.")}</p>
        <div className="hotspot-summary" role="status">
          <Badge appearance="tint" color={status?.state === "running" && status.sharing_verified && !pollError ? "success" : "informative"}>{stateText}</Badge>
          {pending && <Spinner size="tiny" />}
        </div>
      </div>
      <Switch label={text("聚合热点", "Aggregation hotspot")} checked={!!active}
        disabled={pending || status?.state === "stopping" || !status || (!active && (loadingConfig || !!pollError || !status.ready || !validName || !validPassword))}
        onChange={(_, data) => void change(data.checked)} />
    </div>
    <div className="hotspot-layout"><div className="hotspot-settings hotspot-surface glass-surface">
    <header className="hotspot-section-heading"><h2>{text("网络设置", "Network settings")}</h2><span>{active ? text("使用中", "In use") : text("开启前可编辑", "Edit before sharing")}</span></header>
    <div className="hotspot-settings-content" ref={settingsRef}>
    <div className="hotspot-fields">
      <Field label={text("热点名称", "Network name")} validationState={!active && !validName ? "error" : "none"} validationMessage={!active && !validName ? text("请输入 1–32 个 UTF-8 字节，不能包含换行。", "Enter 1–32 UTF-8 bytes without line breaks.") : undefined} hint={text("在手机 Wi-Fi 列表中显示的名称", "The name shown in your phone’s Wi-Fi list")}>
        <Input value={active ? status?.ssid : config.ssid} disabled={loadingConfig || pending || active} onChange={(_, data) => setConfig(value => ({ ...value, ssid: data.value }))} />
      </Field>
      <Field label={text("Wi-Fi 频段", "Wi-Fi band")} hint={text("自动优先尝试 5 GHz，频段受限时回退；旧设备可手动选择 2.4 GHz", "Automatic prefers 5 GHz and falls back on band restrictions; choose 2.4 GHz for older devices")}>
        <Dropdown className="hotspot-band-dropdown" value={bandLabel} selectedOptions={[band]} disabled={loadingConfig || pending || active}
          onOptionSelect={(_, data) => {
            if (data.optionValue === "auto" || data.optionValue === "5" || data.optionValue === "2.4") {
              const nextBand = data.optionValue;
              setConfig(value => ({ ...value, band: nextBand }));
            }
          }}>
          <Option value="auto">{text("自动", "Automatic")}</Option><Option value="5">5 GHz</Option><Option value="2.4">2.4 GHz</Option>
        </Dropdown>
      </Field>
      <Field label={text("热点密码", "Network password")} hint={text("8–63 位英文、数字或符号", "8–63 printable ASCII characters")}>
        <Input type={showPassword ? "text" : "password"} autoComplete="new-password" value={displayedConfig?.password ?? ""} disabled={loadingConfig || pending || active}
          onChange={(_, data) => setConfig(value => ({ ...value, password: data.value }))}
          contentAfter={<Button size="small" appearance="transparent" aria-pressed={showPassword} onClick={() => setShowPassword(value => !value)}>{showPassword ? text("隐藏", "Hide") : text("显示", "Show")}</Button>} />
      </Field>
    </div>
    <div className="hotspot-actions">
      <Button appearance="primary" disabled={loadingConfig || pending || status?.state === "starting" || status?.state === "stopping" || (active ? !liveConfig : !validName || !validPassword)} onClick={() => void save()}>{text("保存设置", "Save settings")}</Button><Button disabled={loadingConfig || pending || active} onClick={generatePassword}>{text("生成密码", "Generate password")}</Button>
      <Button disabled={loadingConfig || pollError !== "" || !displayedConfig?.password} onClick={() => { if (displayedConfig) void copy(displayedConfig.password); }}>{text("复制密码", "Copy password")}</Button>
    </div>
    {active && credentialsError && <p role="alert">{text("无法读取当前热点的连接信息。", "Cannot read this hotspot's connection details.")}<Button size="small" onClick={() => setCredentialsRevision(value => value + 1)}>{text("重试", "Retry")}</Button></p>}
    <p className="hotspot-save-hint">{active ? text("热点运行中仍可保存设置；修改名称、密码或频段前，请先关闭热点。", "Settings can be saved while the hotspot is on. Turn it off before editing its name, password or band.") : text("可提前保存设置；开启时也会自动加密保存。", "You can save settings in advance. Enabling also saves them encrypted.")}</p>
    </div>
    <div className="hotspot-feedback" tabIndex={0} aria-label={text("操作与状态提示", "Operation and status messages")}>
    {notice && <p className="hotspot-notice" role="status">{notice}</p>}
    {status?.state === "failed" && !status.cleanup_complete && <p className="hotspot-error" role="alert">{status.hotspot_off_confirmed === true
      ? text("热点已关闭，但原配置未完全恢复。请在 Windows 移动热点设置中检查名称、密码和频段。", "The hotspot is off, but its original settings were not fully restored. Check the name, password and band in Windows Mobile hotspot settings.")
      : text("尚未确认热点已关闭。请在 Windows 设置中检查并关闭移动热点，然后重新开启聚合热点。", "Hotspot shutdown is unconfirmed. Check and turn off Mobile hotspot in Windows Settings before starting again.")}</p>}
    {(error || pollError || (status?.state === "failed" && status.message)) && <p className="hotspot-error" role={error || pollError || status?.state === "failed" ? "alert" : "status"}>{error || pollError || status?.message}</p>}
    </div></div><aside className="hotspot-connect hotspot-surface glass-surface" aria-label={text("手机连接", "Phone connection")}>
    <header className="hotspot-section-heading"><h2>{text("连接你的设备", "Connect your devices")}</h2><Phone24Regular aria-hidden="true" /></header>
    <div className="hotspot-connect-body" ref={connectionRef} id="hotspot-connection-content">
    {qrVisible ? <div className="hotspot-qr-content" key="qr">
      <strong>{status?.ssid}</strong>
      <QRCodeSVG value={hotspotQRPayload(liveConfig!)} size={224} level="M" marginSize={4} title={text("Wi-Fi 连接二维码", "Wi-Fi connection QR code")} />
      <p>{text("用手机相机或 WLAN 扫一扫连接。二维码包含热点密码，请只向需要连接的人展示。", "Scan with your phone camera or Wi-Fi scanner. This code contains the network password; show it only to people you want to connect.")}</p>
    </div> : <div className="hotspot-device-view" key="devices">
    <div className="hotspot-connect-intro"><span className="hotspot-phone-icon" aria-hidden="true"><Wifi124Regular /></span><strong>{status?.state === "running" && !pollError ? status.ssid : text("准备好，随时连接", "Ready when you are")}</strong><p>{status?.state === "running" && !pollError ? text("打开手机 Wi-Fi，选择此网络", "Choose this network in your phone’s Wi-Fi settings") : text("开启热点后，手机、平板都可以加入", "Once enabled, phones and tablets can join")}</p></div>
    {!active && <ol className="hotspot-connect-steps"><li>{text("在电脑上启动 TUN 聚合", "Start TUN aggregation on your PC")}</li><li>{text("开启本页的聚合热点", "Enable the hotspot on this page")}</li><li>{text("手机选择热点，输入密码连接", "Select the network and enter its password")}</li></ol>}
    {active && !pollError && <div className="hotspot-devices">
      <div className="hotspot-section-heading"><strong>{text("连接设备", "Connected devices")}</strong>{status?.state === "running" && <Badge appearance="tint" aria-label={`${text("已连接设备", "Connected devices")}: ${status.clients}`}>{status.clients}</Badge>}</div>
      {!status?.devices_available ? <p>{text("Windows 暂未提供设备明细，不影响热点使用。", "Windows device details are unavailable. The hotspot can still be used.")}</p>
        : !status.devices?.length ? <p>{text("等待设备连接，请在手机 WLAN 设置中选择上面的热点名称。", "Waiting for devices. Select the network above in your phone’s Wi-Fi settings.")}</p>
        : <ul tabIndex={0} aria-label={text("已连接设备列表", "Connected device list")}>{status.devices.map((device, index) => <li key={`${device.mac}-${index}`}><span>{device.hosts?.join(" · ") || text("未命名设备", "Unnamed device")}</span><code>{device.mac}</code></li>)}</ul>}
    </div>}
    </div>}
    </div>
    <Button className="hotspot-qr-toggle" disabled={!canShowQR} aria-pressed={qrVisible} aria-controls="hotspot-connection-content" onClick={() => setShowQR(value => !value)}>{qrVisible ? text("隐藏二维码", "Hide connection code") : text("扫码连接", "Scan to connect")}</Button>
    </aside></div>
    <details className="hotspot-error"><summary>{text("共享诊断", "Sharing diagnostics")}</summary>
      {active && status?.configured_band && <p>{text("热点频段配置", "Configured hotspot band")}: {status.configured_band === "auto" ? text("Windows 自动选择", "Windows automatic selection") : `${status.configured_band} GHz`}{status.band_fallback ? text("（5 GHz 受限，已回退）", " (5 GHz restricted; fallback applied)") : ""}</p>}
      {active && !!status?.transmit_link_mbps && <p>{text("热点网卡报告的发送链路速率", "Hotspot adapter reported transmit link rate")}: {status.transmit_link_mbps} Mbps. {text("这是驱动报告的链路值，不代表手机实测吞吐。", "This driver-reported link value is not measured phone throughput.")}</p>}
      <p>{text("热点已开启只表示 Wi-Fi 可接入；设备数量表示 Windows 已检测到连接。出口已校验表示共享接口匹配，但不是手机互联网测速结果。", "Hotspot on means Wi-Fi is available; the device count reflects Windows connections. Verified egress confirms the sharing interfaces, not a phone internet speed test.")}</p>
      {status?.state !== "failed" && status?.message && <p>{status.message}</p>}
      {!status?.sharing_verified && <p>{text("Windows 未提供共享接口记录时，仍可正常使用热点。若手机能连接但无法上网，请先检查电脑聚合是否能上网，再复制诊断排查。", "When Windows omits sharing records, the hotspot can still work. If a phone connects without internet, check internet access through PC aggregation, then copy these diagnostics.")}</p>}
      {status?.updated_at && <p>{text("状态采样时间", "Status sampled at")}: {new Date(status.updated_at).toLocaleString()}</p>}
      <p>{status?.diagnostics || text("暂无共享诊断，开启热点后将在这里显示。", "Sharing diagnostics will appear here after starting the hotspot.")}</p>
      <Button disabled={!status?.diagnostics} onClick={() => status && void copy(JSON.stringify({ state: status.state, sampled_at: status.updated_at, requested_band: status.band, configured_band: status.configured_band, band_fallback: status.band_fallback, transmit_link_mbps: status.transmit_link_mbps, receive_link_mbps: status.receive_link_mbps, sharing_verified: status.sharing_verified, gateway: status.gateway_address, diagnostics: status.diagnostics }, null, 2))}>{text("复制诊断", "Copy diagnostics")}</Button>
    </details>
    <details className="hotspot-help"><summary>{text("使用说明与共享规则", "Usage and sharing details")}</summary><div>
      {active && status?.gateway_address && <p>{text("热点网关", "Hotspot gateway")}: {status.gateway_address}</p>}
      <p>{text("手机连接上面的 Wi-Fi 即可，无需安装客户端或设置代理。停止聚合或退出 HypoMux 时，热点会自动关闭。", "Connect your phone to this Wi-Fi network. No client or proxy settings are needed. The hotspot closes when aggregation stops or HypoMux exits.")}</p>
      <p>{text("沿用当前聚合线路和分流规则，多连接可利用多条线路，单连接速度不保证叠加。首次使用请在手机上验证网页、视频和下载。", "Uses your current aggregation links and routing rules. Multiple connections can use multiple links; single-connection bonding is not guaranteed. Verify browsing, video and downloads on your phone on first use.")}</p>
      <p>{text("无线网卡同时接收上游 Wi-Fi 和发送热点时会共享无线资源。对比速度时，请在电脑和手机使用相同下载源及多连接方式，并避免同时测速。", "A Wi-Fi adapter shares radio resources between its upstream connection and hotspot. Compare PC and phone speeds using the same source and multiple connections, one test at a time.")}</p>
      {status?.sharing_verified && !pollError && <p>{text("共享出口已校验", "Shared egress verified")}: {status.shared_adapter}</p>}
    </div></details>
  </section>;
}
