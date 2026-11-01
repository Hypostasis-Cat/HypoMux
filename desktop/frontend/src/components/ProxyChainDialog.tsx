import { Button, Checkbox, Dialog, DialogActions, DialogBody, DialogContent, DialogSurface, DialogTitle, MessageBar, MessageBarBody, Tab, TabList, Textarea } from "@fluentui/react-components";
import { useEffect, useState } from "react";
import { useI18n } from "../i18n/i18n";
import { appServices } from "../platform/services";
import { buildMihomoChainScript, buildMihomoChainYAML, buildSingBoxChainJSON } from "./proxyChain";
import { addChainTunProcessRules, summarizeChainConnections, type ChainStatus, type ChainTunResult } from "./proxyChainTun";

type ChainFormat = "script" | "yaml" | "singbox";

const chainBuilders: Record<ChainFormat, (port: number, aggregateDirect: boolean) => string> = {
  script: buildMihomoChainScript,
  yaml: (port) => buildMihomoChainYAML(port),
  singbox: (port) => buildSingBoxChainJSON(port),
};

const CHAIN_STATUS_POLL_MS = 3000;

type ProxyChainDialogProps = {
  open: boolean;
  socksPort: number;
  systemProxyTakeover: boolean;
  busy: boolean;
  onClose: () => void;
  onEnableChainMode: () => Promise<unknown>;
};

