import {
  Badge,
  Button,
  Dropdown,
  Input,
  Option,
  Spinner,
} from "@fluentui/react-components";
import {
  ArrowSync20Regular,
  Add20Regular,
  Checkmark20Regular,
  CheckmarkCircle20Regular,
  Delete20Regular,
  Dismiss20Regular,
  Globe20Regular,
  Play20Regular,
  Stop20Regular,
  Warning20Regular,
} from "@fluentui/react-icons";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { GlassSurface } from "../components/material/GlassSurface";
import {
  appServices,
  type AdapterView,
  type NATDetectionResult,
  type NATServerSnapshot,
} from "../platform/services";
import { startSerialPoll } from "../platform/serialPoll";

type Text = (zh: string, en: string) => string;
type Notify = (title: string, message: string, intent?: "success" | "error" | "warning") => void;

type NATDetectionPageProps = {
  adapters: AdapterView[];
  loading: boolean;
  preview: boolean;
  text: Text;
  notify: Notify;
};

const emptyResult = (): NATDetectionResult => ({ state: "idle" });

const previewServers = (): NATServerSnapshot => ({
  selected_id: "auto",
  servers: [
    { id: "builtin-miwifi", name: "小米 STUN", address: "stun.miwifi.com:3478", built_in: true },
    { id: "builtin-stuntman", name: "STUNTMAN 官方", address: "stunserver2025.stunprotocol.org:3478", built_in: true },
    { id: "builtin-voipgate", name: "Pion / VoIPGate", address: "stun.voipgate.com:3478", built_in: true },
  ],
});

const previewResult = (adapter: AdapterView): NATDetectionResult => ({
  state: "completed",
  adapter_id: adapter.id,
  name: adapter.name,
  address: adapter.address,
  nat_type: adapter.kind === "wifi" ? "symmetric" : "full_cone",
  mapping_behavior: adapter.kind === "wifi" ? "address_port_dependent" : "endpoint_independent",
  filtering_behavior: adapter.kind === "wifi" ? "address_port_dependent" : "endpoint_independent",
  public_endpoint: adapter.kind === "wifi" ? "198.51.100.26:48192" : "203.0.113.18:52144",
  server: "stun.voipgate.com:3478",
  detail: "RFC 5780 mapping and filtering tests completed",
  duration_ms: 1846,
  started_at: new Date(Date.now() - 1846).toISOString(),
  completed_at: new Date().toISOString(),
});

const previewFailure = (adapter: AdapterView): NATDetectionResult => ({
  state: "inconclusive",
  adapter_id: adapter.id,
  name: adapter.name,
  address: adapter.address,
  nat_type: "inconclusive",
  mapping_behavior: "inconclusive",
  filtering_behavior: "inconclusive",
  detail: "All configured STUN servers failed",
  duration_ms: 6120,
  completed_at: new Date().toISOString(),
  attempts: [
    { server: "stun.voipgate.com:3478", resolved: "185.125.180.70:3478", code: "timeout", detail: "binding request: STUN response timeout", duration_ms: 3010 },
    { server: "stun.example.com:3478", code: "unsupported", detail: "STUN server does not support RFC 5780 OTHER-ADDRESS", duration_ms: 110 },
  ],
});

const previewFirewallLimited = (adapter: AdapterView): NATDetectionResult => ({
  ...previewResult(adapter),
  state: "inconclusive",
  nat_type: "inconclusive",
  mapping_behavior: "endpoint_independent",
  filtering_behavior: "inconclusive",
  host_firewall_limited: true,
  detail: "Windows Firewall may have blocked STUN responses from the alternate endpoint",
  duration_ms: 6032,
  attempts: [{
    server: "stun.miwifi.com:3478",
    resolved: "111.206.174.2:3478",
    code: "host_firewall",
    detail: "Mapping completed, but Windows Firewall can affect alternate-endpoint replies",
    duration_ms: 6032,
  }],
});

