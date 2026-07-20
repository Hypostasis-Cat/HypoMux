"""
HypoMux sing-box 配置生成器 - 第三阶段下半场 · 任务2

把【路由规则页】TableWidget 中的进程级分流规则，动态序列化为标准的
sing-box 兼容 config.json。

架构映射：
- inbounds : 单一 tun 入站（interface_name=HypoMux-Tun，auto_route + strict_route），
  全局吸入系统 TCP/UDP 流量。
- outbounds: 三个 socks 出站，分别对接 Python 本地多端口出站池。
  默认端口为 2001/2002/2003；端口受 HNS/Hyper-V 限制时使用运行时回退端口。
  另含 direct（保底直连）。
- route.rules: 顶部按固定顺序强插后端自流量防环、DNS 劫持、ICMP 网络
  直连防御矩阵，并让 QUIC 快速回退到 TCP（规避 FakeIP/QUIC 会话不稳定），
  再按用户表格逐条生成 {process_name:[...], outbound:...}；
  未命中规则的默认兜底 final 一律指向 aggregation，实现 TCP/UDP 全局聚合叠加。

纯逻辑模块，零 Qt 依赖，防御式编程，绝不抛出未捕获异常。
"""

from __future__ import annotations

import json
import logging
from pathlib import Path
from typing import Any, Dict, List, Optional

logger = logging.getLogger(__name__)

# 三大出站标签（与 UI / 多端口出站池端口一一对应）
OUTBOUND_ETHERNET = "nic_ethernet"
OUTBOUND_WIFI = "nic_wifi"
OUTBOUND_AGGREGATION = "aggregation"
OUTBOUND_DIRECT = "direct"

# 合法出站标签集合（用于校验用户表格输入）
VALID_OUTBOUNDS = {
    OUTBOUND_ETHERNET,
    OUTBOUND_WIFI,
    OUTBOUND_AGGREGATION,
    OUTBOUND_DIRECT,
}

# Python 本地多端口出站池端口（任务1）
PORT_ETHERNET = 2001
PORT_WIFI = 2002
PORT_AGGREGATION = 2003

TUN_INTERFACE_NAME = "HypoMux-Tun"
DNS_LOCAL_TAG = "dns-local"
DNS_FAKEIP_TAG = "dns-fakeip"

# 常见游戏加速器的核心进程。它们负责隧道、分流驱动或本地转发；让这些
# 进程经过 HypoMux TUN 容易造成递归接管、路由冲突或使加速器失效。
#
# 这是一条内置安全兼容规则，而不是用户的分流偏好，因此不会出现在路由
# 规则页面，也不会写入用户配置。刻意不包含 steam.exe / steamservice.exe：
# Steam 下载仍可使用默认 aggregation 出站。游戏本体是否直连，仍由用户
# 在可见规则页自行决定。
BUILTIN_ACCELERATOR_PROCESS_NAMES = (
    # 奇游（QiYou）
    "qiyou.exe",
    "networkdaemon.exe",
    "qeetm.exe",
    "injhelper.exe",
    "injhelper64.exe",
    "lsphelper64.exe",
    # 网易 UU
    "uu.exe",
    "uu_agent.exe",
    "uu_launcher.exe",
    "uu_ball.exe",
    "uu_neths_helper.exe",
    "uu_neth_helper.exe",
    # 迅游
    "xunyou.exe",
    "xylauncher.exe",
    "xyprotectservice.exe",
    "xyservicelink.exe",
    # 雷神
    "leigod.exe",
    "leigod_launcher.exe",
    "leishensdk.exe",
    "leigod-tool.exe",
)

# 常见 Windows 代理客户端及其核心进程。仅在这些客户端使用「系统代理」
# 或本地 SOCKS/HTTP 端口时提供兼容：核心进程直连，避免其连接机场节点时
# 被 HypoMux 再次接管而形成回环。若客户端启用了自己的 TUN/虚拟网卡，
# 仍必须与 HypoMux TUN 二选一。
BUILTIN_PROXY_PROCESS_NAMES = (
    # Clash / Mihomo 系
    "clash.exe",
    "clash-meta.exe",
    "clash-win64.exe",
    "mihomo.exe",
    "mihomo-core.exe",
    "clash-verge.exe",
    "clash verge.exe",
    "clash-verge-rev.exe",
    "clash-verge-service.exe",
    "clash for windows.exe",
    "cfw.exe",
    "clashnyanpasu.exe",
    "flclash.exe",
    # V2Ray / Xray 系
    "v2rayn.exe",
    "v2ray.exe",
    "xray.exe",
    "nekoray.exe",
    "qv2ray.exe",
    "hiddify.exe",
    "hiddify-cli.exe",
    # Shadowsocks 与传统代理工具
    "shadowsocks.exe",
    "shadowsocksr.exe",
    "ss-local.exe",
    "surge.exe",
    "proxifier.exe",
    "sstap.exe",
    "tun2socks.exe",
)