// ProxyChainDialog walks the user through chain mode: HypoMux releases the
// Windows system proxy, and the Clash/Mihomo client sends its node
// connections through HypoMux's SOCKS5 port.
export function ProxyChainDialog({ open, socksPort, systemProxyTakeover, busy, onClose, onEnableChainMode }: ProxyChainDialogProps) {
  const { locale } = useI18n();
  const text = (zh: string, en: string) => locale === "en" ? en : zh;
  const [format, setFormat] = useState<ChainFormat>("script");
  const [copied, setCopied] = useState<ChainFormat | "">("");
  const [copyError, setCopyError] = useState("");
  const [tunBusy, setTunBusy] = useState(false);
  const [tunResult, setTunResult] = useState<ChainTunResult | null>(null);
  const [tunError, setTunError] = useState("");
  const [aggregateDirect, setAggregateDirect] = useState(true);
  const [status, setStatus] = useState<ChainStatus | null>(null);
  const content = chainBuilders[format](socksPort, aggregateDirect);

  useEffect(() => {
    if (!open) return;
    let cancelled = false;
    let timer: number | undefined;
    const poll = async () => {
      try {
        const next = summarizeChainConnections(await appServices.engine.connections());
        if (!cancelled) setStatus(next);
      } catch {
        if (!cancelled) setStatus({ state: "stopped" });
      }
      if (!cancelled) timer = window.setTimeout(() => void poll(), CHAIN_STATUS_POLL_MS);
    };
    void poll();
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, [open]);
  const formatHint: Record<ChainFormat, string> = {
    script: text(
      "Clash Verge Rev：订阅 → 全局扩展脚本；Mihomo Party / Clash Nyanpasu / FlClash：覆写里新建 JavaScript 脚本并启用。保存后重新应用配置。HypoMux 停止时约 30 秒内自动回落直连节点，无需改回配置。",
      "Clash Verge Rev: Profiles → Global Extend Script. Mihomo Party / Clash Nyanpasu / FlClash: add a JavaScript override and enable it. Reapply the profile after saving. When HypoMux stops, nodes fall back to direct within about 30 seconds with no reconfiguration.",
    ),
    yaml: text(
      "手写 Mihomo 配置或客户端不支持脚本时：合并下面的内容，并按注释给节点或订阅加上 dialer-proxy。",
      "For hand-written Mihomo configs or clients without scripts: merge the snippet and add dialer-proxy to nodes or providers as commented.",
    ),
    singbox: text(
      "sing-box 及 GUI.for.SingBox 等：把下面的出站加入 outbounds，并给每个节点出站加上 \"detour\": \"hypomux\"。sing-box 没有自动回落，HypoMux 停止时请先去掉 detour。",
      "sing-box and front ends such as GUI.for.SingBox: add this outbound to outbounds and set \"detour\": \"hypomux\" on every node outbound. sing-box has no automatic fallback, so remove the detour while HypoMux is stopped.",
    ),
  };

  const addTunRules = async () => {
    setTunBusy(true);
    setTunError("");
    setTunResult(null);
    try {
      setTunResult(await addChainTunProcessRules(appServices.routing));
    } catch (error) {
      setTunError(error instanceof Error ? error.message : String(error));
    } finally {
      setTunBusy(false);
    }
  };

  const copy = async () => {
    setCopyError("");
    try {
      await navigator.clipboard.writeText(content);
      setCopied(format);
    } catch (error) {
      setCopyError(String(error));
    }
  };

  return (
    <Dialog open={open} onOpenChange={(_, data) => { if (!data.open) onClose(); }}>
      <DialogSurface className="glass-surface proxy-chain-dialog" data-tone="primary"><DialogBody>
        <DialogTitle>{text("与 Clash 等代理软件联动", "Use with Clash and other proxy clients")}</DialogTitle>
        <DialogContent className="proxy-chain-content">
          <p>{text(
            "联动后，代理软件继续负责系统代理、TUN、节点和分流规则；它连接节点的每条连接再交给 HypoMux 分配到不同网卡。两者不再争抢系统设置，也不会互相回环。",
            "In chain mode, the proxy client keeps the system proxy, its TUN, nodes and rules, and hands each node connection to HypoMux, which spreads them across your adapters. Neither app competes for system settings and no loop is formed.",
          )}</p>
          {status && <ChainStatusMessage status={status} text={text} />}
          <ol className="proxy-chain-steps">
            <li>
              <strong>{text("HypoMux 让出系统代理", "Release the system proxy in HypoMux")}</strong>
              <span>{systemProxyTakeover
                ? text("关闭“管理 Windows 系统代理”，之后以系统代理模式启动聚合。", "Turn off Windows system proxy management, then start aggregation in System Proxy mode.")
                : text("已关闭系统代理接管。请以系统代理模式启动聚合。", "System proxy management is off. Start aggregation in System Proxy mode.")}</span>
              {systemProxyTakeover && <Button size="small" appearance="primary" disabled={busy} onClick={() => void onEnableChainMode()}>
                {text("关闭系统代理接管", "Turn off system proxy management")}
              </Button>}
            </li>
            <li>
              <strong>{text("把下面的配置交给代理软件", "Give the proxy client the configuration below")}</strong>
              <span>{formatHint[format]}</span>
            </li>
            <li>
              <strong>{text("其他客户端", "Other clients")}</strong>
              <span>{text(
                `v2rayN、Hiddify、NekoBox 等：若支持“前置代理 / 链式代理”，把 SOCKS5 127.0.0.1:${socksPort} 设为前置；否则使用下方的 TUN 方案，无需改动客户端配置。`,
                `v2rayN, Hiddify, NekoBox and others: if the client supports an upstream or chained proxy, set SOCKS5 127.0.0.1:${socksPort} as the first hop; otherwise use the TUN option below, which needs no client changes.`,
              )}</span>
            </li>
          </ol>
          <TabList size="small" selectedValue={format} onTabSelect={(_, data) => setFormat(data.value as ChainFormat)}>
            <Tab value="script">{text("Mihomo 脚本（推荐）", "Mihomo script (recommended)")}</Tab>
            <Tab value="yaml">{text("Mihomo YAML", "Mihomo YAML")}</Tab>
            <Tab value="singbox">sing-box</Tab>
          </TabList>
          {format === "script" && <Checkbox checked={aggregateDirect} onChange={(_, data) => setAggregateDirect(Boolean(data.checked))}
            label={text(
              "直连流量也参与聚合（局域网地址，以及按进程、来源或入站指定的直连保持不变）",
              "Aggregate direct traffic too (LAN addresses and direct rules pinned by process, source or inbound stay direct)",
            )} />}
          <Textarea className="proxy-chain-code" readOnly resize="vertical" value={content}
            aria-label={text("联动配置", "Chain configuration")} />
          {copied === format && <MessageBar intent="success"><MessageBarBody>{text("已复制到剪贴板。", "Copied to the clipboard.")}</MessageBarBody></MessageBar>}
          {copyError && <MessageBar intent="error"><MessageBarBody>{copyError}</MessageBarBody></MessageBar>}
          <p className="proxy-chain-note">{text(
            `修改 HypoMux 的 SOCKS5 端口（当前 ${socksPort}）后需要重新复制配置。sing-box 和其他客户端只聚合节点流量，规则里的直连流量不参与。`,
            `Copy the configuration again after changing HypoMux's SOCKS5 port (currently ${socksPort}). sing-box and other clients aggregate node traffic only; traffic their rules send direct is not aggregated.`,
          )}</p>
          <section className="proxy-chain-tun" aria-labelledby="proxy-chain-tun-heading">
            <strong id="proxy-chain-tun-heading">{text("HypoMux 使用 TUN 模式时", "When HypoMux uses TUN mode")}</strong>
            <span>{text(
              "两个 TUN 不能同时接管默认路由：请关闭代理软件的 TUN，只保留它的系统代理。HypoMux 默认让代理软件直连；点下面的按钮会为正在运行的代理内核（Mihomo、FlClash、Xray/V2Ray、Hiddify、NekoBox）添加“多卡聚合”进程规则，它连节点的流量就会被 HypoMux TUN 分到多张网卡。sing-box 内核与 HypoMux 自带内核同名，无法这样区分，请改用上面的 sing-box 配置。",
              "Two TUNs cannot both own the default route: turn off the client's TUN and keep only its system proxy. HypoMux sends proxy clients direct by default; the button below adds aggregation process rules for running proxy cores (Mihomo, FlClash, Xray/V2Ray, Hiddify, NekoBox) so HypoMux's TUN spreads their node connections across adapters. sing-box cores share their name with HypoMux's own core and cannot be told apart, so use the sing-box configuration above instead.",
            )}</span>
            <Button size="small" disabled={tunBusy} onClick={() => void addTunRules()}>
              {tunBusy ? text("正在添加…", "Adding…") : text("让代理内核参与聚合", "Aggregate proxy cores")}
            </Button>
            {tunResult && <ChainTunMessage result={tunResult} text={text} />}
            {tunError && <MessageBar intent="error"><MessageBarBody>{tunError}</MessageBarBody></MessageBar>}
          </section>
        </DialogContent>
        <DialogActions>
          <Button onClick={onClose}>{text("关闭", "Close")}</Button>
          <Button appearance="primary" onClick={() => void copy()}>{text("复制配置", "Copy configuration")}</Button>
        </DialogActions>
      </DialogBody></DialogSurface>
    </Dialog>
  );
}

function ChainTunMessage({ result, text }: { result: ChainTunResult; text: (zh: string, en: string) => string }) {
  if (result.detected.length === 0) {
    return <MessageBar intent="warning"><MessageBarBody>{text(
      "未检测到正在运行的代理内核（如 verge-mihomo.exe、mihomo.exe、xray.exe）。请先启动代理软件再试。",
      "No running proxy core (such as verge-mihomo.exe, mihomo.exe or xray.exe) was found. Start the proxy client and try again.",
    )}</MessageBarBody></MessageBar>;
  }
  const lines: string[] = [];
  if (result.added.length > 0) {
    lines.push(text(`已添加多卡聚合规则：${result.added.join("、")}。`, `Added aggregation rules: ${result.added.join(", ")}.`));
    lines.push(result.restartRequired
      ? text("重启聚合后生效。", "Restart aggregation to apply.")
      : text("规则已生效。", "The rules are active."));
  }
  if (result.existing.length > 0) {
    lines.push(text(`规则已存在：${result.existing.join("、")}。`, `Already routed to aggregation: ${result.existing.join(", ")}.`));
  }
  if (result.conflicts.length > 0) {
    const names = result.conflicts.map((item) => item.name);
    lines.push(text(
      `${names.join("、")} 已有指向其他出口的规则，未作修改；如需聚合，请在路由规则中调整。`,
      `${names.join(", ")} already route to another outbound and were left unchanged; adjust them in Routing if you want aggregation.`,
    ));
  }
  const intent = result.conflicts.length > 0 ? "warning" : result.added.length > 0 ? "success" : "info";
  return <MessageBar intent={intent}><MessageBarBody>{lines.join(" ")}</MessageBarBody></MessageBar>;
}

function ChainStatusMessage({ status, text }: { status: ChainStatus; text: (zh: string, en: string) => string }) {
  if (status.state === "active") {
    const adapters = status.adapters.length > 0 ? text(`，分布在 ${status.adapters.join("、")}`, ` across ${status.adapters.join(", ")}`) : "";
    return <MessageBar intent="success"><MessageBarBody>{text(
      `联动已生效：${status.processes.join("、")} 有 ${status.connections} 条连接正经 HypoMux 出站${adapters}。`,
      `Chain is active: ${status.processes.join(", ")} has ${status.connections} connection(s) leaving through HypoMux${adapters}.`,
    )}</MessageBarBody></MessageBar>;
  }
  if (status.state === "waiting") {
    return <MessageBar intent="info"><MessageBarBody>{text(
      "聚合正在运行，但尚未检测到代理软件经 HypoMux 出站。完成下面的设置后，用代理软件访问一次网络即可确认。",
      "Aggregation is running, but no proxy client is leaving through HypoMux yet. Finish the steps below and browse once through the client to confirm.",
    )}</MessageBarBody></MessageBar>;
  }
  return <MessageBar intent="warning"><MessageBarBody>{text(
    "聚合未运行，暂时无法检测联动状态。",
    "Aggregation is not running, so the chain status cannot be checked yet.",
  )}</MessageBarBody></MessageBar>;
}
