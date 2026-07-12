"""
HypoMux 单网卡被墙域名追踪器 - BlockedDomainTracker

核心机制：
1. 当某网卡连接目标域名失败时，记录失败事件
2. 异步用其他网卡测试同一域名（3 次），全部成功 → 确认域名对该网卡被墙
3. 后续轮询分配网卡时自动跳过黑名单中的网卡
4. 持久化到 ~/.hypomux/blocked_domains.json
"""

from __future__ import annotations

import asyncio
import json
import logging
import socket
import struct
import threading
import time
from pathlib import Path
from typing import Dict, Optional, Set, List

logger = logging.getLogger(__name__)

IP_UNICAST_IF = 31
_VERIFY_RETRIES = 3
_VERIFY_TIMEOUT = 5.0
_PERSIST_DIR = Path.home() / ".hypomux"
_PERSIST_FILE = _PERSIST_DIR / "blocked_domains.json"


def _normalize_domain(domain: str) -> str:
    return (domain or "").rstrip(".").lower().strip()


class BlockedDomainTracker:
    """线程安全的单网卡被墙域名追踪器。

    作为模块级单例使用，ProxyWorker 与 MultiPortProxyWorker 共享同一实例。
    """

    def __init__(self):
        self._lock = threading.Lock()
        # {nic_name: set[domain]}
        self._blocked: Dict[str, Set[str]] = {}
        # 验证中或待验证计数，防止重复触发
        self._pending_verifications: Dict[str, Set[str]] = {}
        # 验证失败计数（同一域名在同一网卡上的失败次数，累计 3 次即加入黑名单）
        self._fail_counts: Dict[str, Dict[str, int]] = {}
        # 全局开关：用户可在 UI 中关闭此功能
        self._enabled = True
        self._load()

    @property
    def enabled(self) -> bool:
        return self._enabled

    @enabled.setter
    def enabled(self, value: bool):
        self._enabled = bool(value)

    # ----- 查询 -----
    def is_blocked(self, nic_name: str, domain: str) -> bool:
        if not self._enabled:
            return False
        domain = _normalize_domain(domain)
        if not domain or not nic_name:
            return False
        with self._lock:
            return domain in self._blocked.get(nic_name, set())

    def get_blocked_domains(self, nic_name: str) -> List[str]:
        with self._lock:
            return sorted(self._blocked.get(nic_name, set()))

    def all_blocked(self) -> Dict[str, List[str]]:
        """返回完整黑名单，供 UI 渲染。"""
        with self._lock:
            return {name: sorted(domains) for name, domains in self._blocked.items() if domains}

    def remove_domain(self, nic_name: str, domain: str):
        domain = _normalize_domain(domain)
        with self._lock:
            if nic_name in self._blocked:
                self._blocked[nic_name].discard(domain)
                if not self._blocked[nic_name]:
                    del self._blocked[nic_name]
            if nic_name in self._fail_counts:
                self._fail_counts[nic_name].pop(domain, None)

    def clear_nic(self, nic_name: str):
        with self._lock:
            self._blocked.pop(nic_name, None)
            self._fail_counts.pop(nic_name, None)

    def clear_all(self):
        with self._lock:
            self._blocked.clear()
            self._fail_counts.clear()
            self._pending_verifications.clear()

    # ----- 连接失败上报 + 异步验证 -----
    def on_connect_failure(
        self,
        nic_name: str,
        domain: str,
        port: int,
        failed_nic: Dict,
        all_nics: List[Dict],
        loop: asyncio.AbstractEventLoop,
    ):
        """连接失败时调用，在后台异步验证该域名是否被墙。

        验证逻辑：
        1. 用 all_nics 中除 failed_nic 以外的网卡尝试连接 domain:port（或用 443 兜底）
        2. 连续 3 次成功 → 域名确认被墙 → 加入黑名单
        3. 任何一次失败 → 不加入黑名单（可能是目标服务器本身挂了）
        """
        if not self._enabled:
            return
        domain = _normalize_domain(domain)
        if not domain or not nic_name:
            return

        # 如果已经确认被墙，不再重复验证
        if self.is_blocked(nic_name, domain):
            return

        # 防止同一 (网卡, 域名) 并发验证 + 冷却期
        key = f"{nic_name}:{domain}"
        with self._lock:
            pending = self._pending_verifications.setdefault(nic_name, set())
            if domain in pending:
                return
            pending.add(domain)

        logger.info(f"[被墙检测] 网卡 [{nic_name}] 连接 {domain} 失败，启动后台验证...")

        verify_port = port if port > 0 else 443

        async def _verify():
            success_count = 0
            for attempt in range(_VERIFY_RETRIES):
                candidates = [n for n in all_nics if n.get("name") != nic_name]
                if not candidates:
                    break
                test_nic = candidates[attempt % len(candidates)]
                logger.info(
                    f"[被墙检测] 第 {attempt + 1}/{_VERIFY_RETRIES} 次验证: "
                    f"用 [{test_nic.get('name')}] 测试 {domain}:{verify_port}"
                )
                ok = await self._test_connect(test_nic, domain, verify_port, loop)
                if ok:
                    success_count += 1
                else:
                    logger.info(
                        f"[被墙检测] 验证中断: [{test_nic.get('name')}] 也无法连接 {domain}，"
                        f"非单网卡被墙，不加入黑名单"
                    )
                    break

            confirmed = success_count >= _VERIFY_RETRIES
            if confirmed:
                with self._lock:
                    self._blocked.setdefault(nic_name, set()).add(domain)
                logger.info(
                    f"[被墙确认] 网卡 [{nic_name}] 无法访问 {domain}（其他网卡 {_VERIFY_RETRIES} 次验证全部成功）"
                )
            else:
                logger.info(
                    f"[被墙检测] 验证完成: [{nic_name}] -> {domain} 未确认被墙"
                )

            with self._lock:
                self._pending_verifications.get(nic_name, set()).discard(domain)

        asyncio.ensure_future(_verify(), loop=loop)

    async def _test_connect(
        self,
        nic: Dict,
        domain: str,
        port: int,
        loop: asyncio.AbstractEventLoop,
    ) -> bool:
        """用指定网卡测试是否可连接目标域名。"""
        sock = None
        nic_name = nic.get("name", "?")
        test_port = port if port > 0 else 443
        try:
            # 先解析 DNS（系统默认 + 公共 DNS 兜底）
            dst_addr = None
            dns_error = ""
            try:
                addrs = await asyncio.wait_for(
                    loop.getaddrinfo(domain, test_port, family=socket.AF_INET, type=socket.SOCK_STREAM),
                    timeout=3.0,
                )
                dst_addr = addrs[0][4][0]
            except Exception as e:
                dns_error = f"系统DNS: {type(e).__name__}"

            # 兜底：通过测试网卡向 223.5.5.5 发送 DNS 查询
            if dst_addr is None:
                try:
                    dst_addr = await self._resolve_via_public_dns(domain, nic, loop)
                except Exception as e2:
                    dns_error += f" | 公共DNS({nic_name}): {type(e2).__name__}"

            if dst_addr is None:
                logger.info(f"[被墙检测] [{nic_name}] DNS解析失败: {dns_error}")
                return False

            if_index = int(nic.get("if_index", nic.get("index", 0)) or 0)
            if not if_index:
                logger.info(f"[被墙检测] [{nic_name}] 无有效 IfIndex")
                return False

            sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
            sock.setblocking(False)
            sock.setsockopt(socket.IPPROTO_IP, IP_UNICAST_IF, struct.pack("!I", if_index))
            local_ip = nic.get("ip")
            if local_ip:
                try:
                    sock.bind((local_ip, 0))
                except OSError:
                    pass

            await asyncio.wait_for(
                loop.sock_connect(sock, (dst_addr, test_port)),
                timeout=_VERIFY_TIMEOUT,
            )
            return True
        except asyncio.TimeoutError:
            logger.info(f"[被墙检测] [{nic_name}] TCP连接 {domain}:{test_port} 超时({_VERIFY_TIMEOUT}s)")
            return False
        except Exception as e:
            logger.info(f"[被墙检测] [{nic_name}] 连接 {domain}:{test_port} 失败: {type(e).__name__}: {e}")
            return False
        finally:
            if sock is not None:
                try:
                    sock.close()
                except Exception:
                    pass

    @staticmethod
    async def _resolve_via_public_dns(domain: str, nic: Dict, loop: asyncio.AbstractEventLoop) -> str:
        """通过指定网卡向 223.5.5.5 发送 DNS A 查询，绕过系统 DNS。"""
        import random
        query_id = random.randint(0, 0xFFFF)
        labels = domain.rstrip(".").encode("idna").split(b".")
        question = b"".join(bytes([len(label)]) + label for label in labels) + b"\x00"
        packet = struct.pack("!HHHHHH", query_id, 0x0100, 1, 0, 0, 0) + question + struct.pack("!HH", 1, 1)

        if_index = int(nic.get("if_index", nic.get("index", 0)) or 0)
        sock = None
        try:
            sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
            sock.setblocking(False)
            sock.setsockopt(socket.IPPROTO_IP, IP_UNICAST_IF, struct.pack("!I", if_index))
            local_ip = nic.get("ip")
            if local_ip:
                try:
                    sock.bind((local_ip, 0))
                except OSError:
                    pass
            await loop.sock_sendto(sock, packet, ("223.5.5.5", 53))
            data, _remote = await asyncio.wait_for(loop.sock_recvfrom(sock, 512), timeout=3.0)

            if len(data) < 12:
                raise ValueError("short response")
            resp_id, flags, qdcount, ancount = struct.unpack("!HHHHHH", data[:12])
            if resp_id != query_id or (flags & 0x8000) == 0:
                raise ValueError("mismatched response")
            if (flags & 0x000F) != 0:
                raise ValueError(f"rcode={flags & 0x000F}")

            offset = 12
            for _ in range(qdcount):
                while offset < len(data) and data[offset] != 0:
                    if data[offset] & 0xC0:
                        offset += 2
                        break
                    offset += 1 + data[offset]
                offset += 5

            for _ in range(ancount):
                while offset < len(data) and data[offset] != 0:
                    if data[offset] & 0xC0:
                        offset += 2
                        break
                    offset += 1 + data[offset]
                offset += 1
                if offset + 10 > len(data):
                    break
                rtype, rclass, rdlen = struct.unpack("!HHI", data[offset:offset + 8])
                offset += 8
                if rtype == 1 and rclass == 1 and rdlen == 4 and offset + 4 <= len(data):
                    return socket.inet_ntoa(data[offset:offset + 4])
                offset += rdlen
            raise ValueError("no A record")
        finally:
            if sock is not None:
                try:
                    sock.close()
                except Exception:
                    pass

    # ----- 持久化 -----
    def _load(self):
        try:
            _PERSIST_DIR.mkdir(parents=True, exist_ok=True)
            if not _PERSIST_FILE.exists():
                return
            raw = json.loads(_PERSIST_FILE.read_text(encoding="utf-8"))
            if not isinstance(raw, dict):
                return
            with self._lock:
                for nic_name, domains in raw.items():
                    if isinstance(domains, list):
                        self._blocked[nic_name] = {_normalize_domain(d) for d in domains if d}
            logger.info(f"已加载被墙域名清单: {len(self._blocked)} 张网卡")
        except Exception as e:
            logger.warning(f"加载被墙域名清单失败: {e}")

    def save(self):
        if not self._enabled:
            return
        try:
            _PERSIST_DIR.mkdir(parents=True, exist_ok=True)
            with self._lock:
                payload = {
                    name: sorted(domains)
                    for name, domains in self._blocked.items()
                    if domains
                }
            tmp = _PERSIST_FILE.with_suffix(".json.tmp")
            tmp.write_text(json.dumps(payload, ensure_ascii=False, indent=2), encoding="utf-8")
            tmp.replace(_PERSIST_FILE)
        except Exception as e:
            logger.warning(f"保存被墙域名清单失败: {e}")


# 模块级单例
_tracker: Optional[BlockedDomainTracker] = None
_tracker_lock = threading.Lock()


def get_tracker() -> BlockedDomainTracker:
    global _tracker
    if _tracker is None:
        with _tracker_lock:
            if _tracker is None:
                _tracker = BlockedDomainTracker()
    return _tracker