def _socks_outbound(tag: str, port: int) -> Dict[str, Any]:
    """构造一个指向本地 Python 出站池端口的 socks 出站块。"""
    return {
        "type": "socks",
        "tag": tag,
        "server": "127.0.0.1",
        "server_port": port,
        "version": "5",
    }


def _is_valid_outbound_tag(tag: str) -> bool:
    """校验出站标签；允许固定标签与 nic_真实网卡别名动态标签。"""
    if tag in VALID_OUTBOUNDS:
        return True
    return tag.startswith("nic_") and len(tag) > 4


def _dynamic_nic_port(
    tag: str,
    ethernet_port: int = PORT_ETHERNET,
    wifi_port: int = PORT_WIFI,
) -> int:
    """把动态网卡别名标签映射到当前三通道出站池端口。"""
    alias = tag[4:].lower()
    if any(key in alias for key in ("wlan", "wi-fi", "wifi", "wireless", "无线")):
        return wifi_port
    return ethernet_port


def build_config(
    rules: Optional[List[Dict[str, Any]]] = None,
    *,
    ethernet_port: int = PORT_ETHERNET,
    wifi_port: int = PORT_WIFI,
    aggregation_port: int = PORT_AGGREGATION,
    tun_name: str = TUN_INTERFACE_NAME,
    default_outbound: str = OUTBOUND_AGGREGATION,
    dns_bind_ip: str = "",
    dns_bind_interface: str = "",
    app_process_path: str | List[str] = "",
) -> Dict[str, Any]:
    """根据用户规则动态构建 sing-box 配置字典。

    Args:
        rules: 规则列表，每项 {"process_name": [...], "outbound": "<tag>"}。
               兼容单字符串 process_name；非法/空规则会被安全跳过。
        default_outbound: 兜底出站标签（默认 aggregation 聚合叠加）。

    Returns:
        dict: 可直接 json.dump 的 sing-box 配置。
    """
    user_route_rules: List[Dict[str, Any]] = []
    for raw in (rules or []):
        rule = _normalize_rule(raw)
        if rule is not None:
            user_route_rules.append(rule)

    defensive_route_rules: List[Dict[str, Any]] = []
    defensive_route_rules.append({
        "action": "sniff",
        "timeout": "300ms",
    })
    app_paths: List[str] = []
    if isinstance(app_process_path, list):
        app_paths = [str(path).strip() for path in app_process_path if str(path).strip()]
    elif app_process_path:
        app_paths = [str(app_process_path).strip()]
    if app_paths:
        defensive_route_rules.append({
            "process_path": app_paths,
            "outbound": OUTBOUND_DIRECT,
        })
    defensive_route_rules.extend([
        {
            "process_name": [
                "HypoMux.exe",
                "main.exe",
                "python.exe",
                "pythonw.exe",
            ],
            "outbound": OUTBOUND_DIRECT,
        },
        {
            "process_name": [
                "sing-box.exe",
            ],
            "outbound": OUTBOUND_DIRECT,
        },
        {
            "process_name": list(BUILTIN_ACCELERATOR_PROCESS_NAMES),
            "outbound": OUTBOUND_DIRECT,
        },
        {
            "process_name": list(BUILTIN_PROXY_PROCESS_NAMES),
            "outbound": OUTBOUND_DIRECT,
        },
        {"port": [53], "action": "hijack-dns"},
        {"protocol": ["dns"], "action": "hijack-dns"},
        # 当前 Python SOCKS UDP relay 尚不能为 QUIC 保持稳定的五元组，
        # FakeIP 下直接放行还会尝试访问 198.18.0.0/15。仅拒绝 sniff 已
        # 识别的 QUIC，令浏览器立即回退 TCP/HTTPS，同时避免误伤其他恰好
        # 使用 UDP/443 的应用；其余 UDP 暂维持物理出口直连。
        {"protocol": ["quic"], "action": "reject"},
        {"network": ["udp"], "action": "route", "outbound": "direct"},
    ])
    route_rules = defensive_route_rules + user_route_rules

    dynamic_outbound_tags = []
    for rule in user_route_rules:
        tag = str(rule.get("outbound", ""))
        if tag.startswith("nic_") and tag not in (OUTBOUND_ETHERNET, OUTBOUND_WIFI):
            if tag not in dynamic_outbound_tags:
                dynamic_outbound_tags.append(tag)

    final_outbound = default_outbound if _is_valid_outbound_tag(default_outbound) else OUTBOUND_AGGREGATION

    outbounds = [
        _socks_outbound(OUTBOUND_ETHERNET, ethernet_port),
        _socks_outbound(OUTBOUND_WIFI, wifi_port),
        _socks_outbound(OUTBOUND_AGGREGATION, aggregation_port),
        {"type": "direct", "tag": OUTBOUND_DIRECT},
    ]
    for tag in dynamic_outbound_tags:
        outbounds.append(_socks_outbound(
            tag,
            _dynamic_nic_port(tag, ethernet_port, wifi_port),
        ))

    dns_server_config: Dict[str, Any] = {
        "type": "local",
        "tag": DNS_LOCAL_TAG,
        # Do not let hijacked TUN DNS follow the Windows default NIC.  The
        # aggregation SOCKS outbound reaches the Python pool, whose resolver
        # sockets are pinned with IP_UNICAST_IF per selected adapter.
        "detour": OUTBOUND_AGGREGATION,
    }
    fakeip_server_config: Dict[str, Any] = {
        "type": "fakeip",
        "tag": DNS_FAKEIP_TAG,
        "inet4_range": "198.18.0.0/15",
    }
    dns_rules: List[Dict[str, Any]] = [{
        "query_type": ["A", "AAAA"],
        "server": DNS_FAKEIP_TAG,
    }]
    config: Dict[str, Any] = {
        "log": {"level": "warn", "timestamp": True},
        "dns": {
            "servers": [
                dns_server_config,
                fakeip_server_config,
            ],
            "rules": dns_rules,
            "final": DNS_LOCAL_TAG,
            "reverse_mapping": True,
        },
        "inbounds": [
            {
                "type": "tun",
                "tag": "tun-in",
                "interface_name": tun_name,
                "address": ["172.19.0.1/30"],
                "mtu": 1492,
                "auto_route": True,
                "strict_route": True,
                "stack": "system",
            }
        ],
        "outbounds": outbounds,
        "route": {
            "auto_detect_interface": True,
            "default_domain_resolver": DNS_LOCAL_TAG,
            "final": final_outbound,
            "rules": route_rules,
        },
    }
    return config


