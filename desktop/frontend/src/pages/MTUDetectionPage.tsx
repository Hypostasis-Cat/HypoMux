import { Button, Dialog, DialogActions, DialogBody, DialogContent, DialogSurface, DialogTitle, Field, Input, Select, Spinner } from "@fluentui/react-components";
import { useEffect, useRef, useState } from "react";
import { GlassSurface } from "../components/material/GlassSurface";
import { appServices, type AdapterView, type MTUInfo, type MTUResult } from "../platform/services";
import type { EnginePhase } from "../state/useEngineState";
import "./mtu.css";

export function MTUDetectionPage({ adapters, enginePhase, loading, preview, text }: {
  adapters: AdapterView[]; enginePhase?: EnginePhase; loading: boolean; preview: boolean;
  text: (zh: string, en: string) => string;
}) {
  const [adapter, setAdapter] = useState("");
  const [target, setTarget] = useState("223.5.5.5");
  const [info, setInfo] = useState<MTUInfo>();
  const [result, setResult] = useState<MTUResult>();
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [confirm, setConfirm] = useState<"apply" | "restore">();
  const [revision, setRevision] = useState(0);
  const operation = useRef("");
  const generation = useRef(0);
  const blocked = !!enginePhase && ["starting", "running", "degraded", "stopping"].includes(enginePhase);
  const selected = adapters.find(item => item.id === adapter);
  const source = selected?.address;
  const index = selected?.if_index;

  useEffect(() => {
    if (!adapters.some(item => item.id === adapter)) setAdapter(adapters.find(item => item.selected)?.id ?? adapters[0]?.id ?? "");
  }, [adapters, adapter]);
  useEffect(() => {
    const current = ++generation.current;
    setInfo(undefined); setResult(undefined); setError(""); setMessage(""); setConfirm(undefined);
    if (!adapter) return;
    operation.current = "read"; setBusy("read");
    const request = preview ? Promise.resolve({ adapter_id: adapter, guid: "preview", if_index: index ?? 1, address: source ?? "", current: 1500 }) : appServices.mtu.current(adapter);
    void request.then(value => { if (generation.current === current) setInfo(value); })
      .catch(reason => { if (generation.current === current) setError(String(reason)); })
      .finally(() => { if (generation.current === current) { operation.current = ""; setBusy(""); } });
    return () => { generation.current++; if (operation.current === "detect") void appServices.mtu.cancel().catch(() => {}); };
  }, [adapter, source, index, preview, revision]);

  const run = async (action: "detect" | "apply" | "restore") => {
    if (operation.current || !info || blocked || preview) return;
    operation.current = action; setBusy(action); setError(""); setMessage(""); setConfirm(undefined);
    const current = generation.current;
    if (action === "detect") setResult(undefined);
    try {
      if (action === "detect") {
        const value = await appServices.mtu.detect(adapter, target.trim());
        if (current === generation.current) { setInfo(value); setResult(value); }
      } else {
        const value = await appServices.mtu[action](adapter);
        if (current === generation.current) {
          setInfo(value); setResult(undefined);
          setMessage(action === "apply" ? text("已应用推荐 MTU。", "Recommended MTU applied.") : text("已恢复原 MTU。", "Original MTU restored."));
        }
      }
    } catch (reason) {
      if (current === generation.current) {
        setError(String(reason)); setResult(undefined);
        if (action !== "detect") {
          try { const value = await appServices.mtu.current(adapter); if (current === generation.current) setInfo(value); }
          catch { if (current === generation.current) setInfo(undefined); }
        }
      }
    } finally { if (current === generation.current) { operation.current = ""; setBusy(""); } }
  };
  const cancel = async () => {
    try { await appServices.mtu.cancel(); } catch (reason) { setError(String(reason)); }
  };

  return <section className="mtu-page health-view-enter" aria-label={text("MTU 检测", "MTU detection")}>
    <GlassSurface className="mtu-panel" tone="secondary">
      <div className="mtu-panel-heading"><div><h2>{text("选择检测路径", "Choose a network path")}</h2><p>{text("按网卡和目标检测，结果不代表所有网络路径的最佳值。", "Results apply to this adapter and target, not every network path.")}</p></div><span className="mtu-tag">IPv4</span></div>
      <div className="mtu-fields">
        <Field label={text("网络适配器", "Network adapter")}>
          <Select value={adapter} disabled={!!busy || loading || !adapters.length} onChange={(_, data) => setAdapter(data.value)}>
            {!adapters.length && <option value="">{text("没有可用网卡", "No adapters available")}</option>}
            {adapters.map(item => <option key={item.id} value={item.id}>{item.name} · {item.address}</option>)}
          </Select>
        </Field>
        <Field label={text("目标 IPv4 地址", "Target IPv4 address")} hint={text("目标需要允许 ICMP 回应。", "The target must respond to ICMP.")}>
          <Input value={target} disabled={!!busy} onChange={(_, data) => { setTarget(data.value); setResult(undefined); setMessage(""); }} placeholder="223.5.5.5" />
        </Field>
      </div>
      <div className="mtu-values">
        <div><span>{text("当前 MTU", "Current MTU")}</span><strong>{info?.current ?? "—"}<small>bytes</small></strong></div>
        <div><span>{text("推荐 MTU", "Recommended MTU")}</span><strong>{result?.recommended ?? "—"}<small>bytes</small></strong></div>
        <div><span>{text("已保存原值", "Saved original")}</span><strong>{info?.original ?? "—"}<small>bytes</small></strong></div>
      </div>
      {blocked && <p role="status">{text("请先停止网络服务，再检测或修改 MTU，以避免流量接管影响结果。", "Stop the network service before testing or changing MTU.")}</p>}
      {preview && <p role="status">{text("浏览器预览仅展示界面，检测与修改需在桌面客户端执行。", "Preview only. Use the desktop app to test or change MTU.")}</p>}
      {error && <p role="alert" className="mtu-error">{error}</p>}
      {message && <p role="status">{message}</p>}
      <div className="mtu-actions">
        <Button appearance="primary" disabled={!!busy || !info || !target.trim() || blocked || preview} onClick={() => void run("detect")}>{text("检测推荐 MTU", "Detect recommended MTU")}</Button>
        <Button disabled={!!busy || !adapter} onClick={() => setRevision(value => value + 1)}>{text("刷新当前值", "Refresh current value")}</Button>
        {busy === "detect" && <Button onClick={() => void cancel()}>{text("取消检测", "Cancel test")}</Button>}
        {busy && <Spinner size="tiny" label={busy === "detect" ? text("正在探测并复测，最长约 50 秒…", "Probing and verifying, up to 50 seconds…") : busy === "read" ? text("正在读取…", "Reading…") : text("正在修改并验证…", "Applying and verifying…")} />}
      </div>
    </GlassSurface>
    <GlassSurface className="mtu-panel">
      <h2>{text("检测结果与应用", "Results and changes")}</h2>
      {result ? <div role="status"><p>{text("检测目标", "Target")}: {result.target} · {result.address}</p><p>{result.at_limit ? text("当前 MTU 已通过检测，无需修改。检测未尝试超过当前网卡 MTU 的值。", "Current MTU passed. No change needed; values above the current MTU were not tested.") : text(`推荐从 ${info?.current} 调整为 ${result.recommended} 字节，应用前请确认。`, `Recommended change: ${info?.current} → ${result.recommended} bytes. Confirm before applying.`)}</p></div> : <p>{text("完成检测后，这里会显示推荐值。检测超时或被过滤时不会生成推荐值。", "Run a test to see a recommendation. Timeouts or filtered probes produce no recommendation.")}</p>}
      <p>{text("修改仅作用于所选网卡的 IPv4，可能短暂影响连接；重启后恢复系统配置。原值会保存在本机，重开应用后仍可恢复。", "Changes affect IPv4 on the selected adapter and may briefly interrupt connections. System configuration returns after reboot. The original value is saved locally for restoration after reopening the app.")}</p>
      <div className="mtu-actions">
        <Button appearance="primary" disabled={!!busy || blocked || preview || !result || result.recommended === info?.current} onClick={() => setConfirm("apply")}>{text("应用推荐值", "Apply recommendation")}</Button>
        <Button disabled={!!busy || blocked || preview || !info?.original || info.original === info.current} onClick={() => setConfirm("restore")}>{text("恢复原值", "Restore original")}</Button>
      </div>
    </GlassSurface>
    <Dialog open={!!confirm} onOpenChange={(_, data) => { if (!data.open) setConfirm(undefined); }}>
      <DialogSurface><DialogBody><DialogTitle>{confirm === "restore" ? text("恢复原 MTU", "Restore original MTU") : text("应用推荐 MTU", "Apply recommended MTU")}</DialogTitle>
        <DialogContent>{selected?.name}: {info?.current} → {confirm === "restore" ? info?.original : result?.recommended} bytes<p>{text("连接可能短暂中断。Windows 可能请求管理员授权。", "Connections may briefly drop. Windows may request administrator permission.")}</p></DialogContent>
        <DialogActions><Button onClick={() => setConfirm(undefined)}>{text("取消", "Cancel")}</Button><Button appearance="primary" onClick={() => { if (confirm) void run(confirm); }}>{text("确认修改", "Confirm change")}</Button></DialogActions>
      </DialogBody></DialogSurface>
    </Dialog>
  </section>;
}
