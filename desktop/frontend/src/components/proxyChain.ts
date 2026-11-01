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
// When direct traffic is aggregated too, local destinations must stay truly
// direct: the engine dials through a scheduled NIC, which cannot reach a LAN
// that sits behind a different adapter.
export const CHAIN_LOCAL_RULES = [
  "IP-CIDR,127.0.0.0/8", "IP-CIDR,10.0.0.0/8", "IP-CIDR,172.16.0.0/12", "IP-CIDR,192.168.0.0/16",
  "IP-CIDR,169.254.0.0/16", "IP-CIDR,100.64.0.0/10", "IP-CIDR,224.0.0.0/4", "IP-CIDR,255.255.255.255/32",
  "IP-CIDR6,::1/128", "IP-CIDR6,fc00::/7", "IP-CIDR6,fe80::/10", "IP-CIDR6,ff00::/8",
].map((rule) => `${rule},DIRECT,no-resolve`).concat("DOMAIN-SUFFIX,local,DIRECT");

export function buildMihomoChainScript(socksPort: number, aggregateDirect = true): string {
  return `// HypoMux 多网卡链式出口 · HypoMux multi-NIC chain egress
// 适用于 Clash Verge Rev / Clash Nyanpasu / Mihomo Party 等 Mihomo 内核客户端的全局扩展脚本。
// HypoMux 停止时自动回落直连，无需改回配置。
const HYPOMUX_PROXY = ${JSON.stringify(CHAIN_PROXY_NAME)};
const HYPOMUX_GROUP = ${JSON.stringify(CHAIN_GROUP_NAME)};
const HYPOMUX_PORT = ${socksPort};
// 为 true 时，规则里的直连流量也经 HypoMux 分到多张网卡；局域网地址与按进程、来源、入站指定的直连保持不变。
const AGGREGATE_DIRECT = ${aggregateDirect};
const SKIP_TYPES = ["direct", "reject", "reject-drop", "pass", "dns", "compatible"];
// 这些规则是用户对某个程序、设备或入站的明确指定，命中 DIRECT 时保持真正直连。
const KEEP_DIRECT_TYPES = ["PROCESS-NAME", "PROCESS-PATH", "PROCESS-NAME-REGEX", "PROCESS-PATH-REGEX",
  "PROCESS-NAME-WILDCARD", "PROCESS-PATH-WILDCARD", "UID", "IN-TYPE", "IN-USER", "IN-NAME", "IN-PORT",
  "SRC-IP-CIDR", "SRC-IP-SUFFIX", "SRC-IP-ASN", "SRC-GEOIP", "SRC-PORT", "SUB-RULE"];
const LOCAL_PAYLOADS = ["LAN", "PRIVATE"];
const BYPASS_RULES = ${JSON.stringify(CHAIN_BYPASS_PROCESSES.map((name) => `PROCESS-NAME,${name},DIRECT`))};
const LOCAL_RULES = ${JSON.stringify(CHAIN_LOCAL_RULES)};

// 按顶层逗号拆分规则，AND/OR/NOT 逻辑规则括号里的逗号不拆。
function splitRule(rule) {
  const parts = [];
  let depth = 0;
  let start = 0;
  for (let index = 0; index < rule.length; index++) {
    const char = rule[index];
    if (char === "(") depth++;
    else if (char === ")") depth--;
    else if (char === "," && depth === 0) {
      parts.push(rule.slice(start, index));
      start = index + 1;
    }
  }
  parts.push(rule.slice(start));
  return parts;
}

function rewriteDirect(rule) {
  if (typeof rule !== "string") return rule;
  const parts = splitRule(rule);
  const type = parts[0].trim().toUpperCase();
  const target = type === "MATCH" ? 1 : 2;
  if (parts.length <= target || parts[target].trim().toUpperCase() !== "DIRECT") return rule;
  if (KEEP_DIRECT_TYPES.includes(type) || (type !== "MATCH" && LOCAL_PAYLOADS.includes(parts[1].trim().toUpperCase()))) return rule;
  parts[target] = HYPOMUX_GROUP;
  return parts.join(",");
}

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
  if (AGGREGATE_DIRECT) {
    for (const group of groups) {
      const members = Array.isArray(group.proxies) ? group.proxies : [];
      const direct = members.indexOf("DIRECT");
      if (direct < 0 || members.includes(HYPOMUX_GROUP)) continue;
      // 手选分组只新增选项，不改用户已选中的 DIRECT；自动测速/回退类分组直接替换。
      if (String(group.type || "").toLowerCase() === "select") members.splice(direct, 0, HYPOMUX_GROUP);
      else members[direct] = HYPOMUX_GROUP;
    }
  }
  groups.push({
    name: HYPOMUX_GROUP, type: "fallback", proxies: [HYPOMUX_PROXY, "DIRECT"],
    url: ${JSON.stringify(CHAIN_HEALTH_URL)}, "expected-status": 200,
    interval: 30, timeout: 3000, lazy: false, hidden: true,
  });
  config["proxy-groups"] = groups;

  const reservedRules = [...BYPASS_RULES, ...LOCAL_RULES];
  const prepareRules = (list) => {
    const kept = (Array.isArray(list) ? list : []).filter((rule) => !reservedRules.includes(rule));
    return AGGREGATE_DIRECT ? kept.map(rewriteDirect) : kept;
  };
  config.rules = [...BYPASS_RULES, ...(AGGREGATE_DIRECT ? LOCAL_RULES : []), ...prepareRules(config.rules)];
  const subRules = config["sub-rules"];
  if (AGGREGATE_DIRECT && subRules && typeof subRules === "object") {
    for (const name of Object.keys(subRules)) subRules[name] = prepareRules(subRules[name]);
  }
  return config;
}
`;
}

// sing-box has no priority-fallback group, so its nodes detour straight to the
// SOCKS5 port and need HypoMux running. The snippet is plain JSON because
// sing-box rejects comments and unknown fields; the dialog carries the steps.
export function buildSingBoxChainJSON(socksPort: number): string {
  return `${JSON.stringify({
    outbounds: [{ type: "socks", tag: CHAIN_PROXY_NAME.toLowerCase(), server: "127.0.0.1", server_port: socksPort, version: "5" }],
  }, null, 2)}\n`;
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
  # 让直连流量也参与聚合：保留下面的局域网规则，并把原有规则里的 DIRECT 改为 ${CHAIN_GROUP_NAME}
  # （按进程、来源、入站指定的 DIRECT 以及 GEOIP,LAN 保持不变）。
${CHAIN_LOCAL_RULES.map((rule) => `  - ${rule}`).join("\n")}
  # ……原有规则保持在这几行之后
`;
}
