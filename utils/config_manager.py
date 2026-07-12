"""
HypoMux 配置持久化模块 - config_manager

【第一阶段 · 任务 1：保存配置功能】

设计目标：
- 使用 Python 原生 `pathlib` + `json`，把用户配置写入【系统家目录】下的
  `~/.hypomux/config.json`，彻底规避「程序安装在 Program Files 等只读目录、
  无法在运行目录写文件」的权限问题。
- 全链路防御式编程：读取损坏 / 缺失 / 越权 的配置文件时一律优雅降级，
  返回默认配置而绝不抛异常拖垮主程序。
- 与 UI 解耦：本模块不依赖任何 Qt 对象，可被任意层级安全导入。

持久化的数据项（至少包含）：
- selected_adapters : 当前勾选的网卡（按网卡别名 alias 记录，别名比 index 稳定）
- socks_port        : 当前加速模式的 SOCKS 端口
- http_port         : 当前加速模式的 HTTP/HTTPS 端口

config.json 形如：
{
    "version": 1,
    "selected_adapters": ["以太网", "WLAN"],
    "socks_port": 10800,
    "http_port": 10801
}
"""

from __future__ import annotations

import json
import logging
from pathlib import Path
from typing import Any, Dict, List

logger = logging.getLogger(__name__)

# 配置结构版本号，便于未来字段迁移
CONFIG_VERSION = 1

# 默认端口（与 main_window / settings_page 中的常量保持一致）
DEFAULT_SOCKS_PORT = 10800
DEFAULT_HTTP_PORT = 10801
DEFAULT_DNS_SERVER = "223.5.5.5"
DEFAULT_DOH_PROVIDER = "auto"
VALID_DOH_PROVIDERS = {"auto", "alidns", "dnspod", "google"}

# 配置目录 / 文件名
CONFIG_DIR_NAME = ".hypomux"
CONFIG_FILE_NAME = "config.json"


def get_config_path() -> Path:
    """返回配置文件的绝对路径，并确保其父目录存在。

    路径固定为 `~/.hypomux/config.json`：
    - `Path.home()` 解析到当前用户家目录（Windows 下通常是 C:\\Users\\<用户名>），
      该目录对当前用户始终可写，避免运行目录只读导致的写入失败。
    - `mkdir(parents=True, exist_ok=True)` 幂等创建目录，已存在不会报错。

    Returns:
        Path: 指向 config.json 的 Path 对象（文件本身可能尚未创建）。
    """
    config_dir = Path.home() / CONFIG_DIR_NAME
    config_dir.mkdir(parents=True, exist_ok=True)
    return config_dir / CONFIG_FILE_NAME


def default_config() -> Dict[str, Any]:
    """返回一份全新的默认配置（深拷贝安全，调用方可随意修改）。"""
    return {
        "version": CONFIG_VERSION,
        "selected_adapters": [],
        "socks_port": DEFAULT_SOCKS_PORT,
        "http_port": DEFAULT_HTTP_PORT,
        # 第三阶段下半场：运行模式与进程级分流规则
        "run_mode": "proxy",          # proxy | tun
        "routing_rules": [],
        "dns_server": DEFAULT_DNS_SERVER,
        "doh_provider": DEFAULT_DOH_PROVIDER,
        "blocked_domain_bypass": True,  # 自动规避单网卡被墙域名
        "weighted_scheduler": False,    # 权重调度器
        "nic_bandwidth_limits": {},     # {nic_alias: mbps}
    }


def _looks_mojibake(value: str) -> bool:
    """识别旧打包版本写入的乱码网卡别名。"""
    text = str(value)
    return any(marker in text for marker in ("�", "浠", "缃", "å", "ç", "Ã"))


