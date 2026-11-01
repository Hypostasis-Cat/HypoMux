// Chain mode lets a Clash/Mihomo client keep the Windows system proxy, its TUN
// and its rules while HypoMux only carries the client's node connections. Each
// node gets `dialer-proxy` pointing at a hidden fallback group whose first
// member is HypoMux's local SOCKS5 port, so per-connection NIC aggregation
// applies to proxy traffic and neither app competes for system settings. The
// group falls back to DIRECT while HypoMux is stopped, so the client keeps
// working without editing its configuration again.

export const CHAIN_PROXY_NAME = "HypoMux";
export const CHAIN_GROUP_NAME = "HypoMux-Egress";
// Microsoft NCSI is reachable in every region and answers 200 with a tiny body.
export const CHAIN_HEALTH_URL = "http://www.msftconnecttest.com/connecttest.txt";
// HypoMux's own sockets are already bound to physical NICs; these rules only
// keep a third-party TUN from looping them back into the chain.
export const CHAIN_BYPASS_PROCESSES = ["hypomux-engine.exe", "HypoMux.exe"];

export function buildMihomoChainScript(socksPort: number): string {
  return `// HypoMux 多网卡链式出口 · HypoMux multi-NIC chain egress
// 适用于 Clash Verge Rev / Clash Nyanpasu / Mihomo Party 等 Mihomo 内核客户端的全局扩展脚本。
// HypoMux 停止时自动回落直连，无需改回配置。
const HYPOMUX_PROXY = ${JSON.stringify(CHAIN_PROXY_NAME)};
const HYPOMUX_GROUP = ${JSON.stringify(CHAIN_GROUP_NAME)};
const HYPOMUX_PORT = ${socksPort};
const SKIP_TYPES = ["direct", "reject", "reject-drop", "pass", "dns", "compatible"];

function main(config) {
  const reserved = [HYPOMUX_PROXY, HYPOMUX_GROUP];
  const proxies = (Array.isArray(config.proxies) ? config.proxies : [])
    .filter((proxy) => proxy && !reserved.includes(proxy.name));
  for (const proxy of proxies) {
    // 已有 dialer-proxy 的节点是用户自建链路的后一跳，只给第一跳加前置。
    if (SKIP_TYPES.includes(String(proxy.type || "").toLowerCase()) || proxy["dialer-proxy"]) continue;
    proxy["dialer-proxy"] = HYPOMUX_GROUP;
  }
  proxies.push({ name: HYPOMUX_PROXY, type: "socks5", server: "127.0.0.1", port: HYPOMUX_PORT, udp: true });
  config.proxies = proxies;

  const providers = config["proxy-providers"];
  if (providers && typeof providers === "object") {
    for (const provider of Object.values(providers)) {
      if (!provider || typeof provider !== "object") continue;
      const override = provider.override && typeof provider.override === "object" ? provider.override : {};
      if (!override["dialer-proxy"]) override["dialer-proxy"] = HYPOMUX_GROUP;
      provider.override = override;
    }
  }

  const groups = (Array.isArray(config["proxy-groups"]) ? config["proxy-groups"] : [])
    .filter((group) => group && !reserved.includes(group.name));
  groups.push({
    name: HYPOMUX_GROUP, type: "fallback", proxies: [HYPOMUX_PROXY, "DIRECT"],
    url: ${JSON.stringify(CHAIN_HEALTH_URL)}, "expected-status": 200,
    interval: 30, timeout: 3000, lazy: false, hidden: true,
  });
  config["proxy-groups"] = groups;

  const bypass = ${JSON.stringify(CHAIN_BYPASS_PROCESSES.map((name) => `PROCESS-NAME,${name},DIRECT`))};
  const rules = (Array.isArray(config.rules) ? config.rules : []).filter((rule) => !bypass.includes(rule));
  config.rules = [...bypass, ...rules];
  return config;
}
`;
}

export function buildMihomoChainYAML(socksPort: number): string {
  return `# HypoMux 多网卡链式出口 · 合并到 Mihomo / Clash Meta 配置
proxies:
  - name: ${CHAIN_PROXY_NAME}
    type: socks5
    server: 127.0.0.1
    port: ${socksPort}
    udp: true
  # 在每个已有节点上追加一行（直连、拒绝类节点除外）：
  #   dialer-proxy: ${CHAIN_GROUP_NAME}

proxy-groups:
  - name: ${CHAIN_GROUP_NAME}
    type: fallback
    proxies: [${CHAIN_PROXY_NAME}, DIRECT]
    url: ${CHAIN_HEALTH_URL}
    expected-status: 200
    interval: 30
    timeout: 3000
    lazy: false
    hidden: true

# 订阅节点（proxy-providers）在各自条目中加入：
#   override:
#     dialer-proxy: ${CHAIN_GROUP_NAME}

rules:
${CHAIN_BYPASS_PROCESSES.map((name) => `  - PROCESS-NAME,${name},DIRECT`).join("\n")}
  # ……原有规则保持在这几行之后
`;
}
