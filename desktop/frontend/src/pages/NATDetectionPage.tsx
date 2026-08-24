import {
  Badge,
  Button,
  Dropdown,
  Option,
  Spinner,
} from "@fluentui/react-components";
import {
  ArrowSync20Regular,
  CheckmarkCircle20Regular,
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

export function NATDetectionPage({ adapters, loading, preview, text, notify }: NATDetectionPageProps) {
  const eligible = useMemo(
    () => adapters.filter((adapter) => adapter.operational && Boolean(adapter.address)),
    [adapters],
  );
  const [adapterID, setAdapterID] = useState("");
  const [result, setResult] = useState<NATDetectionResult>(emptyResult);
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
      setResult(preferred ? previewResult(preferred) : emptyResult());
      return;
    }
    void appServices.diagnostics.natLatest()
      .then((latest) => { if (mounted.current) setResult(latest ?? emptyResult()); })
      .catch(() => { /* A missing persisted result is equivalent to idle. */ });
  }, [eligible, preview]);

  useEffect(() => {
    if (preview || result.state !== "running") return;
    return startSerialPoll(async () => {
      const latest = await appServices.diagnostics.natLatest();
      if (mounted.current) setResult(latest);
    }, 400);
  }, [preview, result.state]);

  const running = result.state === "running";
  const selected = eligible.find((adapter) => adapter.id === adapterID);

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
      const final = await appServices.diagnostics.runNAT(selected.id);
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
  }, [notify, preview, selected, text]);

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
      label: text("无法判定", "Inconclusive"), tone: "warning" as const,
      summary: text("当前证据不足，可能是 UDP 被阻断、超时，或 STUN 服务器不支持 RFC 5780。", "Evidence is insufficient. UDP may be blocked or timed out, or the STUN server may not support RFC 5780."),
    },
  }[result.nat_type ?? "inconclusive"];

  const detail = (() => {
    if (result.state === "cancelled") return text("检测已由用户取消。", "Detection was cancelled.");
    if (result.detail?.includes("OTHER-ADDRESS")) return text("STUN 服务器未提供 RFC 5780 所需的 OTHER-ADDRESS。", "The STUN server did not provide the RFC 5780 OTHER-ADDRESS attribute.");
    if (result.detail?.toLowerCase().includes("timeout")) return text("UDP 探测超时；请检查防火墙、公司网络或上游是否限制 STUN。", "UDP probes timed out; check firewall, corporate-network, or upstream STUN restrictions.");
    return result.detail || text("未返回更多细节。", "No additional detail was returned.");
  })();

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
            <span>{text("每张网卡可能位于不同 NAT 后方，因此一次只检测一个出口。", "Each adapter may sit behind a different NAT, so one egress is tested at a time.")}</span>
          </div>
        </div>
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
        {running ? (
          <Button appearance="secondary" icon={<Stop20Regular />} onClick={cancel}>{text("取消检测", "Cancel")}</Button>
        ) : (
          <Button appearance="primary" icon={result.state === "idle" ? <Play20Regular /> : <ArrowSync20Regular />}
            disabled={loading || !selected} onClick={run}>
            {result.state === "idle" ? text("开始检测", "Start detection") : text("重新检测", "Test again")}
          </Button>
        )}
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
            {(result.state === "inconclusive" || result.nat_type === "unknown") && <div className="nat-detail"><Warning20Regular /><span>{detail}</span></div>}
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
