import { describe, expect, it } from "vitest";
import { buildMihomoChainScript, buildMihomoChainYAML, buildSingBoxChainJSON, CHAIN_GROUP_NAME, CHAIN_LOCAL_RULES, CHAIN_PROXY_NAME } from "./proxyChain";

type Config = Record<string, any>;

const runScript = (port: number, config: Config, aggregateDirect = true): Config =>
  // The script runs inside the client's JavaScript sandbox, so execute it the same way.
  new Function(`${buildMihomoChainScript(port, aggregateDirect)}\nreturn main;`)()(config);

describe("Mihomo chain script", () => {
  it("chains every node through the hidden HypoMux fallback group", () => {
    const result = runScript(12345, {
      proxies: [
        { name: "HK", type: "ss", server: "hk.example", port: 443 },
        { name: "Direct", type: "direct" },
        { name: "Exit", type: "vless", server: "exit.example", port: 443, "dialer-proxy": "HK" },
      ],
      "proxy-groups": [{ name: "Proxy", type: "select", proxies: ["HK", "Exit"] }],
      rules: ["MATCH,Proxy"],
    });

    expect(result.proxies.find((proxy: Config) => proxy.name === "HK")["dialer-proxy"]).toBe(CHAIN_GROUP_NAME);
    expect(result.proxies.find((proxy: Config) => proxy.name === "Direct")["dialer-proxy"]).toBeUndefined();
    // A user-built chain keeps its second hop; the first hop already egresses through HypoMux.
    expect(result.proxies.find((proxy: Config) => proxy.name === "Exit")["dialer-proxy"]).toBe("HK");
    expect(result.proxies.at(-1)).toEqual({ name: CHAIN_PROXY_NAME, type: "socks5", server: "127.0.0.1", port: 12345, udp: true });
    const group = result["proxy-groups"].at(-1);
    expect(group).toMatchObject({ name: CHAIN_GROUP_NAME, type: "fallback", proxies: [CHAIN_PROXY_NAME, "DIRECT"], hidden: true });
    expect(result.rules).toEqual([
      "PROCESS-NAME,hypomux-engine.exe,DIRECT",
      "PROCESS-NAME,HypoMux.exe,DIRECT",
      ...CHAIN_LOCAL_RULES,
      "MATCH,Proxy",
    ]);
  });

  it("sends direct traffic through HypoMux but keeps explicit and local direct rules", () => {
    const result = runScript(10800, {
      rules: [
        "GEOIP,CN,DIRECT,no-resolve",
        "PROCESS-NAME,game.exe,DIRECT",
        "SRC-IP-CIDR,192.168.1.50/32,DIRECT",
        "GEOIP,LAN,DIRECT",
        "AND,((DOMAIN,a.example),(NETWORK,UDP)),DIRECT",
        "DOMAIN,x.example,Proxy",
        "SUB-RULE,(NETWORK,TCP),tcp",
        "MATCH,DIRECT",
      ],
      "sub-rules": { tcp: ["DOMAIN-SUFFIX,cn,DIRECT", "MATCH,Proxy"] },
    });

    expect(result.rules.slice(2 + CHAIN_LOCAL_RULES.length)).toEqual([
      `GEOIP,CN,${CHAIN_GROUP_NAME},no-resolve`,
      "PROCESS-NAME,game.exe,DIRECT",
      "SRC-IP-CIDR,192.168.1.50/32,DIRECT",
      "GEOIP,LAN,DIRECT",
      `AND,((DOMAIN,a.example),(NETWORK,UDP)),${CHAIN_GROUP_NAME}`,
      "DOMAIN,x.example,Proxy",
      "SUB-RULE,(NETWORK,TCP),tcp",
      `MATCH,${CHAIN_GROUP_NAME}`,
    ]);
    expect(result["sub-rules"].tcp).toEqual([`DOMAIN-SUFFIX,cn,${CHAIN_GROUP_NAME}`, "MATCH,Proxy"]);
  });

  it("offers HypoMux in groups without changing a manual DIRECT choice", () => {
    const result = runScript(10800, {
      "proxy-groups": [
        { name: "Direct", type: "select", proxies: ["DIRECT", "Proxy"] },
        { name: "Auto", type: "url-test", proxies: ["HK", "DIRECT"] },
      ],
    });

    expect(result["proxy-groups"][0].proxies).toEqual([CHAIN_GROUP_NAME, "DIRECT", "Proxy"]);
    expect(result["proxy-groups"][1].proxies).toEqual(["HK", CHAIN_GROUP_NAME]);
    // The egress group itself must keep a real DIRECT fallback.
    expect(result["proxy-groups"].at(-1).proxies).toEqual([CHAIN_PROXY_NAME, "DIRECT"]);
  });

  it("leaves direct traffic alone when that option is off", () => {
    const result = runScript(10800, {
      "proxy-groups": [{ name: "Direct", type: "select", proxies: ["DIRECT"] }],
      rules: ["GEOIP,CN,DIRECT", "MATCH,Proxy"],
    }, false);

    expect(result.rules).toEqual(["PROCESS-NAME,hypomux-engine.exe,DIRECT", "PROCESS-NAME,HypoMux.exe,DIRECT", "GEOIP,CN,DIRECT", "MATCH,Proxy"]);
    expect(result["proxy-groups"][0].proxies).toEqual(["DIRECT"]);
  });

  it("applies to subscription providers without overriding their own dialer", () => {
    const result = runScript(10800, {
      "proxy-providers": {
        airport: { type: "http", url: "https://sub.example/a", override: { udp: true } },
        chained: { type: "http", url: "https://sub.example/b", override: { "dialer-proxy": "Relay" } },
        bare: { type: "file", path: "./b.yaml" },
      },
    });

    expect(result["proxy-providers"].airport.override).toEqual({ udp: true, "dialer-proxy": CHAIN_GROUP_NAME });
    expect(result["proxy-providers"].chained.override["dialer-proxy"]).toBe("Relay");
    expect(result["proxy-providers"].bare.override["dialer-proxy"]).toBe(CHAIN_GROUP_NAME);
  });

  it("is idempotent when the client reapplies it", () => {
    const once = runScript(10800, { proxies: [{ name: "JP", type: "trojan" }], rules: ["MATCH,DIRECT"] });
    const twice = runScript(10800, structuredClone(once));

    expect(twice.proxies.filter((proxy: Config) => proxy.name === CHAIN_PROXY_NAME)).toHaveLength(1);
    expect(twice["proxy-groups"].filter((group: Config) => group.name === CHAIN_GROUP_NAME)).toHaveLength(1);
    expect(twice.rules).toEqual(once.rules);
  });

  it("never chains the HypoMux SOCKS5 entry into itself", () => {
    const result = runScript(10800, { proxies: [{ name: CHAIN_PROXY_NAME, type: "socks5", port: 1 }] });

    expect(result.proxies).toHaveLength(1);
    expect(result.proxies[0]).toMatchObject({ port: 10800 });
    expect(result.proxies[0]["dialer-proxy"]).toBeUndefined();
  });
});

describe("sing-box chain outbound", () => {
  it("is a bare socks outbound sing-box can parse", () => {
    expect(JSON.parse(buildSingBoxChainJSON(23456))).toEqual({
      outbounds: [{ type: "socks", tag: "hypomux", server: "127.0.0.1", server_port: 23456, version: "5" }],
    });
  });
});

describe("Mihomo chain YAML", () => {
  it("uses the configured SOCKS5 port", () => {
    const yaml = buildMihomoChainYAML(23456);

    expect(yaml).toContain("port: 23456");
    expect(yaml).toContain(`dialer-proxy: ${CHAIN_GROUP_NAME}`);
    expect(yaml).toContain("PROCESS-NAME,hypomux-engine.exe,DIRECT");
  });
});