export function NATDetectionPage({ adapters, loading, preview, text, notify }: NATDetectionPageProps) {
  const eligible = useMemo(
    () => adapters.filter((adapter) => adapter.operational && Boolean(adapter.address)),
    [adapters],
  );
  const [adapterID, setAdapterID] = useState("");
  const [result, setResult] = useState<NATDetectionResult>(emptyResult);
  const [serverSnapshot, setServerSnapshot] = useState<NATServerSnapshot>({ selected_id: "auto", servers: [] });
  const [addingServer, setAddingServer] = useState(false);
  const [serverAddress, setServerAddress] = useState("");
  const [serverBusy, setServerBusy] = useState(false);
  const [firewallBusy, setFirewallBusy] = useState(false);
  const mounted = useRef(true);

  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; };
  }, []);

  useEffect(() => {
    if (adapterID && eligible.some((adapter) => adapter.id === adapterID)) return;
    const preferred = eligible.find((adapter) => adapter.id === result.adapter_id)
      ?? eligible.find((adapter) => adapter.selected)
      ?? eligible[0];
    setAdapterID(preferred?.id ?? "");
  }, [adapterID, eligible, result.adapter_id]);

  useEffect(() => {
    if (preview) {
      const preferred = eligible.find((adapter) => adapter.selected) ?? eligible[0];
      const fixture = new URLSearchParams(window.location.search).get("nat");
      setResult(preferred
        ? fixture === "failed" ? previewFailure(preferred) : fixture === "firewall" ? previewFirewallLimited(preferred) : previewResult(preferred)
        : emptyResult());
      return;
    }
    void appServices.diagnostics.natLatest()
      .then((latest) => { if (mounted.current) setResult(latest ?? emptyResult()); })
      .catch(() => { /* A missing persisted result is equivalent to idle. */ });
  }, [eligible, preview]);

  useEffect(() => {
    if (preview) {
      setServerSnapshot(previewServers());
      return;
    }
    void appServices.diagnostics.natServers()
      .then((snapshot) => { if (mounted.current) setServerSnapshot(snapshot); })
      .catch((error) => notify(
        text("无法读取 STUN 服务器", "Unable to load STUN servers"),
        error instanceof Error ? error.message : String(error),
        "warning",
      ));
  }, [notify, preview, text]);

  useEffect(() => {
    if (preview || result.state !== "running") return;
    return startSerialPoll(async () => {
      const latest = await appServices.diagnostics.natLatest();
      if (mounted.current) setResult(latest);
    }, 400);
  }, [preview, result.state]);

  const running = result.state === "running";
  const selected = eligible.find((adapter) => adapter.id === adapterID);
  const selectedServer = serverSnapshot.servers.find((server) => server.id === serverSnapshot.selected_id);

  const run = useCallback(async () => {
    if (!selected) {
      notify(
        text("尚未选择网卡", "No adapter selected"),
        text("请选择一张拥有有效 IPv4 的活动网卡。", "Select an active adapter with a valid IPv4 address."),
        "warning",
      );
      return;
    }
    setResult({
      state: "running",
      adapter_id: selected.id,
      name: selected.name,
      address: selected.address,
      started_at: new Date().toISOString(),
    });
    if (preview) {
      window.setTimeout(() => {
        if (mounted.current) setResult(previewResult(selected));
      }, 850);
      return;
    }
    try {
      const final = await appServices.diagnostics.runNAT(selected.id, serverSnapshot.selected_id);
      if (!mounted.current) return;
      setResult(final);
      notify(
        final.state === "completed" ? text("NAT 检测完成", "NAT detection complete") : text("NAT 类型未能判定", "NAT type inconclusive"),
        final.state === "completed"
          ? text("已完成 UDP 映射与过滤行为测试。", "UDP mapping and filtering tests completed.")
          : text("请查看检测证据和失败原因。", "Review the evidence and failure reason."),
        final.state === "completed" ? "success" : "warning",
      );
    } catch (error) {
      if (!mounted.current) return;
      const latest = await appServices.diagnostics.natLatest().catch(() => undefined);
      if (latest) setResult(latest);
      notify(text("NAT 检测失败", "NAT detection failed"), error instanceof Error ? error.message : String(error));
    }
  }, [notify, preview, selected, serverSnapshot.selected_id, text]);

  const cancel = useCallback(async () => {
    if (preview) {
      setResult((current) => ({ ...current, state: "cancelled", completed_at: new Date().toISOString() }));
      return;
    }
    try {
      await appServices.diagnostics.cancelNAT();
    } catch (error) {
      notify(text("取消失败", "Cancel failed"), error instanceof Error ? error.message : String(error));
    }
  }, [notify, preview, text]);

  const chooseServer = useCallback(async (id: string) => {
    if (preview) {
      setServerSnapshot((current) => ({ ...current, selected_id: id }));
      return;
    }
    setServerBusy(true);
    try {
      setServerSnapshot(await appServices.diagnostics.selectNATServer(id));
    } catch (error) {
      notify(text("无法选择服务器", "Unable to select server"), error instanceof Error ? error.message : String(error));
    } finally {
      setServerBusy(false);
    }
  }, [notify, preview, text]);

  const addServer = useCallback(async () => {
    if (!serverAddress.trim()) return;
    setServerBusy(true);
    try {
      if (preview) {
        const address = serverAddress.includes(":") ? serverAddress.trim() : `${serverAddress.trim()}:3478`;
        const id = `custom-${Date.now()}`;
        setServerSnapshot((current) => ({
          selected_id: id,
          servers: [...current.servers, { id, name: serverAddress.trim(), address, built_in: false }],
        }));
      } else {
        const added = await appServices.diagnostics.addNATServer("", serverAddress);
        const custom = added.servers[added.servers.length - 1];
        setServerSnapshot(custom ? await appServices.diagnostics.selectNATServer(custom.id) : added);
      }
      setServerAddress("");
      setAddingServer(false);
      notify(text("STUN 服务器已添加", "STUN server added"), text("新服务器已保存并设为当前选择。", "The new server was saved and selected."), "success");
    } catch (error) {
      notify(text("无法添加服务器", "Unable to add server"), error instanceof Error ? error.message : String(error));
    } finally {
      setServerBusy(false);
    }
  }, [notify, preview, serverAddress, text]);

  const removeServer = useCallback(async (id: string) => {
    setServerBusy(true);
    try {
      if (preview) {
        setServerSnapshot((current) => ({
          selected_id: current.selected_id === id ? "auto" : current.selected_id,
          servers: current.servers.filter((server) => server.id !== id),
        }));
      } else {
        setServerSnapshot(await appServices.diagnostics.removeNATServer(id));
      }
    } catch (error) {
      notify(text("无法删除服务器", "Unable to remove server"), error instanceof Error ? error.message : String(error));
    } finally {
      setServerBusy(false);
    }
  }, [notify, preview, text]);

  const resetServers = useCallback(async () => {
    setServerBusy(true);
    try {
      setServerSnapshot(preview ? previewServers() : await appServices.diagnostics.resetNATServers());
      notify(text("已恢复内置服务器", "Built-in servers restored"), text("服务器列表与自动选择顺序已恢复。", "The server list and automatic order were restored."), "success");
    } catch (error) {
      notify(text("无法恢复服务器", "Unable to restore servers"), error instanceof Error ? error.message : String(error));
    } finally {
      setServerBusy(false);
    }
  }, [notify, preview, text]);

  const allowFirewallAndRetry = useCallback(async () => {
    setFirewallBusy(true);
    try {
      if (!preview) await appServices.diagnostics.allowNATFirewallTraffic();
      notify(
        text("已允许 STUN 检测回包", "STUN probe replies allowed"),
        text("仅为 HypoMux 放行入站 UDP；现在重新检测。", "Inbound UDP was allowed only for HypoMux. Retesting now."),
        "success",
      );
      await run();
    } catch (error) {
      notify(text("无法调整防火墙", "Unable to update firewall"), error instanceof Error ? error.message : String(error), "error");
    } finally {
      setFirewallBusy(false);
    }
  }, [notify, preview, run, text]);

  const behaviorLabel = (behavior?: NATDetectionResult["mapping_behavior"]) => ({
    direct: text("无 NAT", "No NAT"),
    endpoint_independent: text("端点无关", "Endpoint-independent"),
    address_dependent: text("地址相关", "Address-dependent"),
    address_port_dependent: text("地址与端口相关", "Address-and-port-dependent"),
    inconclusive: text("无法判定", "Inconclusive"),
  })[behavior ?? "inconclusive"];

  const typeMeta = {
    direct: {
      label: text("公网直连", "Publicly reachable"), tone: "success" as const,
      summary: text("本机地址与 STUN 观察到的地址一致，没有检测到 NAT 地址转换。", "The local and STUN-observed endpoints match; no address translation was detected."),
    },
    full_cone: {
      label: text("全锥型 NAT", "Full-cone NAT"), tone: "success" as const,
      summary: text("映射和过滤均为端点无关，对打洞与点对点连接最友好。", "Mapping and filtering are endpoint-independent, the most P2P-friendly behavior."),
    },
    restricted_cone: {
      label: text("受限锥型 NAT", "Restricted-cone NAT"), tone: "informative" as const,
      summary: text("映射保持稳定，但入站响应需要来自曾访问过的远端地址。", "The mapping stays stable, while inbound traffic is limited to previously contacted addresses."),
    },
    port_restricted_cone: {
      label: text("端口受限锥型 NAT", "Port-restricted-cone NAT"), tone: "warning" as const,
      summary: text("映射保持稳定，但入站响应同时受远端地址和端口限制。", "The mapping stays stable, while inbound traffic is restricted by remote address and port."),
    },
    symmetric: {
      label: text("对称型 NAT", "Symmetric NAT"), tone: "danger" as const,
      summary: text("公网映射会随目标变化，点对点打洞更困难，部分场景需要中继。", "The public mapping changes with the destination, making P2P traversal harder and sometimes requiring relay."),
    },
    unknown: {
      label: text("混合型行为", "Mixed behavior"), tone: "warning" as const,
      summary: text("检测到了非典型的映射与过滤组合，请结合下方原始行为判断。", "A non-classic mapping/filtering combination was observed; review the raw behaviors below."),
    },
    inconclusive: {
      label: result.host_firewall_limited ? text("待校准", "Needs calibration") : text("无法判定", "Inconclusive"), tone: "warning" as const,
      summary: result.host_firewall_limited
        ? text("本机防火墙可能影响备用端点回包，放行 HypoMux 后重测即可得到可靠分类。", "The host firewall may affect alternate-endpoint replies. Allow HypoMux and retest for a reliable classification.")
        : text("当前证据不足，可能是 UDP 被阻断、超时，或 STUN 服务器不支持 RFC 5780。", "Evidence is insufficient. UDP may be blocked or timed out, or the STUN server may not support RFC 5780."),
    },
  }[result.nat_type ?? "inconclusive"];

  const detail = (() => {
    if (result.state === "cancelled") return text("检测已由用户取消。", "Detection was cancelled.");
    if (result.detail?.includes("OTHER-ADDRESS")) return text("STUN 服务器未提供 RFC 5780 所需的 OTHER-ADDRESS。", "The STUN server did not provide the RFC 5780 OTHER-ADDRESS attribute.");
    if (result.detail?.toLowerCase().includes("timeout")) return text("UDP 探测超时；请检查防火墙、公司网络或上游是否限制 STUN。", "UDP probes timed out; check firewall, corporate-network, or upstream STUN restrictions.");
    if (result.detail?.includes("fake-IP")) return text("域名被解析到 Fake-IP；请关闭 DNS 劫持，或改用 IP 地址形式的 STUN 服务器。", "The hostname resolved to a fake-IP; disable DNS interception or use an IP-based STUN endpoint.");
    if (result.host_firewall_limited) return text("Windows 防火墙可能拦截了 STUN 备用地址回包。重测前系统会请求管理员权限，放行规则仅作用于 HypoMux。", "Windows Firewall may have blocked alternate STUN replies. Windows will request administrator approval; the allow rule applies only to HypoMux.");
    if (result.detail?.includes("All configured")) return text("所有已配置 STUN 服务器均未完成检测，请查看下方逐项记录。", "No configured STUN server completed the test; review the attempt history below.");
    return result.detail || text("未返回更多细节。", "No additional detail was returned.");
  })();

  const attemptLabel = (code: string) => ({
    success: text("检测成功", "Succeeded"),
    timeout: text("3 秒内无响应", "No response within 3 seconds"),
    unsupported: text("不支持 RFC 5780", "RFC 5780 unsupported"),
    fake_ip: text("解析为 Fake-IP", "Resolved to fake-IP"),
    resolve_failed: text("域名解析失败", "DNS resolution failed"),
    invalid_response: text("响应格式不兼容", "Incompatible response"),
    network_error: text("网络错误", "Network error"),
    host_firewall: text("受本机防火墙影响", "Host firewall interference"),
  })[code] ?? code;

  const completed = result.completed_at
    ? new Intl.DateTimeFormat(undefined, { hour: "2-digit", minute: "2-digit", second: "2-digit" }).format(new Date(result.completed_at))
    : "—";

  return (
    <section className="nat-page" aria-live="polite">
      <GlassSurface className="nat-control" tone="secondary">
        <div className="nat-control-copy">
          <span className="nat-icon"><Globe20Regular /></span>
          <div>
            <strong>{text("选择检测出口", "Choose an egress adapter")}</strong>
            <span>{text("按出口检测，每次选择一张网卡。", "Test one adapter egress at a time.")}</span>
          </div>
        </div>
        <div className="nat-control-fields">
          <label>
            <span>{text("出口网卡", "Egress adapter")}</span>
            <Dropdown
              className="nat-adapter-dropdown"
              value={selected ? `${selected.name} · ${selected.address}` : ""}
              selectedOptions={adapterID ? [adapterID] : []}
              disabled={loading || running || eligible.length === 0}
              aria-label={text("检测出口网卡", "Egress adapter to test")}
              placeholder={text("请选择活动网卡", "Select an active adapter")}
              onOptionSelect={(_, data) => data.optionValue && setAdapterID(data.optionValue)}
            >
              {eligible.map((adapter) => (
                <Option key={adapter.id} value={adapter.id} text={`${adapter.name} · ${adapter.address}`}>
                  <span className="nat-adapter-option"><strong>{adapter.name}</strong><small>{adapter.address}</small></span>
                </Option>
              ))}
            </Dropdown>
          </label>
          <label className="nat-stun-field">
            <span>STUN</span>
            <div className="nat-stun-editor">
              {addingServer ? (
                <>
                  <Input autoFocus value={serverAddress} disabled={running || serverBusy}
                    placeholder="stun.example.com:3478" aria-label={text("新 STUN 服务器地址", "New STUN server address")}
                    onChange={(_, data) => setServerAddress(data.value)}
                    onKeyDown={(event) => { if (event.key === "Enter") void addServer(); if (event.key === "Escape") setAddingServer(false); }} />
                  <Button appearance="primary" icon={<Checkmark20Regular />} disabled={serverBusy || !serverAddress.trim()}
                    aria-label={text("保存 STUN 服务器", "Save STUN server")} title={text("保存", "Save")} onClick={addServer} />
                  <Button appearance="subtle" icon={<Dismiss20Regular />} disabled={serverBusy}
                    aria-label={text("取消添加", "Cancel adding")} title={text("取消", "Cancel")}
                    onClick={() => { setAddingServer(false); setServerAddress(""); }} />
                </>
              ) : (
                <>
                  <Dropdown
                    className="nat-adapter-dropdown"
                    value={serverSnapshot.selected_id === "auto"
                      ? text(`自动选择 · ${serverSnapshot.servers.length} 台`, `Automatic · ${serverSnapshot.servers.length} servers`)
                      : selectedServer?.address ?? ""}
                    selectedOptions={[serverSnapshot.selected_id]}
                    disabled={running || serverBusy}
                    aria-label={text("STUN 服务器", "STUN server")}
                    onOptionSelect={(_, data) => {
                      if (data.optionValue === "restore-builtins") void resetServers();
                      else if (data.optionValue) void chooseServer(data.optionValue);
                    }}
                  >
                    <Option value="auto">{text(`自动选择（依次尝试 ${serverSnapshot.servers.length} 台）`, `Automatic (${serverSnapshot.servers.length} servers in order)`)}</Option>
                    {serverSnapshot.servers.map((server) => (
                      <Option key={server.id} value={server.id} text={`${server.name} · ${server.address}`}>
                        <span className="nat-adapter-option"><strong>{server.name}</strong><small>{server.address}</small></span>
                      </Option>
                    ))}
                    <Option value="restore-builtins">{text("恢复内置服务器", "Restore built-in servers")}</Option>
                  </Dropdown>
                  <Button appearance="subtle" icon={<Add20Regular />} disabled={running || serverBusy}
                    aria-label={text("添加 STUN 服务器", "Add STUN server")} title={text("添加服务器", "Add server")}
                    onClick={() => setAddingServer(true)} />
                  <Button appearance="subtle" icon={<Delete20Regular />}
                    disabled={running || serverBusy || serverSnapshot.selected_id === "auto" || !selectedServer}
                    aria-label={text("删除当前 STUN 服务器", "Delete selected STUN server")}
                    title={text("删除当前服务器", "Delete selected server")}
                    onClick={() => selectedServer && void removeServer(selectedServer.id)} />
                </>
              )}
            </div>
          </label>
        </div>
        <div className="nat-control-actions">
          {running ? (
            <Button appearance="secondary" icon={<Stop20Regular />} onClick={cancel}>{text("取消检测", "Cancel")}</Button>
          ) : (
            <Button appearance="primary" icon={result.state === "idle" ? <Play20Regular /> : <ArrowSync20Regular />}
              disabled={loading || !selected || serverSnapshot.servers.length === 0} onClick={run}>
              {result.state === "idle" ? text("开始检测", "Start detection") : text("重新检测", "Test again")}
            </Button>
          )}
        </div>
      </GlassSurface>

      <div className="nat-content">
        {running ? (
          <GlassSurface className="nat-status-card nat-running" tone="secondary">
            <Spinner size="medium" />
            <div><strong>{text("正在分析 NAT 行为", "Analyzing NAT behavior")}</strong><span>{selected?.name} · {selected?.address}</span></div>
            <ol><li>{text("建立 STUN 基准映射", "Establishing baseline STUN mapping")}</li><li>{text("比较不同目标的公网映射", "Comparing mappings across destinations")}</li><li>{text("验证入站过滤条件", "Testing inbound filtering conditions")}</li></ol>
          </GlassSurface>
        ) : result.state === "idle" ? (
          <GlassSurface className="nat-status-card nat-idle" tone="secondary">
            <Globe20Regular />
            <div><strong>{text("尚未进行 NAT 类型检测", "No NAT detection yet")}</strong><span>{text("选择出口后，将发送少量 UDP STUN 探针，不会改变代理、路由或网卡配置。", "A few UDP STUN probes will be sent after selecting an egress. Proxy, routing, and adapter settings are not changed.")}</span></div>
          </GlassSurface>
        ) : result.state === "cancelled" ? (
          <GlassSurface className="nat-status-card nat-idle" tone="secondary">
            <Warning20Regular />
            <div><strong>{text("检测已取消", "Detection cancelled")}</strong><span>{detail}</span></div>
          </GlassSurface>
        ) : (
          <GlassSurface className={`nat-result-card nat-result-${typeMeta.tone}`} tone="secondary">
            <div className="nat-result-hero">
              <span className="nat-result-icon">{result.state === "completed" ? <CheckmarkCircle20Regular /> : <Warning20Regular />}</span>
              <div><span>{text("检测结果", "Detection result")}</span><strong>{typeMeta.label}</strong><p>{typeMeta.summary}</p></div>
              <Badge appearance="tint" color={typeMeta.tone}>{result.state === "completed" ? text("判定完成", "Completed") : text("证据不足", "Inconclusive")}</Badge>
            </div>
            <div className="nat-evidence-grid">
              <div><span>{text("映射行为", "Mapping behavior")}</span><strong>{behaviorLabel(result.mapping_behavior)}</strong></div>
              <div><span>{text("过滤行为", "Filtering behavior")}</span><strong>{behaviorLabel(result.filtering_behavior)}</strong></div>
              <div><span>{text("公网端点", "Public endpoint")}</span><strong>{result.public_endpoint || "—"}</strong></div>
              <div><span>{text("检测网卡", "Tested adapter")}</span><strong>{result.name || selected?.name || "—"}</strong><small>{result.address}</small></div>
              <div><span>STUN</span><strong>{result.server || "—"}</strong></div>
              <div><span>{text("耗时 / 完成时间", "Duration / completed")}</span><strong>{result.duration_ms ? `${(result.duration_ms / 1000).toFixed(2)} s` : "—"}</strong><small>{completed}</small></div>
            </div>
            {result.host_firewall_limited ? (
              <div className="nat-firewall-warning">
                <Warning20Regular />
                <span><strong>{text("本机防火墙影响了过滤测试", "Host firewall affected the filtering test")}</strong><small>{detail}</small></span>
                <Button appearance="secondary" size="small" disabled={firewallBusy} onClick={allowFirewallAndRetry}>
                  {firewallBusy ? text("正在处理…", "Working…") : text("允许并重测", "Allow and retest")}
                </Button>
              </div>
            ) : (result.state === "inconclusive" || result.nat_type === "unknown") && <div className="nat-detail"><Warning20Regular /><span>{detail}</span></div>}
            {(result.attempts?.length ?? 0) > 0 && result.state === "inconclusive" && (
              <div className="nat-attempts">
                <strong>{text("服务器尝试记录", "Server attempt history")}</strong>
                {result.attempts?.map((attempt) => (
                  <div key={`${attempt.server}-${attempt.duration_ms}`}>
                    <span><strong>{attempt.server}</strong><small>{attempt.resolved || text("未解析", "Unresolved")}</small></span>
                    <Badge appearance="tint" color={attempt.code === "unsupported" ? "warning" : "danger"}>{attemptLabel(attempt.code)}</Badge>
                    <small>{(attempt.duration_ms / 1000).toFixed(2)} s</small>
                  </div>
                ))}
              </div>
            )}
          </GlassSurface>
        )}

        <GlassSurface as="aside" className="nat-method-card" tone="secondary">
          <div><strong>{text("如何理解结果", "How to read the result")}</strong><span>{text("基于 RFC 5780 的 UDP 映射与过滤行为快照", "A snapshot of UDP mapping and filtering behavior based on RFC 5780")}</span></div>
          <ul>
            <li><i className="nat-dot nat-dot-good" /><span><strong>{text("全锥型", "Full cone")}</strong>{text("端点无关映射与过滤", "Endpoint-independent mapping and filtering")}</span></li>
            <li><i className="nat-dot nat-dot-info" /><span><strong>{text("受限 / 端口受限", "Restricted / port-restricted")}</strong>{text("映射稳定，入站条件更严格", "Stable mapping with stricter inbound conditions")}</span></li>
            <li><i className="nat-dot nat-dot-warn" /><span><strong>{text("对称型", "Symmetric")}</strong>{text("映射随目标变化，穿透更困难", "Mapping changes by destination; traversal is harder")}</span></li>
          </ul>
          <p>{text("检测仅描述当前网卡、当前网络下的 UDP 行为，不代表防火墙整体安全等级，也不会影响现有代理分流。", "This describes current UDP behavior for the selected adapter and network. It is not a firewall security grade and does not affect proxy routing.")}</p>
        </GlassSurface>
      </div>
    </section>
  );
}
