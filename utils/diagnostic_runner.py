"""
HypoMux 网卡诊断调用层 - diagnostic_runner

异步调用 Go 引擎的网络诊断命令。

职责：
- 智能定位 hypomux-engine.exe（兼容源码运行、开发构建与 Nuitka 安装目录）。
- 通过 asyncio.create_subprocess_exec 异步拉起 ``diagnose`` 命令，传入 --src-ip / --target-ip，
  全程不阻塞调用方事件循环；隐藏子进程窗口。
- 解析内核 stdout 的单行 JSON，规整为结构化 dict 返回。
- 任何异常（exe 缺失 / 超时 / JSON 损坏）均优雅降级为 unavailable 结果，绝不抛出。
"""

from __future__ import annotations

import asyncio
import json
import os
import sys
import logging
from typing import Any, Dict, List, Optional

logger = logging.getLogger(__name__)

ENGINE_EXE_NAME = "hypomux-engine.exe"
ENGINE_PATH_ENV = "HYPOMUX_ENGINE_PATH"
DEFAULT_TARGET_IP = "223.5.5.5"   # 阿里云公共 DNS
PROBE_TIMEOUT_SEC = 20            # 10 包 * 1s + 余量


def _base_dir() -> str:
    """返回主程序所在目录。

    - 打包态 (Nuitka/PyInstaller)：sys.frozen / __compiled__ 为真，
      使用 sys.executable 所在目录（引擎位于它的 bin 子目录）。
    - 源码态：用本文件上溯到项目根目录（utils/ 的上一级）。
    """
    is_frozen = getattr(sys, "frozen", False) or ("__compiled__" in globals())
    if is_frozen:
        exe = sys.executable or sys.argv[0]
        return os.path.dirname(os.path.abspath(exe))
    # utils/diagnostic_runner.py -> 项目根
    return os.path.dirname(os.path.dirname(os.path.abspath(__file__)))


def get_engine_path() -> Optional[str]:
    """解析 hypomux-engine.exe 的绝对路径；找不到返回 None。

    环境变量用于开发/测试覆盖；安装包使用 ``bin`` 子目录。
    """
    configured = os.environ.get(ENGINE_PATH_ENV, "").strip()
    base_dir = _base_dir()
    current_dir = os.getcwd()
    candidates = [
        configured,
        os.path.join(base_dir, "bin", ENGINE_EXE_NAME),
        os.path.join(base_dir, ENGINE_EXE_NAME),
        os.path.join(base_dir, "dist", ENGINE_EXE_NAME),
        os.path.join(base_dir, "engine", ENGINE_EXE_NAME),
        os.path.join(current_dir, "bin", ENGINE_EXE_NAME),
        os.path.join(current_dir, ENGINE_EXE_NAME),
    ]
    for path in dict.fromkeys(path for path in candidates if path):
        if os.path.isfile(path):
            return os.path.abspath(path)
    logger.warning(f"未找到 {ENGINE_EXE_NAME}，尝试过: {candidates}")
    return None


def get_diagnostic_path() -> Optional[str]:
    """兼容旧调用方；诊断程序现已合并到 Go 引擎。"""
    return get_engine_path()


def _fallback_result(src_ip: str, target_ip: str, note: str) -> Dict[str, Any]:
    """构造一个降级的 unavailable 结果（exe 缺失/异常时使用）。"""
    return {
        "status": "unavailable",
        "loss_rate": 100,
        "avg_latency_ms": 0,
        "jitter_ms": 0,
        "sent": 0,
        "received": 0,
        "src_ip": src_ip,
        "target_ip": target_ip,
        "note": note,
    }


def _normalize(raw: Any, src_ip: str, target_ip: str) -> Dict[str, Any]:
    """把内核 JSON 规整为可信结构（类型与状态白名单校验）。"""
    if not isinstance(raw, dict):
        return _fallback_result(src_ip, target_ip, "malformed kernel output")

    status = str(raw.get("status", "unavailable")).lower()
    if status not in ("available", "unstable", "unavailable"):
        status = "unavailable"

    def _int(key: str, default: int = 0) -> int:
        try:
            return int(raw.get(key, default))
        except (TypeError, ValueError):
            return default

    return {
        "status": status,
        "loss_rate": _int("loss_rate", 100),
        "avg_latency_ms": _int("avg_latency_ms", 0),
        "jitter_ms": _int("jitter_ms", 0),
        "sent": _int("sent", 0),
        "received": _int("received", 0),
        "src_ip": str(raw.get("src_ip", src_ip)),
        "target_ip": str(raw.get("target_ip", target_ip)),
        "note": str(raw.get("note", "")),
    }


