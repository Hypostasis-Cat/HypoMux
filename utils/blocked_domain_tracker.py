"""
HypoMux 单网卡被墙域名追踪器 - BlockedDomainTracker

核心机制：
1. 当某网卡连接目标域名失败时，记录失败事件
2. 异步用其他网卡测试同一域名（5 次，≥4 次成功即确认），逐次间隔 1s 避免触发限流
3. 确认被墙 → 加入黑名单（30 分钟自动过期恢复）
4. 未确认的域名 10 分钟内不再重复验证（冷却期）
5. 后续轮询分配网卡时自动跳过黑名单中的网卡
6. 持久化到 ~/.hypomux/blocked_domains.json
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
_VERIFY_RETRIES = 5
_VERIFY_MIN_SUCCESS = 4
_VERIFY_TIMEOUT = 5.0
_VERIFY_INTERVAL = 1.0          # 逐次验证间隔，避免触发目标限流
_EXPIRY_SECONDS = 1800          # 黑名单过期时间：30 分钟
_PERSIST_DIR = Path.home() / ".hypomux"
_PERSIST_FILE = _PERSIST_DIR / "blocked_domains.json"


def _normalize_domain(domain: str) -> str:
    return (domain or "").rstrip(".").lower().strip()


class BlockedDomainTracker:
    """线程安全的单网卡被墙域名追踪器。

    _blocked: {nic_name: {domain: expiry_timestamp}}
    """

    def __init__(self):
        self._lock = threading.Lock()
        self._blocked: Dict[str, Dict[str, float]] = {}
        self._pending_verifications: Dict[str, Set[str]] = {}
        self._enabled = True
        self._use_expiry = True
        self._log_callback = None
        self._load()

    @property
    def enabled(self) -> bool:
        return self._enabled

    @enabled.setter
    def enabled(self, value: bool):
        self._enabled = bool(value)

    @property
    def use_expiry(self) -> bool:
        return self._use_expiry

    @use_expiry.setter
    def use_expiry(self, value: bool):
        self._use_expiry = bool(value)

    def set_log_callback(self, callback):
        """设置 UI 控制台日志回调，用于在界面调试控制台同步显示被墙检测日志。"""
        self._log_callback = callback

    def _emit_log(self, message: str):
        logger.info(message)
        cb = self._log_callback
        if cb is not None:
            try:
                cb(message)
            except Exception:
                pass

    # ----- 过期/冷却清理 -----
    def _purge_expired(self):
        """清理所有已过期的黑名单记录（调用方必须持有 _lock）。"""
        if not self._use_expiry:
            return
        now = time.time()
        for nic_name in list(self._blocked):
            domains = self._blocked[nic_name]
            expired = [d for d, ts in domains.items() if ts <= now]
            for d in expired:
                del domains[d]
            if not domains:
                del self._blocked[nic_name]

    # ----- 查询 -----
    def is_blocked(self, nic_name: str, domain: str) -> bool:
        if not self._enabled:
            return False
        domain = _normalize_domain(domain)
        if not domain or not nic_name:
            return False
        with self._lock:
            self._purge_expired()
            entry = self._blocked.get(nic_name, {}).get(domain)
            return entry is not None

    def all_blocked(self) -> Dict[str, List[str]]:
        with self._lock:
            self._purge_expired()
            return {
                name: sorted(domains.keys())
                for name, domains in self._blocked.items()
                if domains
            }

    def remaining_seconds(self, nic_name: str, domain: str) -> int:
        """返回黑名单条目还剩多少秒过期，已过期或不存在返回 0。"""
        domain = _normalize_domain(domain)
        with self._lock:
            ts = self._blocked.get(nic_name, {}).get(domain)
            if ts is None:
                return 0
            return max(0, int(ts - time.time()))

    def remove_domain(self, nic_name: str, domain: str):
        domain = _normalize_domain(domain)
        with self._lock:
            if nic_name in self._blocked:
                self._blocked[nic_name].pop(domain, None)
                if not self._blocked[nic_name]:
                    del self._blocked[nic_name]

    def clear_all(self):
        with self._lock:
            self._blocked.clear()
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
        """连接失败时调用，在后台异步验证该域名是否被墙。"""
        if not self._enabled:
            return
        domain = _normalize_domain(domain)
        if not domain or not nic_name:
            return

        if self.is_blocked(nic_name, domain):
            return

        with self._lock:
            # 防止并发验证
            pending = self._pending_verifications.setdefault(nic_name, set())
            if domain in pending:
                return
            pending.add(domain)

        self._emit_log(f"[被墙检测] 网卡 [{nic_name}] 连接 {domain} 失败，启动后台验证...")

        verify_port = port if port > 0 else 443

        async def _verify():
            try:
                candidates = [n for n in all_nics if n.get("name") != nic_name]
                if not candidates:
                    return

                # 快速否决：前 3 次全部失败 → 其他网卡也连不上，直接放弃
                success_count = 0
                for attempt in range(3):
                    if attempt > 0:
                        await asyncio.sleep(_VERIFY_INTERVAL)
                    test_nic = candidates[attempt % len(candidates)]
                    self._emit_log(
                        f"[被墙检测] 快速验证 {attempt + 1}/3: "
                        f"用 [{test_nic.get('name')}] 测试 {domain}:{verify_port}"
                    )
                    if await self._test_connect(test_nic, domain, verify_port, loop):
                        success_count = 1
                        break
                else:
                    self._emit_log(
                        f"[被墙检测] 快速否决: 其他网卡也无法连接 {domain}，非单网卡被墙"
                    )
                    return

                # 快速验证通过 → 其他网卡能通，跑满 5 次确认
                for attempt in range(success_count, _VERIFY_RETRIES):
                    await asyncio.sleep(_VERIFY_INTERVAL)
                    test_nic = candidates[attempt % len(candidates)]
                    self._emit_log(
                        f"[被墙检测] 第 {attempt + 1}/{_VERIFY_RETRIES} 次验证: "
                        f"用 [{test_nic.get('name')}] 测试 {domain}:{verify_port}"
                    )
                    if await self._test_connect(test_nic, domain, verify_port, loop):
                        success_count += 1
                        if success_count >= _VERIFY_MIN_SUCCESS:
                            break
                    else:
                        self._emit_log(
                            f"[被墙检测] 第 {attempt + 1} 次验证失败: [{test_nic.get('name')}] {domain}"
                        )

                confirmed = success_count >= _VERIFY_MIN_SUCCESS
                if confirmed:
                    expiry = time.time() + _EXPIRY_SECONDS
                    with self._lock:
                        # 若验证期间用户清空了清单（pending 被 clear_all 清除），
                        # 尊重用户操作，放弃写回，避免刚清空又冒出条目
                        committed = domain in self._pending_verifications.get(nic_name, set())
                        if committed:
                            self._blocked.setdefault(nic_name, {})[domain] = expiry
                    if committed:
                        self._emit_log(
                            f"[被墙确认] 网卡 [{nic_name}] 无法访问 {domain}（其他网卡 {success_count}/{_VERIFY_RETRIES} 次成功，{_EXPIRY_SECONDS // 60} 分钟后自动恢复）"
                        )
                    else:
                        self._emit_log(
                            f"[被墙检测] 验证期间清单已被清空，放弃写入 [{nic_name}] -> {domain}"
                        )
                else:
                    self._emit_log(
                        f"[被墙检测] 验证未达标: [{nic_name}] -> {domain} ({success_count}/{_VERIFY_RETRIES})"
                    )
            except Exception as e:
                self._emit_log(f"[被墙检测] 验证异常: [{nic_name}] -> {domain}: {type(e).__name__}: {e}")
            finally:
                with self._lock:
                    self._pending_verifications.get(nic_name, set()).discard(domain)

        loop.create_task(_verify())

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
            dst_addr = None
            dns_error = ""
            # 目标本身是 IPv4 字面量时无需 DNS 解析，直接使用（IP 被墙检测路径）
            try:
                socket.inet_aton(domain)
                dst_addr = domain
            except OSError:
                pass

            if dst_addr is None:
                try:
                    addrs = await asyncio.wait_for(
                        loop.getaddrinfo(domain, test_port, family=socket.AF_INET, type=socket.SOCK_STREAM),
                        timeout=3.0,
                    )
                    dst_addr = addrs[0][4][0]
                except Exception as e:
                    dns_error = f"系统DNS: {type(e).__name__}"

            if dst_addr is None:
                try:
                    dst_addr = await self._resolve_via_public_dns(domain, nic, loop)
                except Exception as e2:
                    dns_error += f" | 公共DNS({nic_name}): {type(e2).__name__}"

            if dst_addr is None:
                self._emit_log(f"[被墙检测] [{nic_name}] DNS解析失败: {dns_error}")
                return False

            if_index = int(nic.get("if_index", nic.get("index", 0)) or 0)
            if not if_index:
                self._emit_log(f"[被墙检测] [{nic_name}] 无有效 IfIndex")
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
            self._emit_log(f"[被墙检测] [{nic_name}] TCP连接 {domain}:{test_port} 超时({_VERIFY_TIMEOUT}s)")
            return False
        except Exception as e:
            self._emit_log(f"[被墙检测] [{nic_name}] 连接 {domain}:{test_port} 失败: {type(e).__name__}: {e}")
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

        def _skip_name(data: bytes, offset: int) -> int:
            while offset < len(data) and data[offset] != 0:
                if data[offset] & 0xC0:
                    return offset + 2
                offset += 1 + data[offset]
            return offset + 1

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
            resp_id, flags, qdcount, ancount, _nscount, _arcount = struct.unpack("!HHHHHH", data[:12])
            if resp_id != query_id or (flags & 0x8000) == 0:
                raise ValueError("mismatched response")
            if (flags & 0x000F) != 0:
                raise ValueError(f"rcode={flags & 0x000F}")

            offset = 12
            for _ in range(qdcount):
                offset = _skip_name(data, offset)
                offset += 4

            for _ in range(ancount):
                offset = _skip_name(data, offset)
                if offset + 10 > len(data):
                    break
                rtype, rclass, _ttl, rdlen = struct.unpack("!HHIH", data[offset:offset + 10])
                offset += 10
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
            now = time.time()
            with self._lock:
                for nic_name, value in raw.items():
                    if isinstance(value, dict):
                        # 新格式: {domain: expiry_ts}
                        entries = {}
                        for domain, ts in value.items():
                            if isinstance(ts, (int, float)) and ts > now:
                                entries[_normalize_domain(domain)] = float(ts)
                        if entries:
                            self._blocked[nic_name] = entries
                    elif isinstance(value, list):
                        # 旧格式兼容: [domain, ...]，设置 30 分钟后过期
                        expiry = now + _EXPIRY_SECONDS
                        entries = {_normalize_domain(d): expiry for d in value if d}
                        if entries:
                            self._blocked[nic_name] = entries
            logger.info(f"已加载被墙域名清单: {len(self._blocked)} 张网卡（过期项已自动清除）")
        except Exception as e:
            logger.warning(f"加载被墙域名清单失败: {e}")

    def save(self):
        try:
            _PERSIST_DIR.mkdir(parents=True, exist_ok=True)
            now = time.time()
            with self._lock:
                self._purge_expired()
                payload = {
                    name: dict(domains)
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
