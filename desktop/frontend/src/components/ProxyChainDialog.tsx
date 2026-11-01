import { Button, Dialog, DialogActions, DialogBody, DialogContent, DialogSurface, DialogTitle, MessageBar, MessageBarBody, Tab, TabList, Textarea } from "@fluentui/react-components";
import { useState } from "react";
import { useI18n } from "../i18n/i18n";
import { buildMihomoChainScript, buildMihomoChainYAML } from "./proxyChain";

type ChainFormat = "script" | "yaml";

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
  const content = format === "script" ? buildMihomoChainScript(socksPort) : buildMihomoChainYAML(socksPort);

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
            "联动后，Clash 继续负责系统代理、TUN、节点和分流规则；它连接节点的每条连接再交给 HypoMux 分配到不同网卡。两者不再争抢系统设置，也不会互相回环。",
            "In chain mode, Clash keeps the system proxy, its TUN, nodes and rules, and hands each node connection to HypoMux, which spreads them across your adapters. Neither app competes for system settings and no loop is formed.",
          )}</p>
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
              <strong>{text("把下面的配置交给 Clash", "Give Clash the configuration below")}</strong>
              <span>{format === "script"
                ? text("Clash Verge Rev：订阅 → 全局扩展脚本；Mihomo Party / Clash Nyanpasu：覆写 → 新建 JavaScript 覆写并启用。保存后重新应用配置。", "Clash Verge Rev: Profiles → Global Extend Script. Mihomo Party / Clash Nyanpasu: Overrides → new JavaScript override, then enable it. Reapply the profile after saving.")
                : text("手写 Mihomo 配置：合并下面的内容，并按注释给节点或订阅加上 dialer-proxy。", "Hand-written Mihomo config: merge the snippet and add dialer-proxy to nodes or providers as commented.")}</span>
            </li>
            <li>
              <strong>{text("照常使用 Clash", "Keep using Clash as usual")}</strong>
              <span>{text(
                "HypoMux 停止时，Clash 会在约 30 秒内自动回落直连节点；HypoMux 启动后自动恢复多网卡出口，无需改回配置。",
                "When HypoMux stops, Clash falls back to connecting nodes directly within about 30 seconds and returns to the multi-NIC egress once HypoMux starts again; no reconfiguration is needed.",
              )}</span>
            </li>
          </ol>
          <TabList size="small" selectedValue={format} onTabSelect={(_, data) => setFormat(data.value as ChainFormat)}>
            <Tab value="script">{text("扩展脚本（推荐）", "Extend script (recommended)")}</Tab>
            <Tab value="yaml">{text("Mihomo YAML", "Mihomo YAML")}</Tab>
          </TabList>
          <Textarea className="proxy-chain-code" readOnly resize="vertical" value={content}
            aria-label={text("联动配置", "Chain configuration")} />
          {copied === format && <MessageBar intent="success"><MessageBarBody>{text("已复制到剪贴板。", "Copied to the clipboard.")}</MessageBarBody></MessageBar>}
          {copyError && <MessageBar intent="error"><MessageBarBody>{copyError}</MessageBarBody></MessageBar>}
          <p className="proxy-chain-note">{text(
            `修改 HypoMux 的 SOCKS5 端口（当前 ${socksPort}）后需要重新复制配置。Clash 规则中走 DIRECT 的流量不经过节点，因此不参与聚合。`,
            `Copy the configuration again after changing HypoMux's SOCKS5 port (currently ${socksPort}). Traffic that Clash rules send DIRECT does not use a node and is not aggregated.`,
          )}</p>
        </DialogContent>
        <DialogActions>
          <Button onClick={onClose}>{text("关闭", "Close")}</Button>
          <Button appearance="primary" onClick={() => void copy()}>{text("复制配置", "Copy configuration")}</Button>
        </DialogActions>
      </DialogBody></DialogSurface>
    </Dialog>
  );
}