def _coerce_config(raw: Any) -> Dict[str, Any]:
    """把任意来源的原始数据规整成合法配置字典。

    对每个字段做类型校验与回退，确保返回值的结构永远可信，
    上层 UI 拿到后无需再做防御性判断。
    """
    cfg = default_config()

    if not isinstance(raw, dict):
        return cfg

    # selected_adapters：必须是字符串列表
    raw_selected = raw.get("selected_adapters")
    if isinstance(raw_selected, list):
        cfg["selected_adapters"] = [
            text for text in (str(item).strip() for item in raw_selected if item is not None)
            if text and not _looks_mojibake(text)
        ]

    # socks_port / http_port：必须是合法端口范围内的整数
    cfg["socks_port"] = _coerce_port(raw.get("socks_port"), DEFAULT_SOCKS_PORT)
    cfg["http_port"] = _coerce_port(raw.get("http_port"), DEFAULT_HTTP_PORT)

    # run_mode：仅允许 proxy/tun，非法回退 proxy
    raw_mode = str(raw.get("run_mode", "proxy")).strip().lower()
    cfg["run_mode"] = raw_mode if raw_mode in ("proxy", "tun") else "proxy"

    # routing_rules：进程级分流规则列表，轻量结构校验，具体合法性由 singbox_config 规整
    raw_rules = raw.get("routing_rules")
    if isinstance(raw_rules, list):
        cfg["routing_rules"] = [item for item in raw_rules if isinstance(item, dict)]

    # dns_server：首选 DNS IPv4，非法回退默认值
    raw_dns = str(raw.get("dns_server", DEFAULT_DNS_SERVER)).strip()
    cfg["dns_server"] = raw_dns if _is_valid_ipv4(raw_dns) else DEFAULT_DNS_SERVER

    raw_doh = str(raw.get("doh_provider", DEFAULT_DOH_PROVIDER)).strip().lower()
    cfg["doh_provider"] = raw_doh if raw_doh in VALID_DOH_PROVIDERS else DEFAULT_DOH_PROVIDER

    # blocked_domain_bypass：自动规避单网卡被墙域名
    raw_bypass = raw.get("blocked_domain_bypass", True)
    cfg["blocked_domain_bypass"] = bool(raw_bypass)

    # weighted_scheduler：权重调度器开关
    cfg["weighted_scheduler"] = bool(raw.get("weighted_scheduler", False))

    # nic_bandwidth_limits：网卡带宽上限 {alias: mbps}
    raw_limits = raw.get("nic_bandwidth_limits")
    if isinstance(raw_limits, dict):
        cfg["nic_bandwidth_limits"] = {
            str(k): int(v) for k, v in raw_limits.items()
            if str(k) and isinstance(v, (int, float)) and int(v) > 0
        }
    else:
        cfg["nic_bandwidth_limits"] = {}

    return cfg


def _coerce_port(value: Any, fallback: int) -> int:
    """把任意值转换为 1-65534 范围内的端口整数，非法则回退。"""
    try:
        port = int(value)
    except (TypeError, ValueError):
        return fallback
    if 1 <= port <= 65534:
        return port
    return fallback


def _is_valid_ipv4(value: Any) -> bool:
    """校验基础 IPv4 地址格式。"""
    try:
        parts = str(value).strip().split(".")
        if len(parts) != 4:
            return False
        for part in parts:
            if not part.isdigit():
                return False
            if len(part) > 1 and part.startswith("0"):
                return False
            number = int(part)
            if number < 0 or number > 255:
                return False
        return True
    except Exception:
        return False


def load_config() -> Dict[str, Any]:
    """读取并返回配置字典。

    优雅降级策略：
    - 文件不存在        -> 返回默认配置
    - 文件损坏 / 非法 JSON -> 记录警告并返回默认配置
    - 任意 IO / 权限异常  -> 记录警告并返回默认配置

    本函数【保证永不抛出异常】，调用方可以无脑信任其返回值。

    Returns:
        Dict[str, Any]: 经过结构校验的合法配置字典。
    """
    try:
        config_path = get_config_path()
    except Exception as e:
        logger.warning(f"无法定位配置目录，使用默认配置: {e}")
        return default_config()

    if not config_path.exists():
        logger.info("配置文件不存在，使用默认配置")
        return default_config()

    try:
        text = config_path.read_text(encoding="utf-8")
        raw = json.loads(text)
        cfg = _coerce_config(raw)
        logger.info(f"成功加载配置: {config_path}")
        return cfg
    except (json.JSONDecodeError, ValueError) as e:
        logger.warning(f"配置文件损坏（非法 JSON），已回退默认配置: {e}")
        return default_config()
    except OSError as e:
        logger.warning(f"读取配置文件失败（IO/权限），已回退默认配置: {e}")
        return default_config()
    except Exception as e:
        logger.warning(f"加载配置发生未知异常，已回退默认配置: {e}")
        return default_config()


def save_config(config: Dict[str, Any]) -> bool:
    """把配置字典持久化到 `~/.hypomux/config.json`。

    采用「先写临时文件再原子替换」的方式，避免写入过程中崩溃留下半截文件。
    本函数同样【吞掉所有异常】，写失败只返回 False，绝不拖垮主程序。

    Args:
        config: 待保存的配置字典（会先经过结构规整）。

    Returns:
        bool: True 表示写入成功，False 表示写入失败（已被安全吞掉）。
    """
    cfg = _coerce_config(config)
    cfg["version"] = CONFIG_VERSION

    try:
        config_path = get_config_path()
        tmp_path = config_path.with_suffix(".json.tmp")
        tmp_path.write_text(
            json.dumps(cfg, ensure_ascii=False, indent=2),
            encoding="utf-8",
        )
        # 原子替换，保证 config.json 永远是完整内容
        tmp_path.replace(config_path)
        logger.info(f"配置已保存: {config_path}")
        return True
    except OSError as e:
        logger.warning(f"保存配置失败（IO/权限）: {e}")
        return False
    except Exception as e:
        logger.warning(f"保存配置发生未知异常: {e}")
        return False