def _normalize_rule(raw: Any) -> Optional[Dict[str, Any]]:
    """把任意来源的单条规则规整为合法的 sing-box route 规则；非法返回 None。"""
    if not isinstance(raw, dict):
        return None

    outbound = str(raw.get("outbound", "")).strip()
    if not _is_valid_outbound_tag(outbound):
        return None

    raw_proc = raw.get("process_name")
    procs: List[str] = []
    if isinstance(raw_proc, str):
        procs = [raw_proc.strip()] if raw_proc.strip() else []
    elif isinstance(raw_proc, list):
        procs = [str(p).strip() for p in raw_proc if str(p).strip()]
    if not procs:
        return None

    return {"process_name": procs, "outbound": outbound}


def write_config(
    config: Dict[str, Any],
    path: str | Path,
) -> bool:
    """把配置字典原子写入 config.json（先临时文件后替换）。

    Returns:
        bool: True 写入成功，False 失败（异常已被安全吞掉）。
    """
    try:
        target = Path(path)
        target.parent.mkdir(parents=True, exist_ok=True)
        tmp = target.with_suffix(".json.tmp")
        tmp.write_text(
            json.dumps(config, ensure_ascii=False, indent=2),
            encoding="utf-8",
        )
        tmp.replace(target)
        logger.info(f"sing-box 配置已写入: {target}")
        return True
    except OSError as e:
        logger.warning(f"写入 sing-box 配置失败（IO/权限）: {e}")
        return False
    except Exception as e:
        logger.warning(f"写入 sing-box 配置发生未知异常: {e}")
        return False


def generate_config_file(
    rules: Optional[List[Dict[str, Any]]],
    path: str | Path,
    **kwargs,
) -> bool:
    """便捷入口：构建 + 写入一步到位。"""
    return write_config(build_config(rules, **kwargs), path)
