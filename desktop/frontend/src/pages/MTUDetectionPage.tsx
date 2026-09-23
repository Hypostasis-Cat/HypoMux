import { Button, Dialog, DialogActions, DialogBody, DialogContent, DialogSurface, DialogTitle, Dropdown, Field, Input, Option, Spinner } from "@fluentui/react-components";
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
    <GlassSurface className="mtu-panel mtu-setup">
      <div className="mtu-panel-heading">
        <div><span className="mtu-eyebrow">{text("01 · 检测路径", "01 · Network path")}</span><h2>{text("选择网卡与目标", "Choose adapter and target")}</h2></div>
        <span className="mtu-tag">IPv4</span>
      </div>
      <p className="mtu-description">{text("检测这条网络路径可通过的数据包大小，检测本身不会修改网卡设置。", "Find the packet size supported by this path. Testing does not change adapter settings.")}</p>
      <div className="mtu-fields">
        <Field label={text("网络适配器", "Network adapter")}>
          <Dropdown
            className="mtu-adapter-dropdown"
            value={selected ? `${selected.name} · ${selected.address}` : ""}
            selectedOptions={adapter ? [adapter] : []}
            placeholder={text("没有可用网卡", "No adapters available")}
            disabled={!!busy || loading || !adapters.length}
            listbox={{ className: "mtu-adapter-listbox" }}
            onOptionSelect={(_, data) => { if (data.optionValue) setAdapter(data.optionValue); }}
          >
            {adapters.map(item => <Option key={item.id} value={item.id} text={`${item.name} · ${item.address}`}>
              <span className="mtu-adapter-option"><strong>{item.name}</strong><small>{item.address}</small></span>
            </Option>)}
          </Dropdown>
        </Field>
        <Field label={text("目标 IPv4 地址", "Target IPv4 address")} hint={text("目标需要允许 ICMP 回应。", "The target must respond to ICMP.")}>
          <Input value={target} disabled={!!busy} onChange={(_, data) => { setTarget(data.value); setResult(undefined); setMessage(""); }} placeholder="223.5.5.5" spellCheck={false} autoComplete="off" />
        </Field>
      </div>
      {blocked && <p className="mtu-notice" role="status">{text("请先停止网络服务，再检测或修改 MTU，以避免流量接管影响结果。", "Stop the network service before testing or changing MTU.")}</p>}
      {preview && <p className="mtu-notice" role="status">{text("浏览器预览仅展示界面，检测与修改需在桌面客户端执行。", "Preview only. Use the desktop app to test or change MTU.")}</p>}
      {error && <p role="alert" className="mtu-notice mtu-error">{error}</p>}
      {message && <p className="mtu-notice mtu-success" role="status">{message}</p>}
      <div className="mtu-actions">
        <Button appearance="primary" disabled={!!busy || !info || !target.trim() || blocked || preview} onClick={() => void run("detect")}>{text("检测推荐 MTU", "Detect recommended MTU")}</Button>
        {busy === "detect" ? <Button onClick={() => void cancel()}>{text("取消检测", "Cancel test")}</Button>
          : <Button appearance="subtle" disabled={!!busy || !adapter} onClick={() => setRevision(value => value + 1)}>{text("刷新当前值", "Refresh current value")}</Button>}
      </div>
      <div className="mtu-progress" role="status" aria-live="polite">
        {busy ? <Spinner size="tiny" label={busy === "detect" ? text("正在探测并复测，最长约 50 秒…", "Probing and verifying, up to 50 seconds…") : busy === "read" ? text("正在读取…", "Reading…") : text("正在修改并验证…", "Applying and verifying…")} />
          : <span>{text("结果仅适用于所选网卡到目标地址的路径。", "Results apply only to the selected adapter and target.")}</span>}
      </div>
    </GlassSurface>
    <GlassSurface className="mtu-panel mtu-result">
      <div className="mtu-panel-heading">
        <div><span className="mtu-eyebrow">{text("02 · 检测结果", "02 · Test result")}</span><h2>{text("推荐 MTU", "Recommended MTU")}</h2></div>
        <span className="mtu-tag">{busy === "detect" ? text("检测中", "Testing") : result ? text("检测完成", "Complete") : error ? text("未获得结果", "No result") : text("等待检测", "Awaiting test")}</span>
      </div>
      <div className={`mtu-recommendation${result ? " has-result" : ""}`}>
        <strong>{result?.recommended ?? "—"}</strong><span>{text("字节", "bytes")}</span>
      </div>
      <div className="mtu-values">
        <div><span>{text("当前 MTU", "Current MTU")}</span><strong>{info?.current ?? "—"}<small>{text("字节", "bytes")}</small></strong></div>
        <div><span>{text("已保存原值", "Saved original")}</span><strong>{info?.original ?? "—"}<small>{text("字节", "bytes")}</small></strong></div>
      </div>
      <div className="mtu-result-copy" role="status" aria-live="polite">
        {result ? <><p className="mtu-result-path">{text("检测目标", "Target")}: {result.target} · {result.address}</p><p>{result.at_limit ? text("当前 MTU 已通过检测，无需修改。检测未尝试超过当前网卡 MTU 的值。", "Current MTU passed. No change needed; values above the current MTU were not tested.") : text(`推荐从 ${info?.current} 调整为 ${result.recommended} 字节，应用前请确认。`, `Recommended change: ${info?.current} → ${result.recommended} bytes. Confirm before applying.`)}</p></>
          : <p>{busy === "detect" ? text("正在查找可通过的大小，并复测确认。完成后会在这里显示推荐值。", "Finding a supported size and verifying it. The recommendation will appear here when complete.") : error ? text("当前没有可用推荐值。请根据左侧错误提示处理后重试。", "No recommendation is available. Resolve the error shown on the left and try again.") : text("完成检测后显示推荐值。检测超时或被过滤时不会生成推荐值。", "Run a test to see a recommendation. Timeouts or filtered probes produce no recommendation.")}</p>}
      </div>
      <div className="mtu-actions mtu-result-actions">
        <Button appearance="primary" disabled={!!busy || blocked || preview || !result || result.recommended === info?.current} onClick={() => setConfirm("apply")}>{text("应用推荐值", "Apply recommendation")}</Button>
        <Button disabled={!!busy || blocked || preview || !info?.original || info.original === info.current} onClick={() => setConfirm("restore")}>{text("恢复原值", "Restore original")}</Button>
      </div>
      <p className="mtu-change-note">{text("修改仅作用于所选网卡的 IPv4，可能短暂影响连接；重启后恢复系统配置。原值会保存在本机，重开应用后仍可恢复。", "Changes affect IPv4 on the selected adapter and may briefly interrupt connections. System configuration returns after reboot. The original value is saved locally for restoration after reopening the app.")}</p>
    </GlassSurface>
    <Dialog open={!!confirm} onOpenChange={(_, data) => { if (!data.open) setConfirm(undefined); }}>
      <DialogSurface><DialogBody><DialogTitle>{confirm === "restore" ? text("恢复原 MTU", "Restore original MTU") : text("应用推荐 MTU", "Apply recommended MTU")}</DialogTitle>
        <DialogContent>{selected?.name}: {info?.current} → {confirm === "restore" ? info?.original : result?.recommended} bytes<p>{text("连接可能短暂中断。Windows 可能请求管理员授权。", "Connections may briefly drop. Windows may request administrator permission.")}</p></DialogContent>
        <DialogActions><Button onClick={() => setConfirm(undefined)}>{text("取消", "Cancel")}</Button><Button appearance="primary" onClick={() => { if (confirm) void run(confirm); }}>{text("确认修改", "Confirm change")}</Button></DialogActions>
      </DialogBody></DialogSurface>
    </Dialog>
  </section>;
}