def build_configuration_checks(adapter: Dict[str, Any], result: Dict[str, Any]) -> List[Dict[str, str]]:
    """补充 ICMP 无法区分的本机链路配置问题。

    ICMP 结果只能说明目标是否可达；默认网关、DNS 和跃点配置缺失时，用户仍会
    感到“网络异常”。这些字段来自同一次网卡扫描，不额外发起网络请求，也不会
    延长体检时间。界面根据 key 本地化展示名称和状态。
    """
    note = str(result.get("note", "")).strip()
    bind_failed = "bind failed" in note.lower() or "1231" in note
    src_ip = str(result.get("src_ip") or adapter.get("ip") or "")
    gateway = str(adapter.get("gateway") or "").strip()
    dns_servers = adapter.get("dns_servers") or []
    if not isinstance(dns_servers, list):
        dns_servers = [dns_servers]
    dns_text = ", ".join(str(item).strip() for item in dns_servers if str(item).strip())

    try:
        metric = int(adapter.get("metric", -1))
    except (TypeError, ValueError):
        metric = -1
    automatic_metric = bool(adapter.get("is_auto", True))

    checks: List[Dict[str, str]] = [
        {
            "key": "source_binding",
            "level": "fail" if bind_failed else "pass",
            "detail": note if bind_failed else src_ip,
        },
        {
            "key": "gateway",
            "level": "pass" if gateway else "warn",
            "detail": gateway,
        },
        {
            "key": "dns",
            "level": "pass" if dns_text else "warn",
            "detail": dns_text,
        },
        {
            "key": "metric",
            "level": "pass" if metric >= 0 else "warn",
            "detail": str(metric) if metric >= 0 else "",
            "mode": "auto" if automatic_metric else "fixed",
        },
    ]
    return checks


async def run_diagnostic(
    src_ip: str,
    target_ip: str = DEFAULT_TARGET_IP,
    timeout: float = PROBE_TIMEOUT_SEC,
) -> Dict[str, Any]:
    """异步执行一次网卡链路诊断。

    Args:
        src_ip: 待诊断网卡的本地出口 IPv4（必填）。
        target_ip: 探测目标 IP，默认阿里云 DNS。
        timeout: 子进程整体超时（秒）。

    Returns:
        Dict: 规整后的诊断结果，永不抛异常。
    """
    if not src_ip:
        return _fallback_result(src_ip, target_ip, "empty src_ip")

    exe_path = get_engine_path()
    if not exe_path:
        return _fallback_result(src_ip, target_ip, "hypomux-engine.exe not found")

    # 隐藏子进程控制台窗口（Windows）
    creationflags = 0
    if sys.platform == "win32":
        creationflags = getattr(asyncio.subprocess, "CREATE_NO_WINDOW", 0x08000000)

    try:
        proc = await asyncio.create_subprocess_exec(
            exe_path,
            "diagnose",
            "--src-ip", src_ip,
            "--target-ip", target_ip,
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE,
            creationflags=creationflags,
        )
    except Exception as e:
        logger.warning(f"启动 hypomux-engine.exe 失败: {e}")
        return _fallback_result(src_ip, target_ip, f"spawn failed: {e}")

    try:
        stdout_data, _stderr = await asyncio.wait_for(proc.communicate(), timeout=timeout)
    except asyncio.TimeoutError:
        try:
            proc.kill()
        except Exception:
            pass
        logger.warning("hypomux-engine.exe diagnose 执行超时")
        return _fallback_result(src_ip, target_ip, "timeout")
    except Exception as e:
        logger.warning(f"hypomux-engine.exe diagnose 通信异常: {e}")
        return _fallback_result(src_ip, target_ip, f"communicate failed: {e}")

    text = (stdout_data or b"").decode("utf-8", errors="replace").strip()
    if not text:
        return _fallback_result(src_ip, target_ip, "empty kernel output")

    # 内核保证单行 JSON；万一有多行，取最后一行非空内容
    last_line = [ln for ln in text.splitlines() if ln.strip()]
    json_line = last_line[-1] if last_line else text

    try:
        raw = json.loads(json_line)
    except json.JSONDecodeError as e:
        logger.warning(f"解析 Go 诊断 JSON 失败: {e} | 原始: {json_line[:200]}")
        return _fallback_result(src_ip, target_ip, "bad json")

    result = _normalize(raw, src_ip, target_ip)
    logger.info(
        f"诊断完成 src={src_ip} -> {target_ip}: {result['status']} "
        f"loss={result['loss_rate']}% jitter={result['jitter_ms']}ms"
    )
    return result
