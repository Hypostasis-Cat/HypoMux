"""
HypoMux 代理后端模块 - v2.0（SOCKS5 + HTTP 双协议无感接管）

将 Phase 2 手搓验证通过的 asyncio SOCKS5 分发内核（proxy_core.py），
重构为可无缝接入 PySide6 UI 的 QThread 后端。

核心设计：
- 网卡参数由 UI 在启动时通过 selected_nics 传入，绝不写死。
  用户在界面勾选几张网卡，调度器就只在这几张里轮询。
- asyncio 事件循环跑在独立的 QThread 子线程里，绝不阻塞 PySide6 主事件循环。
- 用 QtCore.Signal 替代 print：
    * log_signal(str)     -- 每次新连接被分配给某张网卡时发出
    * traffic_signal(dict) -- 每秒发出一次各选中网卡的实时下行速度与连接数
- 提供 stop()，从主线程安全地叫停子线程里的 asyncio loop（"停止加速"）。

【神圣地基】handle_client 中的双保险物理绑定
    upstream_sock.bind((nic['ip'], 0))
    upstream_sock.setsockopt(socket.IPPROTO_IP, IP_UNICAST_IF, struct.pack("!I", nic['index']))
以及前置异步 DNS 解析逻辑，均一字不差地继承自 Phase 2，不得改动。
"""

import asyncio
import ctypes
import ssl
import random
import socket
import struct
import threading
import time
from ctypes import wintypes
from typing import Callable, Dict, List, Optional, Tuple
from urllib.parse import urlsplit

import psutil
from PySide6.QtCore import QThread, Signal

from utils.network_utils import get_adapter_if_indices
from utils.blocked_domain_tracker import get_tracker


def _is_winerror6_overlapped_cancel(context: dict) -> bool:
    """识别 Windows Proactor 在停止阶段产生的无效句柄取消噪声。"""
    message = str(context.get("message", ""))
    exception = context.get("exception")
    return (
        "Cancelling an overlapped future failed" in message
        and isinstance(exception, OSError)
        and getattr(exception, "winerror", None) == 6
    )


def _quiet_asyncio_exception_handler(loop, context: dict):
    """过滤停止阶段的 WinError 6 日志，其余异常仍交给 asyncio 默认处理。"""
    if _is_winerror6_overlapped_cancel(context):
        return
    loop.default_exception_handler(context)


def _shutdown_event_loop(loop: asyncio.AbstractEventLoop):
    """按 Windows Proactor 生命周期要求完整关闭事件循环。"""
    try:
        pending = [task for task in asyncio.all_tasks(loop) if not task.done()]
        for task in pending:
            task.cancel()
        if pending:
            loop.run_until_complete(asyncio.gather(*pending, return_exceptions=True))
    except Exception:
        pass
    try:
        loop.run_until_complete(loop.shutdown_asyncgens())
    except Exception:
        pass
    shutdown_default_executor = getattr(loop, "shutdown_default_executor", None)
    if callable(shutdown_default_executor):
        try:
            loop.run_until_complete(shutdown_default_executor())
        except Exception:
            pass
    loop.close()


# IPv4 下的 IP_UNICAST_IF：强制指定物理网卡出口，绕过 Windows 默认路由判定。
IP_UNICAST_IF = 31
AF_INET = 2
TCP_TABLE_OWNER_PID_ALL = 5
ERROR_INSUFFICIENT_BUFFER = 122
NO_ERROR = 0

# 这些 WinSock 错误代表本地接口/路由已失效，而不是目标网站拒绝连接。
# 仅对它们执行短时熔断，避免单个 CDN 或远端服务故障被误判为整张网卡故障。
_LOCAL_LINK_FAILURE_CODES = {10049, 10050, 10051, 10065, 1231, 1232}
_LINK_FAILURE_COOLDOWN_SECONDS = 15.0


def _is_local_link_failure(error: Exception) -> bool:
    """判断连接失败是否明确由本地网卡断开或路由消失引起。"""
    if not isinstance(error, OSError):
        return False
    return getattr(error, "winerror", None) in _LOCAL_LINK_FAILURE_CODES


class MIB_TCPROW_OWNER_PID(ctypes.Structure):
    _fields_ = [
        ("dwState", wintypes.DWORD),
        ("dwLocalAddr", wintypes.DWORD),
        ("dwLocalPort", wintypes.DWORD),
        ("dwRemoteAddr", wintypes.DWORD),
        ("dwRemotePort", wintypes.DWORD),
        ("dwOwningPid", wintypes.DWORD),
    ]


# ==========================================
# L4 连接调度器 (Balancer)
# ==========================================
class RoundRobinBalancer:
    """
    在"用户选中的网卡集合"内做轮询分发。

    网卡集合完全由外部注入（selected_nics），调度器不持有任何硬编码网卡。
    同时维护每张网卡的活跃连接计数，供 UI 仪表盘展示——这是判断分流是否
    真正生效（而非轮班倒）的关键指标。
    """

    def __init__(self, selected_nics: List[Dict]):
        if not selected_nics:
            raise ValueError("RoundRobinBalancer 至少需要 1 张网卡，selected_nics 为空")
        # 复制一份，避免外部列表被意外修改影响调度
        self.nics: List[Dict] = [dict(nic) for nic in selected_nics]
        self._current = 0
        self._lock = threading.Lock()
        # 按网卡 name 统计实时活跃连接数
        self._active: Dict[str, int] = {nic["name"]: 0 for nic in self.nics}
        self._unavailable_until: Dict[str, float] = {}

    def get_next_nic(self) -> Dict:
        with self._lock:
            now = time.monotonic()
            fallback = None
            for _ in range(len(self.nics)):
                nic = self.nics[self._current]
                self._current = (self._current + 1) % len(self.nics)
                fallback = fallback or nic
                if self._unavailable_until.get(nic["name"], 0) <= now:
                    return nic
            return fallback

    def get_next_nic_for_domain(
        self,
        domain: str,
        exclude_names: Optional[set[str]] = None,
    ) -> Optional[Dict]:
        """为指定域名分配网卡，自动跳过被墙网卡。

        `exclude_names` 仅供尚未建立的连接故障切换使用；调用方可以据此
        安全地请求另一张物理网卡，绝不会影响已经建立的会话。
        """
        tracker = get_tracker()
        excluded = exclude_names or set()

        with self._lock:
            fallback = None
            unavailable_fallback = None
            now = time.monotonic()
            attempts = len(self.nics)
            for _ in range(attempts):
                nic = self.nics[self._current]
                self._current = (self._current + 1) % len(self.nics)
                if nic["name"] in excluded:
                    continue
                if self._unavailable_until.get(nic["name"], 0) > now:
                    unavailable_fallback = unavailable_fallback or nic
                    continue
                if fallback is None:
                    fallback = nic
                if (
                    not tracker.enabled
                    or not domain
                    or not tracker.is_blocked(nic["name"], domain)
                ):
                    return nic
            # 全部被墙时仍允许首选候选尝试；但故障切换排除了所有网卡时，
            # 返回 None，防止把同一张失败网卡再次交给当前连接。
            return fallback or unavailable_fallback

    def on_connect(self, nic_name: str):
        with self._lock:
            self._active[nic_name] = self._active.get(nic_name, 0) + 1

    def on_disconnect(self, nic_name: str):
        with self._lock:
            if self._active.get(nic_name, 0) > 0:
                self._active[nic_name] -= 1

    def mark_temporarily_unavailable(self, nic_name: str):
        """本地链路断开时暂时跳过该网卡；无需后台探测，超时后自动恢复。"""
        with self._lock:
            self._unavailable_until[nic_name] = (
                time.monotonic() + _LINK_FAILURE_COOLDOWN_SECONDS
            )

    def on_connect_success(self, nic_name: str):
        with self._lock:
            self._unavailable_until.pop(nic_name, None)

    def active_connections(self) -> Dict[str, int]:
        with self._lock:
            return dict(self._active)


# ==========================================
# L4 连接调度器 (WeightedBalancer)
# ==========================================
class WeightedBalancer:
    """按用户设定的相对权重做平滑加权轮询。

    权重计算：nic_weight = schedule_weight / total_schedule_weight。
    该值只影响新连接的分配概率，不会对网卡进行带宽限速。
    平滑轮询会在短窗口内稳定地贴近用户比例，避免加权随机在少量并发
    连接时连续命中同一张网卡而造成瞬时拥塞。
    兼容 RoundRobinBalancer 的全部接口（get_next_nic / get_next_nic_for_domain
    / on_connect / on_disconnect / active_connections）。
    """

    def __init__(self, selected_nics: List[Dict], bandwidth_limits: Dict[str, int]):
        if not selected_nics:
            raise ValueError("WeightedBalancer 至少需要 1 张网卡")
        self.nics: List[Dict] = [dict(nic) for nic in selected_nics]
        self._lock = threading.Lock()
        self._active: Dict[str, int] = {nic["name"]: 0 for nic in self.nics}
        self._unavailable_until: Dict[str, float] = {}
        self._bandwidth: Dict[str, int] = {}
        for nic in self.nics:
            name = nic["name"]
            self._bandwidth[name] = max(int(bandwidth_limits.get(name, 0) or 0), 1)
        self._current_weight: Dict[str, int] = {nic["name"]: 0 for nic in self.nics}

    def _select_smooth_locked(self, candidates: List[Dict]) -> Optional[Dict]:
        if not candidates:
            return None
        total = 0
        selected = None
        for nic in candidates:
            name = nic["name"]
            weight = self._bandwidth.get(name, 1)
            total += weight
            self._current_weight[name] = self._current_weight.get(name, 0) + weight
            if selected is None or self._current_weight[name] > self._current_weight[selected["name"]]:
                selected = nic
        self._current_weight[selected["name"]] -= total
        return selected

    def get_next_nic(self) -> Dict:
        with self._lock:
            now = time.monotonic()
            candidates = [
                nic for nic in self.nics
                if self._unavailable_until.get(nic["name"], 0) <= now
            ]
            return self._select_smooth_locked(candidates or self.nics)

    def get_next_nic_for_domain(
        self,
        domain: str,
        exclude_names: Optional[set[str]] = None,
    ) -> Optional[Dict]:
        """平滑加权轮询，并自动跳过被墙或已失败的网卡。"""
        tracker = get_tracker()
        excluded = exclude_names or set()

        with self._lock:
            candidates = [n for n in self.nics if n["name"] not in excluded]
            if not candidates:
                return None
            now = time.monotonic()
            available = [
                nic for nic in candidates
                if self._unavailable_until.get(nic["name"], 0) <= now
            ]
            candidates = available or candidates
            if not tracker.enabled or not domain:
                return self._select_smooth_locked(candidates)

            unblocked = [n for n in candidates if not tracker.is_blocked(n["name"], domain)]
            if not unblocked:
                return self._select_smooth_locked(candidates)
            return self._select_smooth_locked(unblocked)

    def on_connect(self, nic_name: str):
        with self._lock:
            self._active[nic_name] = self._active.get(nic_name, 0) + 1

    def on_disconnect(self, nic_name: str):
        with self._lock:
            if self._active.get(nic_name, 0) > 0:
                self._active[nic_name] -= 1

    def mark_temporarily_unavailable(self, nic_name: str):
        with self._lock:
            self._unavailable_until[nic_name] = (
                time.monotonic() + _LINK_FAILURE_COOLDOWN_SECONDS
            )

    def on_connect_success(self, nic_name: str):
        with self._lock:
            self._unavailable_until.pop(nic_name, None)

    def active_connections(self) -> Dict[str, int]:
        with self._lock:
            return dict(self._active)


async def _connect_with_one_failover(
    *,
    loop: asyncio.AbstractEventLoop,
    balancer,
    schedule_target: str,
    dst_addr: str,
    dst_port: int,
    create_socket: Callable[[Dict], socket.socket],
    upstream_sockets: set,
    timeout: float,
    on_failure: Callable[[Dict, Exception, str], None],
    on_retry: Callable[[Dict, Exception], None],
    first_nic: Optional[Dict] = None,
) -> Tuple[socket.socket, Dict]:
    """在业务数据尚未转发前，最多尝试两张不同网卡建立 TCP 连接。

    这不是连接迁移：SOCKS/HTTP 成功响应会在本函数返回后才发送。因此失败
    时释放的是尚未承载业务数据的半成品 socket，不会重复提交 HTTP 请求或
    打断既有会话。
    """
    excluded: set[str] = set()
    max_attempts = min(2, max(1, len(getattr(balancer, "nics", ()))))
    last_error: Optional[Exception] = None

    for attempt in range(max_attempts):
        nic = first_nic if attempt == 0 and first_nic is not None else (
            balancer.get_next_nic_for_domain(schedule_target, excluded)
        )
        if nic is None:
            break

        sock = None
        balancer.on_connect(nic["name"])
        failure_stage = "bind"
        try:
            sock = create_socket(nic)
            upstream_sockets.add(sock)
            failure_stage = "connect"
            await asyncio.wait_for(
                loop.sock_connect(sock, (dst_addr, dst_port)), timeout=timeout
            )
            balancer.on_connect_success(nic["name"])
            return sock, nic
        except asyncio.CancelledError:
            if sock is not None:
                try:
                    sock.close()
                except Exception:
                    pass
                upstream_sockets.discard(sock)
            balancer.on_disconnect(nic["name"])
            raise
        except Exception as error:
            last_error = error
            excluded.add(nic["name"])
            if _is_local_link_failure(error):
                balancer.mark_temporarily_unavailable(nic["name"])
            if sock is not None:
                try:
                    sock.close()
                except Exception:
                    pass
                upstream_sockets.discard(sock)
            balancer.on_disconnect(nic["name"])
            try:
                on_failure(nic, error, failure_stage)
            except Exception:
                pass
            if attempt + 1 < max_attempts:
                try:
                    on_retry(nic, error)
                except Exception:
                    pass

    raise ConnectionError("all selected adapters failed to connect") from last_error


# ==========================================
# ProxyWorker：asyncio SOCKS5 内核的 QThread 封装
# ==========================================
class ProxyWorker(QThread):
    """
    在独立子线程中运行 asyncio SOCKS5 + HTTP 分发代理。

    Signals:
        log_signal(str)      -- 连接调度 / 错误日志，喂给 UI 控制台
        traffic_signal(dict) -- 每秒一次的各网卡实时吞吐与连接数快照
        started_ok(str)      -- SOCKS 和 HTTP 端口都成功监听后发出
        stopped(str)         -- 代理已完全停止
        error_signal(str)    -- 启动失败等致命错误
    """

    log_signal = Signal(str)
    traffic_signal = Signal(dict)
    started_ok = Signal(str)
    stopped = Signal(str)
    error_signal = Signal(str)

    STOP_TASK_TIMEOUT = 2.0
    MONITOR_STOP_TIMEOUT = 0.5
    SERVER_CLOSE_TIMEOUT = 1.0
    TCP_CONNECT_TIMEOUT = 6.0

    def __init__(
        self,
        selected_nics: List[Dict],
        listen_host: str = "127.0.0.1",
        listen_port: int = 10800,
        http_port: Optional[int] = None,
        use_weighted: bool = False,
        bandwidth_limits: Optional[Dict[str, int]] = None,
        parent=None,
    ):
        super().__init__(parent)
        self._selected_nics = [dict(nic) for nic in selected_nics]
        self._resolve_if_indices()
        self._listen_host = listen_host
        self._listen_port = listen_port
        self._http_port = http_port if http_port is not None else listen_port + 1

        if use_weighted:
            self.balancer = WeightedBalancer(self._selected_nics, bandwidth_limits or {})
        else:
            self.balancer = RoundRobinBalancer(self._selected_nics)

        # 以下三个对象都在子线程的 asyncio loop 内创建/使用
        self._loop: Optional[asyncio.AbstractEventLoop] = None
        self._stop_event: Optional[asyncio.Event] = None
        self.socks_server = None
        self.http_server = None
        # 跟踪在途连接任务，停止时主动取消
        self._client_tasks: "set[asyncio.Task]" = set()
        self._client_writers = set()
        self._upstream_sockets = set()
        self._monitor_task: Optional[asyncio.Task] = None
        # 主线程在 loop 就绪前调用 stop() 的兜底标记
        self._stop_requested = False
        self._passthrough_mode = False

    # ---------- 网卡接口索引解析与 socket 接口强绑定 ----------
    def _resolve_if_indices(self):
        """用 GetAdaptersAddresses 把每张网卡的出口 IP 反查为权威 IfIndex。

        UI 扫描阶段记录的 index 可能与底层 IfIndex 不一致（或网卡重插后变动），
        这里用出口 IP 现场反查一次，查不到则回退到原 index。结果写入
        nic['if_index']，供 IP_UNICAST_IF 接口强绑定使用。
        """
        try:
            ip_to_index = get_adapter_if_indices()
        except Exception:
            ip_to_index = {}

        for nic in self._selected_nics:
            fallback = int(nic.get("index", 0) or 0)
            resolved = ip_to_index.get(nic.get("ip", ""), fallback)
            nic["if_index"] = int(resolved or fallback)

    @staticmethod
    def _create_bound_upstream_socket(nic) -> socket.socket:
        """创建出站 TCP socket 并用 IP_UNICAST_IF 把它死锁在目标网卡上。

        关键修复（WinError 10049）：先注入 IP_UNICAST_IF（IPPROTO_IP 级别，常量 31），
        把出口物理网卡锁死在 nic['if_index'] 上，再执行 bind()。这样即便两张网卡
        同处一个网段，内核也按接口索引而非默认路由选路，不会再 bind 命中错网卡。

        IPv4 下 IP_UNICAST_IF 的接口索引必须为【网络字节序（大端）】，
        故用 struct.pack('!I', if_index) 转换。
        """
        if_index = int(nic.get("if_index", nic.get("index", 0)) or 0)
        sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        try:
            sock.setblocking(False)
            # 1) 先注入接口索引强绑定（必须在 bind() 之前）
            sock.setsockopt(
                socket.IPPROTO_IP,
                IP_UNICAST_IF,
                struct.pack("!I", if_index),  # 大端 / 网络字节序
            )
            # 2) 再尝试 bind 到本地出口 IP（仅用于固定源地址）。即便失败，
            #    上面的 IP_UNICAST_IF 已保证出口网卡正确，故 bind 失败可降级忽略。
            local_ip = nic.get("ip")
            if local_ip:
                try:
                    sock.bind((local_ip, 0))
                except OSError:
                    pass
        except Exception:
            sock.close()
            raise
        return sock

    # ---------- QThread 入口 ----------
    def run(self):
        """子线程主体：建立独立 asyncio loop 并跑到收到停止信号。"""
        self._loop = asyncio.new_event_loop()
        self._loop.set_exception_handler(_quiet_asyncio_exception_handler)
        asyncio.set_event_loop(self._loop)
        try:
            self._loop.run_until_complete(self._serve())
        except Exception as e:
            self.error_signal.emit(f"代理内核异常退出: {type(e).__name__}: {e}")
        finally:
            try:
                _shutdown_event_loop(self._loop)
            finally:
                self._loop = None
            self.stopped.emit("代理已停止")

    async def _serve(self):
        self._stop_event = asyncio.Event()
        # 若主线程在本事件创建前就请求过停止，这里立刻退出
        if self._stop_requested:
            return

        try:
            self.socks_server = await asyncio.start_server(
                self._handle_client, self._listen_host, self._listen_port
            )
            self.http_server = await asyncio.start_server(
                self._handle_http_client, self._listen_host, self._http_port
            )
        except Exception as e:
            await self._aggressive_teardown()
            self.error_signal.emit(
                f"无法监听 {self._listen_host}:{self._listen_port} / {self._http_port} -- {e}"
            )
            return

        nic_names = ", ".join(nic["name"] for nic in self._selected_nics)
        self.log_signal.emit(
            f"[HypoMux] SOCKS5+HTTP 分发引擎已启动 | SOCKS {self._listen_host}:{self._listen_port} "
            f"| HTTP {self._listen_host}:{self._http_port} | 参与分流网卡: {nic_names}"
        )
        self.started_ok.emit(
            f"socks={self._listen_host}:{self._listen_port};http={self._listen_host}:{self._http_port}"
        )

        self._monitor_task = asyncio.create_task(self._traffic_monitor())

        try:
            await self._stop_event.wait()
        finally:
            await self._aggressive_teardown()
            self.log_signal.emit("[HypoMux] 收到停止指令，已强制关闭监听并销毁所有在途连接")

    def stop(self):
        """从主线程安全地请求停止（不阻塞 UI）。"""
        self._stop_requested = True
        loop = self._loop
        event = self._stop_event
        if loop is not None and event is not None:
            loop.call_soon_threadsafe(event.set)
            loop.call_soon_threadsafe(self._force_close_servers)
            loop.call_soon_threadsafe(self._force_close_connections)

    def set_passthrough_mode(self, enabled: bool):
        """切换为直连保活模式：继续提供代理入口，但不再多网卡分流。"""
        self._passthrough_mode = enabled
        if enabled:
            self.log_signal.emit("[Steam] 已切换为直连保活模式，本地代理仅用于防止 Steam 离线")

    @staticmethod
    def _decode_tcp_port(raw_port: int) -> int:
        return socket.ntohs(raw_port & 0xFFFF)

    @staticmethod
    def _decode_tcp_addr(raw_addr: int) -> str:
        return socket.inet_ntoa(struct.pack("<L", raw_addr))

    @staticmethod
    def _pid_is_steam(pid: Optional[int]) -> bool:
        if not pid:
            return False
        try:
            name = (psutil.Process(int(pid)).name() or "").lower()
            return name in {"steam.exe", "steamwebhelper.exe"}
        except Exception:
            return False

    @classmethod
    def _find_tcp_owner_pid(cls, local_addr: Tuple[str, int], peer_addr: Tuple[str, int]) -> Optional[int]:
        """按 TCP 四元组反查连接所属客户端 PID。"""
        try:
            iphlpapi = ctypes.windll.Iphlpapi
            get_table = iphlpapi.GetExtendedTcpTable
            get_table.argtypes = [
                ctypes.c_void_p,
                ctypes.POINTER(wintypes.DWORD),
                wintypes.BOOL,
                wintypes.ULONG,
                wintypes.ULONG,
                wintypes.ULONG,
            ]
            get_table.restype = wintypes.DWORD

            size = wintypes.DWORD(0)
            ret = get_table(None, ctypes.byref(size), False, AF_INET, TCP_TABLE_OWNER_PID_ALL, 0)
            if ret not in (ERROR_INSUFFICIENT_BUFFER, NO_ERROR):
                return None

            buffer = ctypes.create_string_buffer(size.value)
            ret = get_table(buffer, ctypes.byref(size), False, AF_INET, TCP_TABLE_OWNER_PID_ALL, 0)
            if ret != NO_ERROR:
                return None

            count = wintypes.DWORD.from_buffer_copy(buffer.raw[:ctypes.sizeof(wintypes.DWORD)]).value
            row_size = ctypes.sizeof(MIB_TCPROW_OWNER_PID)
            offset = ctypes.sizeof(wintypes.DWORD)
            wanted_local_ip, wanted_local_port = local_addr
            wanted_peer_ip, wanted_peer_port = peer_addr

            for index in range(count):
                row = MIB_TCPROW_OWNER_PID.from_buffer_copy(buffer, offset + index * row_size)
                if (
                    cls._decode_tcp_addr(row.dwLocalAddr) == wanted_peer_ip
                    and cls._decode_tcp_port(row.dwLocalPort) == wanted_peer_port
                    and cls._decode_tcp_addr(row.dwRemoteAddr) == wanted_local_ip
                    and cls._decode_tcp_port(row.dwRemotePort) == wanted_local_port
                ):
                    return int(row.dwOwningPid)
        except Exception:
            return None
        return None

    def _allow_passthrough_client(self, writer) -> bool:
        """保活模式下只允许 Steam/SteamWebHelper 继续使用本地代理。"""
        if not self._passthrough_mode:
            return True
        try:
            sock = writer.get_extra_info("socket")
            if sock is None:
                return False
            local_addr = sock.getsockname()
            peer_addr = sock.getpeername()
            if not isinstance(local_addr, tuple) or not isinstance(peer_addr, tuple):
                return False
            pid = self._find_tcp_owner_pid(local_addr, peer_addr)
            return self._pid_is_steam(pid)
        except Exception:
            return False

    def _force_close_servers(self):
        for server in (self.socks_server, self.http_server):
            if server is not None:
                try:
                    server.close()
                except Exception:
                    pass

    def _force_close_connections(self):
        for writer in list(self._client_writers):
            self._abort_writer(writer)
        for sock in list(self._upstream_sockets):
            self._close_socket(sock)

    @staticmethod
    def _abort_writer(writer):
        try:
            transport = getattr(writer, "transport", None)
            if transport is not None:
                transport.abort()
            else:
                writer.close()
        except Exception:
            try:
                writer.close()
            except Exception:
                pass

    @staticmethod
    def _close_socket(sock):
        try:
            sock.shutdown(socket.SHUT_RDWR)
        except Exception:
            pass
        try:
            sock.close()
        except Exception:
            pass

    async def _cancel_tasks_with_timeout(self, tasks, timeout: float, label: str):
        waiting = [task for task in tasks if task is not None and not task.done()]
        if not waiting:
            return

        for task in waiting:
            task.cancel()

        _, pending = await asyncio.wait(waiting, timeout=timeout)
        if pending:
            self.log_signal.emit(
                f"[停止] {label}仍有 {len(pending)} 个任务未及时退出，已强制跳过等待"
            )

    async def _wait_server_closed(self, server, label: str):
        if server is None:
            return
        try:
            await asyncio.wait_for(server.wait_closed(), timeout=self.SERVER_CLOSE_TIMEOUT)
        except asyncio.TimeoutError:
            self.log_signal.emit(f"[停止] {label}监听关闭等待超时，已跳过")
        except Exception:
            pass

    async def _aggressive_teardown(self):
        self._force_close_servers()
        self._force_close_connections()

        monitor_task = self._monitor_task
        self._monitor_task = None
        if monitor_task is not None:
            await self._cancel_tasks_with_timeout(
                [monitor_task], self.MONITOR_STOP_TIMEOUT, "流量监控"
            )

        tasks = list(self._client_tasks)
        if tasks:
            await self._cancel_tasks_with_timeout(
                tasks, self.STOP_TASK_TIMEOUT, "连接清理"
            )

        self._client_tasks.clear()
        self._client_writers.clear()
        self._upstream_sockets.clear()

        await self._wait_server_closed(self.socks_server, "SOCKS5")
        await self._wait_server_closed(self.http_server, "HTTP")

        self.socks_server = None
        self.http_server = None

    # ---------- 连接处理（神圣地基所在） ----------
    async def _handle_client(self, reader, writer):
        task = asyncio.current_task()
        if task is not None:
            self._client_tasks.add(task)
        self._client_writers.add(writer)

        nic = None
        upstream_sock = None
        relay_tasks = []
        try:
            if not self._allow_passthrough_client(writer):
                writer.close()
                return

            # 1. SOCKS5 握手
            version, nmethods = await reader.readexactly(2)
            methods = await reader.readexactly(nmethods)
            writer.write(b"\x05\x00")
            await writer.drain()

            # 2. 接收请求
            version, cmd, rsv, atyp = await reader.readexactly(4)
            if cmd != 1:  # 仅支持 CONNECT
                writer.close()
                return

            dst_domain = None
            loop = asyncio.get_running_loop()

            if atyp == 1:  # IPv4
                dst_addr = socket.inet_ntoa(await reader.readexactly(4))
            elif atyp == 3:  # 域名
                domain_len = ord(await reader.readexactly(1))
                dst_domain = (await reader.readexactly(domain_len)).decode()
                try:
                    # 【关键修复】前置异步解析 DNS，绕过单网卡解析死锁
                    addr_info = await loop.getaddrinfo(dst_domain, None, family=socket.AF_INET, type=socket.SOCK_STREAM)
                    dst_addr = addr_info[0][4][0]
                except Exception as e:
                    self.log_signal.emit(f"[DNS失败] 无法解析域名 {dst_domain}: {e}")
                    writer.close()
                    return
            elif atyp == 4:  # IPv6 暂不支持
                writer.close()
                return
            else:
                writer.close()
                return

            dst_port = struct.unpack("!H", await reader.readexactly(2))[0]

            target_display = dst_domain if dst_domain else dst_addr
            if self._passthrough_mode:
                if not self._allow_passthrough_client(writer):
                    writer.close()
                    return
                upstream_sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
                upstream_sock.setblocking(False)
                self._upstream_sockets.add(upstream_sock)
                await asyncio.wait_for(
                    loop.sock_connect(upstream_sock, (dst_addr, dst_port)),
                    timeout=self.TCP_CONNECT_TIMEOUT,
                )
            else:
                # 3. 【L4 调度】仅在建连成功前允许切换一次备用网卡。
                domain_for_schedule = dst_domain or dst_addr
                def on_failure(failed_nic, error, stage):
                    self.log_signal.emit(
                        f"[{'连接' if stage == 'connect' else '绑定'}失败] 网卡: "
                        f"{failed_nic['name']} -> {target_display}: "
                        f"{type(error).__name__}: {error}"
                    )
                    if stage == "connect":
                        get_tracker().on_connect_failure(
                            nic_name=failed_nic["name"],
                            domain=domain_for_schedule,
                            port=dst_port,
                            failed_nic=failed_nic,
                            all_nics=self._selected_nics,
                            loop=self._loop,
                        )

                def on_retry(failed_nic, _error):
                    self.log_signal.emit(
                        f"[故障切换] [{failed_nic['name']}] 建连失败，"
                        f"正在尝试另一张网卡: {target_display}:{dst_port}"
                    )

                try:
                    upstream_sock, nic = await _connect_with_one_failover(
                        loop=loop,
                        balancer=self.balancer,
                        schedule_target=domain_for_schedule,
                        dst_addr=dst_addr,
                        dst_port=dst_port,
                        create_socket=self._create_bound_upstream_socket,
                        upstream_sockets=self._upstream_sockets,
                        timeout=self.TCP_CONNECT_TIMEOUT,
                        on_failure=on_failure,
                        on_retry=on_retry,
                    )
                except Exception:
                    writer.write(b"\x05\x05\x00\x01\x00\x00\x00\x00\x00\x00")
                    await writer.drain()
                    writer.close()
                    return
                self.log_signal.emit(
                    f"[调度分配] 新连接 -> [{nic['name']}] | 目标: {target_display}:{dst_port}"
                )

            # 连接成功，回应 SOCKS5 客户端
            writer.write(b"\x05\x00\x00\x01\x00\x00\x00\x00\x00\x00")
            await writer.drain()

            # 6. 双向透传
            relay_tasks = [
                asyncio.create_task(self._relay_to_sock(reader, upstream_sock, loop)),
                asyncio.create_task(self._relay_from_sock(upstream_sock, writer, loop)),
            ]
            self._client_tasks.update(relay_tasks)
            _, pending = await asyncio.wait(
                relay_tasks,
                return_when=asyncio.FIRST_COMPLETED,
            )
            for t in pending:
                t.cancel()

        except (asyncio.IncompleteReadError, ConnectionResetError, asyncio.CancelledError):
            pass
        except Exception as e:
            self.log_signal.emit(f"[连接异常] {type(e).__name__}: {e}")
        finally:
            try:
                writer.close()
            except Exception:
                pass
            if upstream_sock is not None:
                try:
                    upstream_sock.close()
                except Exception:
                    pass
                self._upstream_sockets.discard(upstream_sock)
            self._client_writers.discard(writer)
            if nic is not None:
                self.balancer.on_disconnect(nic["name"])
            for relay_task in relay_tasks:
                self._client_tasks.discard(relay_task)
            if task is not None:
                self._client_tasks.discard(task)

    async def _handle_http_client(self, reader, writer):
        task = asyncio.current_task()
        if task is not None:
            self._client_tasks.add(task)
        self._client_writers.add(writer)

        nic = None
        upstream_sock = None
        relay_tasks = []
        try:
            if not self._allow_passthrough_client(writer):
                writer.close()
                return

            try:
                header_blob = await reader.readuntil(b"\r\n\r\n")
            except asyncio.LimitOverrunError:
                writer.write(b"HTTP/1.1 431 Request Header Fields Too Large\r\nConnection: close\r\n\r\n")
                await writer.drain()
                return

            try:
                header_text = header_blob.decode("iso-8859-1")
                header_lines = header_text.split("\r\n")
                method, target, version = header_lines[0].split(" ", 2)
            except Exception:
                writer.write(b"HTTP/1.1 400 Bad Request\r\nConnection: close\r\n\r\n")
                await writer.drain()
                return

            method_upper = method.upper()
            outbound_header = None

            if method_upper == "CONNECT":
                dst_host, dst_port = self._split_host_port(target, default_port=443)
            else:
                parsed = urlsplit(target)
                if parsed.hostname:
                    dst_host = parsed.hostname
                    dst_port = parsed.port or (443 if parsed.scheme.lower() == "https" else 80)
                    path = parsed.path or "/"
                    if parsed.query:
                        path += f"?{parsed.query}"
                    outbound_header = self._build_origin_http_header(
                        method, path, version, header_lines
                    )
                else:
                    host_header = self._find_header(header_lines, "host")
                    if not host_header:
                        writer.write(b"HTTP/1.1 400 Bad Request\r\nConnection: close\r\n\r\n")
                        await writer.drain()
                        return
                    dst_host, dst_port = self._split_host_port(host_header, default_port=80)
                    outbound_header = header_blob

            if not dst_host or not dst_port:
                writer.write(b"HTTP/1.1 400 Bad Request\r\nConnection: close\r\n\r\n")
                await writer.drain()
                return

            if self._passthrough_mode and not self._allow_passthrough_client(writer):
                writer.close()
                return

            loop = asyncio.get_running_loop()
            try:
                upstream_sock, nic, target_display = await self._open_bound_upstream(
                    dst_host, dst_port, "HTTP"
                )
            except Exception as e:
                if not self._passthrough_mode:
                    self.log_signal.emit(f"[HTTP 连通失败] {dst_host}:{dst_port} -- {e}")
                writer.write(b"HTTP/1.1 502 Bad Gateway\r\nConnection: close\r\n\r\n")
                await writer.drain()
                return

            if method_upper == "CONNECT":
                writer.write(b"HTTP/1.1 200 Connection Established\r\nProxy-Agent: HypoMux\r\n\r\n")
                await writer.drain()
            else:
                await loop.sock_sendall(upstream_sock, outbound_header)

            relay_tasks = [
                asyncio.create_task(self._relay_to_sock(reader, upstream_sock, loop)),
                asyncio.create_task(self._relay_from_sock(upstream_sock, writer, loop)),
            ]
            self._client_tasks.update(relay_tasks)
            _, pending = await asyncio.wait(
                relay_tasks,
                return_when=asyncio.FIRST_COMPLETED,
            )
            for t in pending:
                t.cancel()

        except (asyncio.IncompleteReadError, ConnectionResetError, asyncio.CancelledError):
            pass
        except Exception as e:
            self.log_signal.emit(f"[HTTP 连接异常] {type(e).__name__}: {e}")
        finally:
            try:
                writer.close()
            except Exception:
                pass
            if upstream_sock is not None:
                try:
                    upstream_sock.close()
                except Exception:
                    pass
                self._upstream_sockets.discard(upstream_sock)
            self._client_writers.discard(writer)
            if nic is not None:
                self.balancer.on_disconnect(nic["name"])
            for relay_task in relay_tasks:
                self._client_tasks.discard(relay_task)
            if task is not None:
                self._client_tasks.discard(task)

    async def _open_bound_upstream(self, dst_host: str, dst_port: int, protocol: str):
        loop = asyncio.get_running_loop()
        try:
            addr_info = await loop.getaddrinfo(
                dst_host, dst_port, family=socket.AF_INET, type=socket.SOCK_STREAM
            )
            dst_addr = addr_info[0][4][0]
        except Exception as e:
            raise RuntimeError(f"DNS 解析失败: {e}") from e

        if self._passthrough_mode:
            nic = None
            upstream_sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
            upstream_sock.setblocking(False)
            self._upstream_sockets.add(upstream_sock)
            try:
                await asyncio.wait_for(
                    loop.sock_connect(upstream_sock, (dst_addr, dst_port)),
                    timeout=self.TCP_CONNECT_TIMEOUT,
                )
            except Exception:
                upstream_sock.close()
                self._upstream_sockets.discard(upstream_sock)
                raise
        else:
            def on_failure(failed_nic, _error, stage):
                if stage == "connect":
                    get_tracker().on_connect_failure(
                        nic_name=failed_nic["name"],
                        domain=dst_host,
                        port=dst_port,
                        failed_nic=failed_nic,
                        all_nics=self._selected_nics,
                        loop=self._loop,
                    )

            def on_retry(failed_nic, error):
                self.log_signal.emit(
                    f"[{protocol} 故障切换] [{failed_nic['name']}] 建连失败 "
                    f"({type(error).__name__})，正在尝试另一张网卡"
                )

            upstream_sock, nic = await _connect_with_one_failover(
                loop=loop,
                balancer=self.balancer,
                schedule_target=dst_host,
                dst_addr=dst_addr,
                dst_port=dst_port,
                create_socket=self._create_bound_upstream_socket,
                upstream_sockets=self._upstream_sockets,
                timeout=self.TCP_CONNECT_TIMEOUT,
                on_failure=on_failure,
                on_retry=on_retry,
            )

        target_display = f"{dst_host}({dst_addr})"
        if nic is None:
            pass
        else:
            self.log_signal.emit(
                f"[{protocol} 调度分配] 新连接 -> [{nic['name']}] | 目标: {target_display}:{dst_port}"
            )
        return upstream_sock, nic, target_display

    @staticmethod
    def _find_header(header_lines: List[str], name: str) -> str:
        prefix = f"{name.lower()}:"
        for line in header_lines[1:]:
            if line.lower().startswith(prefix):
                return line.split(":", 1)[1].strip()
        return ""

    @staticmethod
    def _build_origin_http_header(method: str, path: str, version: str, header_lines: List[str]) -> bytes:
        hop_by_hop = {"proxy-connection", "proxy-authorization"}
        headers = []
        for line in header_lines[1:]:
            if not line:
                continue
            name = line.split(":", 1)[0].strip().lower()
            if name in hop_by_hop:
                continue
            headers.append(line)
        return (f"{method} {path} {version}\r\n" + "\r\n".join(headers) + "\r\n\r\n").encode("iso-8859-1")

    @staticmethod
    def _split_host_port(value: str, default_port: int):
        host = value.strip()
        if not host:
            return "", 0
        if host.startswith("["):
            return "", 0
        if ":" in host:
            host_part, port_part = host.rsplit(":", 1)
            try:
                return host_part.strip(), int(port_part)
            except ValueError:
                return "", 0
        return host, default_port

    @staticmethod
    async def _relay_to_sock(reader, sock, loop):
        try:
            while True:
                data = await reader.read(65536)
                if not data:
                    break
                await loop.sock_sendall(sock, data)
        except Exception:
            pass
        finally:
            try:
                sock.close()
            except Exception:
                pass

    @staticmethod
    async def _relay_from_sock(sock, writer, loop):
        try:
            while True:
                data = await loop.sock_recv(sock, 65536)
                if not data:
                    break
                writer.write(data)
                await writer.drain()
        except Exception:
            pass
        finally:
            try:
                sock.close()
            except Exception:
                pass

    # ---------- L5 流量遥测 ----------
    async def _traffic_monitor(self):
        """
        每秒采样一次各选中网卡的真实下行/上行速率（数据源：Windows 内核计数器），
        连同实时活跃连接数打包成 dict，通过 traffic_signal 发给 UI。

        dict 结构示例：
            {
              "以太网": {"index": 19, "down_mbps": 12.3, "up_mbps": 0.4, "connections": 8},
              "WLAN":  {"index": 11, "down_mbps": 11.8, "up_mbps": 0.3, "connections": 7},
              "_total": {"down_mbps": 24.1, "up_mbps": 0.7, "connections": 15},
            }
        """
        def snapshot():
            io = psutil.net_io_counters(pernic=True)
            recv, sent = {}, {}
            for nic in self._selected_nics:
                c = io.get(nic["name"])
                recv[nic["name"]] = c.bytes_recv if c else 0
                sent[nic["name"]] = c.bytes_sent if c else 0
            return recv, sent

        last_recv, last_sent = snapshot()

        try:
            while True:
                await asyncio.sleep(1.0)
                now_recv, now_sent = snapshot()
                active = self.balancer.active_connections()

                payload: Dict[str, Dict] = {}
                total_down = total_up = 0.0
                total_conn = 0
                for nic in self._selected_nics:
                    name = nic["name"]
                    down = (now_recv[name] - last_recv[name]) / 1024 / 1024
                    up = (now_sent[name] - last_sent[name]) / 1024 / 1024
                    conn = active.get(name, 0)
                    payload[name] = {
                        "index": nic["index"],
                        "down_mbps": round(max(down, 0.0), 2),
                        "up_mbps": round(max(up, 0.0), 2),
                        "connections": conn,
                    }
                    total_down += max(down, 0.0)
                    total_up += max(up, 0.0)
                    total_conn += conn

                payload["_total"] = {
                    "down_mbps": round(total_down, 2),
                    "up_mbps": round(total_up, 2),
                    "connections": total_conn,
                }
                self.traffic_signal.emit(payload)

                last_recv, last_sent = now_recv, now_sent
        except asyncio.CancelledError:
            pass


# ==========================================
# 任务1：多端口多出站池 (MultiPortProxyWorker)
# ==========================================
# 在同一个子线程的 asyncio loop 内同时开启三个隔离的本地 SOCKS5 监听。
# 默认优先使用 2001/2002/2003；若被 Hyper-V/HNS 排除或被其他程序占用，
# 自动回退到 Windows 分配的可用端口，并把实际端口同步给 sing-box。
# 供 sing-box TUN 的三个 socks 出站对接，实现进程级分流 + 物理多卡叠加。
PORT_ETHERNET = 2001
PORT_WIFI = 2002
PORT_AGGREGATION = 2003

# 网卡分组：有线 6 / PPP 拨号 23，其余（71 等）归为无线
_IFTYPE_ETHERNET = 6
_IFTYPE_PPP = 23


def classify_nics(selected_nics: List[Dict]) -> Tuple[List[Dict], List[Dict]]:
    """把选中网卡分成 (有线/PPP 组, 无线组)。

    判定优先级：is_ppp / iftype==23 / iftype==6 归有线组；
    iftype==71 或别名含 WLAN/Wi-Fi/Wireless 归无线组；
    无法判定者默认进有线组（更安全的单卡分流出口）。
    """
    wired: List[Dict] = []
    wifi: List[Dict] = []
    for nic in selected_nics:
        iftype = int(nic.get("iftype", -1) or -1)
        alias = str(nic.get("name", nic.get("alias", "")))
        is_ppp = bool(nic.get("is_ppp", False)) or iftype == _IFTYPE_PPP
        is_wifi = (iftype == 71) or any(
            kw.lower() in alias.lower() for kw in ("wlan", "wi-fi", "wifi", "wireless", "无线")
        )
        if is_ppp or iftype == _IFTYPE_ETHERNET:
            wired.append(nic)
        elif is_wifi:
            wifi.append(nic)
        else:
            wired.append(nic)
    return wired, wifi


class _MergedBalancerView:
    """把多个 RoundRobinBalancer 的实时连接数合并成一个只读视图，
    供复用 ProxyWorker._traffic_monitor 时统计 active_connections()。"""

    def __init__(self, balancers):
        self._balancers = list(balancers)

    def active_connections(self) -> Dict[str, int]:
        merged: Dict[str, int] = {}
        for bal in self._balancers:
            for name, cnt in bal.active_connections().items():
                merged[name] = merged.get(name, 0) + cnt
        return merged


class _SocksUdpRelayProtocol(asyncio.DatagramProtocol):
    """SOCKS5 UDP ASSOCIATE 本地 relay。"""

    def __init__(self, owner: "MultiPortProxyWorker", balancer: RoundRobinBalancer, channel: str):
        self.owner = owner
        self.balancer = balancer
        self.channel = channel
        self.transport = None

    def connection_made(self, transport):
        self.transport = transport
        self.owner._udp_transports.add(transport)

    def datagram_received(self, data: bytes, addr):
        task = asyncio.create_task(
            self.owner._handle_udp_packet(data, addr, self.balancer, self.channel, self.transport)
        )
        self.owner._client_tasks.add(task)
        task.add_done_callback(lambda t: self.owner._client_tasks.discard(t))

    def connection_lost(self, exc):
        if self.transport is not None:
            self.owner._udp_transports.discard(self.transport)


class MultiPortProxyWorker(QThread):
    """多端口多出站池 SOCKS5 引擎（任务1）。

    与 ProxyWorker 复用同一套接口强绑定 / 透传逻辑，但同时开三个监听端口，
    每个端口绑定到不同的网卡选择策略（balancer）。

    Signals 与 ProxyWorker 对齐，便于 UI 复用同一套槽函数。
    """

    log_signal = Signal(str)
    traffic_signal = Signal(dict)
    connectivity_signal = Signal(str)
    started_ok = Signal(str)
    stopped = Signal(str)
    error_signal = Signal(str)

    STOP_TASK_TIMEOUT = 2.0
    SERVER_CLOSE_TIMEOUT = 1.0
    DNS_SERVERS = ("223.5.5.5", "119.29.29.29")
    DNS53_TIMEOUT = 0.8
    TCP_CONNECT_TIMEOUT = 6.0
    DOH_PRESETS = {
        "alidns": (("223.5.5.5", "dns.alidns.com", "/dns-query"),),
        "dnspod": (
            ("1.12.12.12", "doh.pub", "/dns-query"),
            ("120.53.53.53", "doh.pub", "/dns-query"),
        ),
        "google": (
            ("8.8.8.8", "dns.google", "/dns-query"),
            ("8.8.4.4", "dns.google", "/dns-query"),
        ),
    }
    DEFAULT_DOH_ENDPOINTS = (
        ("223.5.5.5", "dns.alidns.com", "/dns-query"),
        ("1.12.12.12", "doh.pub", "/dns-query"),
        ("120.53.53.53", "doh.pub", "/dns-query"),
        ("8.8.8.8", "dns.google", "/dns-query"),
        ("8.8.4.4", "dns.google", "/dns-query"),
    )
    DNS_CACHE_TTL = 180.0
    DNS_PREFLIGHT_DOMAIN = "www.msftconnecttest.com"
    DNS_PREFLIGHT_DOH_LIMIT = 2

    def __init__(
        self,
        selected_nics: List[Dict],
        listen_host: str = "127.0.0.1",
        ethernet_port: int = PORT_ETHERNET,
        wifi_port: int = PORT_WIFI,
        aggregation_port: int = PORT_AGGREGATION,
        use_weighted: bool = False,
        bandwidth_limits: Optional[Dict[str, int]] = None,
        parent=None,
    ):
        super().__init__(parent)
        self._selected_nics = [dict(nic) for nic in selected_nics]
        ProxyWorker._resolve_if_indices(self)
        self._listen_host = listen_host
        self._ports = {
            "nic_ethernet": ethernet_port,
            "nic_wifi": wifi_port,
            "aggregation": aggregation_port,
        }

        wired, wifi = classify_nics(self._selected_nics)
        self._wired = wired or self._selected_nics
        self._wifi = wifi or self._selected_nics

        bal_cls = WeightedBalancer if (use_weighted and bandwidth_limits) else RoundRobinBalancer
        bal_kwargs = {"bandwidth_limits": bandwidth_limits} if bal_cls is WeightedBalancer else {}
        self.bal_ethernet = bal_cls(self._wired, **bal_kwargs)
        self.bal_wifi = bal_cls(self._wifi, **bal_kwargs)
        self.bal_aggregation = bal_cls(self._selected_nics, **bal_kwargs)
        self.balancer = _MergedBalancerView(
            [self.bal_ethernet, self.bal_wifi, self.bal_aggregation]
        )

        self._loop: Optional[asyncio.AbstractEventLoop] = None
        self._stop_event: Optional[asyncio.Event] = None
        self._servers: List = []
        self._client_tasks: "set[asyncio.Task]" = set()
        self._client_writers = set()
        self._upstream_sockets = set()
        self._udp_transports = set()
        self._monitor_task: Optional[asyncio.Task] = None
        self._stop_requested = False
        self._configured_dns_servers: Tuple[str, ...] = self.DNS_SERVERS
        self._doh_endpoints: Tuple[Tuple[str, str, str], ...] = self.DEFAULT_DOH_ENDPOINTS
        self._dns_cache: Dict[Tuple[int, str], Tuple[float, str]] = {}
        self._dns_inflight: Dict[Tuple[int, str], asyncio.Task] = {}
        self._last_connectivity_emit = 0.0

    def listen_ports(self) -> Dict[str, int]:
        """返回三个通道当前实际监听端口的快照。"""
        return dict(self._ports)

    def selected_nics_snapshot(self) -> List[Dict]:
        """返回已解析权威接口索引的网卡快照，供 TUN 健康探测复用。"""
        return [dict(nic) for nic in self._selected_nics]

    def _emit_connectivity(self, detail: str):
        """节流上报真实上游响应，作为 TUN 双向数据链路已打通的证据。"""
        now = time.monotonic()
        if now - self._last_connectivity_emit < 0.5:
            return
        self._last_connectivity_emit = now
        self.connectivity_signal.emit(detail)

    async def _relay_from_sock_with_health(self, sock, writer, loop, detail: str):
        first_response = True
        try:
            while True:
                data = await loop.sock_recv(sock, 65536)
                if not data:
                    break
                if first_response:
                    first_response = False
                    self._emit_connectivity(detail)
                writer.write(data)
                await writer.drain()
        except Exception:
            pass
        finally:
            try:
                sock.close()
            except Exception:
                pass

    # 复用 ProxyWorker 的静态/实例方法，避免重复实现
    _create_bound_upstream_socket = staticmethod(ProxyWorker._create_bound_upstream_socket)
    _relay_to_sock = staticmethod(ProxyWorker._relay_to_sock)
    _relay_from_sock = staticmethod(ProxyWorker._relay_from_sock)
    _abort_writer = staticmethod(ProxyWorker._abort_writer)
    _close_socket = staticmethod(ProxyWorker._close_socket)

    def set_dns_servers(self, servers: List[str]):
        """设置传统 53 端口 DNS 兜底服务器。"""
        normalized: List[str] = []
        for server in servers or []:
            text = str(server).strip()
            if self._is_usable_ipv4(text) and text not in normalized:
                normalized.append(text)
        for fallback in self.DNS_SERVERS:
            if fallback not in normalized:
                normalized.append(fallback)
        self._configured_dns_servers = tuple(normalized)

    def set_doh_provider(self, provider: str):
        key = str(provider or "auto").strip().lower()
        self._doh_endpoints = self.DOH_PRESETS.get(key, self.DEFAULT_DOH_ENDPOINTS)

    def run(self):
        self._loop = asyncio.new_event_loop()
        self._loop.set_exception_handler(_quiet_asyncio_exception_handler)
        asyncio.set_event_loop(self._loop)
        try:
            self._loop.run_until_complete(self._serve())
        except Exception as e:
            self.error_signal.emit(f"多端口出站池异常退出: {type(e).__name__}: {e}")
        finally:
            try:
                _shutdown_event_loop(self._loop)
            finally:
                self._loop = None
            self.stopped.emit("多端口出站池已停止")

    async def _serve(self):
        self._stop_event = asyncio.Event()
        if self._stop_requested:
            return

        try:
            # 三个隔离监听：每个绑定不同 balancer。逐个加入列表，确保后续
            # 通道失败时，前面已经创建的 server 也能被 _teardown() 关闭。
            self._servers = []
            for channel, balancer in (
                ("nic_ethernet", self.bal_ethernet),
                ("nic_wifi", self.bal_wifi),
                ("aggregation", self.bal_aggregation),
            ):
                server = await self._start_channel_server(channel, balancer)
                self._servers.append(server)
        except Exception as e:
            await self._teardown()
            self.error_signal.emit(f"多端口出站池监听失败: {e}")
            return

        dns_ready, dns_details = await self._preflight_selected_nics_dns()
        for detail in dns_details:
            self.log_signal.emit(f"[出站池][DNS预检] {detail}")
        if self._stop_requested:
            await self._teardown()
            return
        if not dns_ready:
            await self._teardown()
            self.error_signal.emit(
                "多端口出站池 DNS 预检失败：至少一张已选网卡无法完成域名解析，"
                "已取消 TUN 接管"
            )
            return

        self.log_signal.emit(
            f"[出站池] 三通道已就绪 | 有线 {self._listen_host}:{self._ports['nic_ethernet']} "
            f"| 无线 {self._listen_host}:{self._ports['nic_wifi']} "
            f"| 聚合 {self._listen_host}:{self._ports['aggregation']}"
        )
        dns_plan = "; ".join(
            f"{nic.get('name', nic.get('ip', 'unknown'))}=>{','.join(self._dns_servers_for_nic(nic))}"
            for nic in self._selected_nics
        )
        doh_plan = ",".join(f"{host}@{ip}" for ip, host, _path in self._doh_endpoints)
        self.log_signal.emit(f"[出站池][DNS] DoH优先 {doh_plan} | 53兜底 {dns_plan}")
        self.started_ok.emit(
            f"ethernet={self._ports['nic_ethernet']};wifi={self._ports['nic_wifi']};"
            f"aggregation={self._ports['aggregation']}"
        )

        self._monitor_task = asyncio.create_task(self._traffic_monitor())
        try:
            await self._stop_event.wait()
        finally:
            await self._teardown()
            self.log_signal.emit("[出站池] 收到停止指令，已关闭三通道并清理在途连接")

    @staticmethod
    def _can_fallback_bind_error(error: OSError) -> bool:
        """识别端口被占用或被 Windows/HNS 排除导致的可恢复监听错误。"""
        codes = {
            getattr(error, "errno", None),
            getattr(error, "winerror", None),
        }
        # 13/EACCES、98/10048/EADDRINUSE、10013/WSAEACCES。
        return bool(codes & {13, 98, 10013, 10048})

    async def _start_channel_server(
        self,
        channel: str,
        balancer: "RoundRobinBalancer",
    ):
        preferred_port = int(self._ports[channel])
        handler = self._make_handler(balancer, channel)
        try:
            server = await asyncio.start_server(
                handler, self._listen_host, preferred_port,
            )
        except OSError as error:
            if preferred_port <= 0 or not self._can_fallback_bind_error(error):
                raise
            # port=0 让 Windows 原子选择一个当前可绑定且不在排除范围内的端口，
            # 避免“先探测、后绑定”之间被其他进程抢占的竞态。
            server = await asyncio.start_server(handler, self._listen_host, 0)
            actual_port = int(server.sockets[0].getsockname()[1])
            self._ports[channel] = actual_port
            self.log_signal.emit(
                f"[出站池] 本地端口 {preferred_port} 不可用（{error}），"
                f"{channel} 已自动改用 {actual_port}"
            )
            return server

        actual_port = int(server.sockets[0].getsockname()[1])
        self._ports[channel] = actual_port
        return server

    def _make_handler(self, balancer: "RoundRobinBalancer", channel: str):
        async def handler(reader, writer):
            await self._handle_socks(reader, writer, balancer, channel)
        return handler

    @staticmethod
    def _create_bound_udp_socket(nic) -> socket.socket:
        """创建出站 UDP socket，并用物理接口索引锁定出口。"""
        if_index = int(nic.get("if_index", nic.get("index", 0)) or 0)
        sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        try:
            sock.setblocking(False)
            sock.setsockopt(socket.IPPROTO_IP, IP_UNICAST_IF, struct.pack("!I", if_index))
            local_ip = nic.get("ip")
            if local_ip:
                try:
                    sock.bind((local_ip, 0))
                except OSError:
                    pass
        except Exception:
            sock.close()
            raise
        return sock

    @staticmethod
    def _is_usable_ipv4(ip: str) -> bool:
        parts = str(ip).strip().split(".")
        if len(parts) != 4 or not all(part.isdigit() for part in parts):
            return False
        nums = [int(part) for part in parts]
        if any(num < 0 or num > 255 for num in nums):
            return False
        if nums[0] in (0, 127, 224, 255):
            return False
        if nums[0] == 169 and nums[1] == 254:
            return False
        if nums[0] == 198 and nums[1] in (18, 19):
            return False
        return True

    def _dns_servers_for_nic(self, nic: Dict) -> Tuple[str, ...]:
        servers: List[str] = []
        for source in (nic.get("dns_servers", []), self._configured_dns_servers):
            values = source if isinstance(source, (list, tuple)) else [source]
            for server in values:
                text = str(server).strip()
                if self._is_usable_ipv4(text) and text not in servers:
                    servers.append(text)
        for fallback in self.DNS_SERVERS:
            if fallback not in servers:
                servers.append(fallback)
        return tuple(servers)

    @staticmethod
    def _pack_socks5_udp_header(host: str, port: int, payload: bytes) -> bytes:
        try:
            addr = socket.inet_aton(host)
            return b"\x00\x00\x00\x01" + addr + struct.pack("!H", port) + payload
        except OSError:
            encoded = host.encode("idna")[:255]
            return b"\x00\x00\x00\x03" + bytes([len(encoded)]) + encoded + struct.pack("!H", port) + payload

    @staticmethod
    def _build_dns_query(domain: str, query_id: int) -> bytes:
        labels = domain.rstrip(".").encode("idna").split(b".")
        question = b"".join(bytes([len(label)]) + label for label in labels) + b"\x00"
        return (
            struct.pack("!HHHHHH", query_id, 0x0100, 1, 0, 0, 0)
            + question
            + struct.pack("!HH", 1, 1)
        )

    @staticmethod
    def _read_dns_name(packet: bytes, offset: int) -> int:
        jumped = False
        start = offset
        limit = 0
        while offset < len(packet):
            length = packet[offset]
            if length == 0:
                offset += 1
                return offset if not jumped else start + 2
            if length & 0xC0 == 0xC0:
                if offset + 1 >= len(packet):
                    raise ValueError("bad compressed dns name")
                if not jumped:
                    start = offset
                pointer = ((length & 0x3F) << 8) | packet[offset + 1]
                offset = pointer
                jumped = True
                limit += 1
                if limit > 16:
                    raise ValueError("dns compression loop")
                continue
            offset += 1 + length
        raise ValueError("bad dns name")

    @classmethod
    def _parse_dns_a_response(cls, packet: bytes, query_id: int) -> str:
        if len(packet) < 12:
            raise ValueError("short dns response")
        resp_id, flags, qdcount, ancount, _nscount, _arcount = struct.unpack("!HHHHHH", packet[:12])
        if resp_id != query_id or (flags & 0x8000) == 0:
            raise ValueError("mismatched dns response")
        rcode = flags & 0x000F
        if rcode != 0:
            raise ValueError(f"dns rcode={rcode}")

        offset = 12
        for _ in range(qdcount):
            offset = cls._read_dns_name(packet, offset)
            offset += 4

        for _ in range(ancount):
            offset = cls._read_dns_name(packet, offset)
            if offset + 10 > len(packet):
                raise ValueError("truncated dns answer")
            rtype, rclass, _ttl, rdlength = struct.unpack("!HHIH", packet[offset:offset + 10])
            offset += 10
            if offset + rdlength > len(packet):
                raise ValueError("truncated dns rdata")
            rdata = packet[offset:offset + rdlength]
            offset += rdlength
            if rtype == 1 and rclass == 1 and rdlength == 4:
                ip = socket.inet_ntoa(rdata)
                if not ip.startswith("198.18.") and not ip.startswith("198.19."):
                    return ip
        raise ValueError("no A record")

    async def _query_dns_udp(self, domain: str, nic: Dict, loop, dns_server: str) -> str:
        query_id = random.randint(0, 0xFFFF)
        packet = self._build_dns_query(domain, query_id)
        sock = None
        try:
            sock = self._create_bound_udp_socket(nic)
            self._upstream_sockets.add(sock)
            await loop.sock_sendto(sock, packet, (dns_server, 53))
            data, _remote = await asyncio.wait_for(
                loop.sock_recvfrom(sock, 1232),
                timeout=self.DNS53_TIMEOUT,
            )
            return self._parse_dns_a_response(data, query_id)
        finally:
            if sock is not None:
                self._close_socket(sock)
                self._upstream_sockets.discard(sock)

    async def _sock_recv_exact(self, sock: socket.socket, loop, size: int, timeout: float) -> bytes:
        chunks = []
        remaining = size
        deadline = time.monotonic() + timeout
        while remaining > 0:
            chunk_timeout = max(0.1, deadline - time.monotonic())
            chunk = await asyncio.wait_for(loop.sock_recv(sock, remaining), timeout=chunk_timeout)
            if not chunk:
                raise ConnectionError("dns tcp closed")
            chunks.append(chunk)
            remaining -= len(chunk)
        return b"".join(chunks)

    async def _query_dns_tcp(self, domain: str, nic: Dict, loop, dns_server: str) -> str:
        query_id = random.randint(0, 0xFFFF)
        packet = self._build_dns_query(domain, query_id)
        sock = None
        try:
            sock = self._create_bound_upstream_socket(nic)
            self._upstream_sockets.add(sock)
            await asyncio.wait_for(loop.sock_connect(sock, (dns_server, 53)), timeout=self.DNS53_TIMEOUT)
            await asyncio.wait_for(
                loop.sock_sendall(sock, struct.pack("!H", len(packet)) + packet),
                timeout=self.DNS53_TIMEOUT,
            )
            header = await self._sock_recv_exact(sock, loop, 2, self.DNS53_TIMEOUT)
            response_len = struct.unpack("!H", header)[0]
            if response_len <= 0 or response_len > 4096:
                raise ValueError(f"bad dns tcp length={response_len}")
            data = await self._sock_recv_exact(sock, loop, response_len, self.DNS53_TIMEOUT)
            return self._parse_dns_a_response(data, query_id)
        finally:
            if sock is not None:
                self._close_socket(sock)
                self._upstream_sockets.discard(sock)

    def _query_dns_doh_sync(
        self,
        domain: str,
        nic: Dict,
        endpoint_ip: str,
        host: str,
        path: str,
        connect_timeout: float = 4.0,
        io_timeout: float = 8.0,
    ) -> str:
        query_id = random.randint(0, 0xFFFF)
        packet = self._build_dns_query(domain, query_id)
        sock = None
        ssl_sock = None
        try:
            sock = self._create_bound_upstream_socket(nic)
            sock.settimeout(connect_timeout)
            sock.connect((endpoint_ip, 443))
            context = ssl.create_default_context()
            ssl_sock = context.wrap_socket(sock, server_hostname=host)
            ssl_sock.settimeout(io_timeout)
            sock = None
            request = (
                f"POST {path} HTTP/1.1\r\n"
                f"Host: {host}\r\n"
                "Accept: application/dns-message\r\n"
                "Content-Type: application/dns-message\r\n"
                f"Content-Length: {len(packet)}\r\n"
                "Connection: close\r\n"
                "\r\n"
            ).encode("ascii") + packet
            ssl_sock.sendall(request)

            response = bytearray()
            while True:
                try:
                    chunk = ssl_sock.recv(8192)
                except socket.timeout:
                    break
                if not chunk:
                    break
                response.extend(chunk)
                if b"\r\n\r\n" in response:
                    header, body = bytes(response).split(b"\r\n\r\n", 1)
                    header_text = header.decode("iso-8859-1", errors="replace")
                    if "Transfer-Encoding: chunked" not in header_text:
                        content_length = None
                        for line in header_text.split("\r\n"):
                            if line.lower().startswith("content-length:"):
                                try:
                                    content_length = int(line.split(":", 1)[1].strip())
                                except ValueError:
                                    content_length = None
                                break
                        if content_length is not None and len(body) >= content_length:
                            break

            raw = bytes(response)
            if b"\r\n\r\n" not in raw:
                raise ValueError("bad doh response")
            header, body = raw.split(b"\r\n\r\n", 1)
            header_text = header.decode("iso-8859-1", errors="replace")
            status_line = header_text.split("\r\n", 1)[0]
            if " 200 " not in status_line:
                raise ValueError(status_line)
            if "Transfer-Encoding: chunked" in header_text:
                body = self._decode_http_chunked(body)
            return self._parse_dns_a_response(body, query_id)
        finally:
            if ssl_sock is not None:
                self._close_socket(ssl_sock)
            if sock is not None:
                self._close_socket(sock)

    async def _query_dns_doh(
        self,
        domain: str,
        nic: Dict,
        loop,
        endpoint_ip: str,
        host: str,
        path: str,
    ) -> str:
        return await loop.run_in_executor(
            None,
            self._query_dns_doh_sync,
            domain,
            nic,
            endpoint_ip,
            host,
            path,
        )

    @staticmethod
    def _decode_http_chunked(body: bytes) -> bytes:
        decoded = bytearray()
        pos = 0
        while True:
            line_end = body.find(b"\r\n", pos)
            if line_end < 0:
                raise ValueError("bad chunked doh response")
            size_text = body[pos:line_end].split(b";", 1)[0].strip()
            size = int(size_text, 16)
            pos = line_end + 2
            if size == 0:
                return bytes(decoded)
            if pos + size > len(body):
                raise ValueError("truncated chunked doh response")
            decoded.extend(body[pos:pos + size])
            pos += size + 2

    async def _resolve_domain_uncached(self, domain: str, nic: Dict, loop) -> str:
        errors: List[str] = []
        for endpoint_ip, host, path in self._doh_endpoints:
            try:
                return await self._query_dns_doh(domain, nic, loop, endpoint_ip, host, path)
            except Exception as e:
                errors.append(f"doh/{host}@{endpoint_ip} {type(e).__name__}: {e!r}")

        for dns_server in self._dns_servers_for_nic(nic):
            for method, resolver in (("udp", self._query_dns_udp), ("tcp", self._query_dns_tcp)):
                try:
                    return await resolver(domain, nic, loop, dns_server)
                except Exception as e:
                    errors.append(f"{method}/{dns_server} {type(e).__name__}: {e!r}")

        try:
            infos = await asyncio.wait_for(
                loop.getaddrinfo(domain, None, family=socket.AF_INET, type=socket.SOCK_STREAM),
                timeout=2.0,
            )
            for info in infos:
                ip = info[4][0]
                if self._is_usable_ipv4(ip):
                    return ip
            errors.append("system resolver returned no usable A record")
        except Exception as e:
            errors.append(f"system {type(e).__name__}: {e!r}")

        raise RuntimeError("; ".join(errors[-8:]) or "dns resolve failed")

    async def _resolve_domain_via_nic(self, domain: str, nic: Dict, loop) -> str:
        """用绑定到目标物理网卡的 DNS 查询解析域名，避开 TUN/FakeIP 污染。"""
        cache_key = (int(nic.get("if_index", nic.get("index", 0)) or 0), domain.rstrip(".").lower())
        now = time.monotonic()
        cached = self._dns_cache.get(cache_key)
        if cached and cached[0] > now:
            return cached[1]

        task = self._dns_inflight.get(cache_key)
        if task is None or task.done():
            task = asyncio.create_task(self._resolve_domain_uncached(domain, nic, loop))
            self._dns_inflight[cache_key] = task
        try:
            ip = await task
            self._dns_cache[cache_key] = (time.monotonic() + self.DNS_CACHE_TTL, ip)
            return ip
        finally:
            if self._dns_inflight.get(cache_key) is task and task.done():
                self._dns_inflight.pop(cache_key, None)

    async def _preflight_dns_for_nic(self, nic: Dict, loop) -> Tuple[bool, str]:
        """在 TUN 接管前快速验证指定物理网卡的 DNS 可用性。"""
        name = str(nic.get("name") or nic.get("ip") or "unknown")
        errors: List[str] = []
        for endpoint_ip, host, path in self._doh_endpoints[:self.DNS_PREFLIGHT_DOH_LIMIT]:
            if self._stop_requested:
                return False, f"{name}: cancelled"
            try:
                ip = await asyncio.wait_for(
                    loop.run_in_executor(
                        None,
                        self._query_dns_doh_sync,
                        self.DNS_PREFLIGHT_DOMAIN,
                        nic,
                        endpoint_ip,
                        host,
                        path,
                        2.0,
                        3.0,
                    ),
                    timeout=4.0,
                )
                return True, f"{name}: DoH {host} -> {ip}"
            except Exception as e:
                errors.append(f"DoH {host}: {type(e).__name__}")

        # DoH 暂不可用时仅用传统 DNS 判断该网卡是否仍具备基本解析能力；
        # 正式连接阶段依旧保持 DoH 优先和多节点回退。
        for dns_server in self._dns_servers_for_nic(nic)[:2]:
            if self._stop_requested:
                return False, f"{name}: cancelled"
            try:
                ip = await self._query_dns_udp(
                    self.DNS_PREFLIGHT_DOMAIN, nic, loop, dns_server
                )
                return True, f"{name}: DNS {dns_server} -> {ip} (DoH preflight unavailable)"
            except Exception as e:
                errors.append(f"DNS {dns_server}: {type(e).__name__}")
        return False, f"{name}: " + "; ".join(errors[-4:])

    async def _preflight_selected_nics_dns(self) -> Tuple[bool, List[str]]:
        """并行预热全部选中网卡；任一卡完全无法解析时拒绝 TUN 接管。"""
        loop = asyncio.get_running_loop()
        results = await asyncio.gather(*(
            self._preflight_dns_for_nic(nic, loop) for nic in self._selected_nics
        ))
        details = [detail for _ok, detail in results]
        return all(ok for ok, _detail in results), details

    async def _start_udp_associate(self, reader, writer, balancer, channel):
        """为一个 SOCKS5 UDP ASSOCIATE TCP 控制连接启动本地 UDP relay。"""
        loop = asyncio.get_running_loop()
        transport, _protocol = await loop.create_datagram_endpoint(
            lambda: _SocksUdpRelayProtocol(self, balancer, channel),
            local_addr=(self._listen_host, 0),
        )
        host, port = transport.get_extra_info("sockname")[:2]
        writer.write(b"\x05\x00\x00\x01" + socket.inet_aton(host) + struct.pack("!H", port))
        await writer.drain()
        try:
            while await reader.read(1024):
                pass
        except Exception:
            pass
        finally:
            transport.close()

    async def _handle_udp_packet(self, data: bytes, client_addr, balancer, channel, transport):
        if len(data) < 10 or data[2] != 0:
            return
        atyp = data[3]
        pos = 4
        try:
            if atyp == 1:
                dst_host = socket.inet_ntoa(data[pos:pos + 4])
                pos += 4
            elif atyp == 3:
                size = data[pos]
                pos += 1
                dst_host = data[pos:pos + size].decode("idna")
                pos += size
            else:
                return
            dst_port = struct.unpack("!H", data[pos:pos + 2])[0]
            payload = data[pos + 2:]
        except Exception:
            return
        if not payload:
            return

        loop = asyncio.get_running_loop()
        nic = balancer.get_next_nic()
        balancer.on_connect(nic["name"])
        sock = None
        try:
            sock = self._create_bound_udp_socket(nic)
            self._upstream_sockets.add(sock)
            await loop.sock_sendto(sock, payload, (dst_host, dst_port))
            try:
                response, remote = await asyncio.wait_for(loop.sock_recvfrom(sock, 65535), timeout=8.0)
            except asyncio.TimeoutError:
                return
            if response and transport is not None:
                packet = self._pack_socks5_udp_header(remote[0], remote[1], response)
                transport.sendto(packet, client_addr)
        except Exception as e:
            self.log_signal.emit(f"[出站池-{channel}][UDP异常] {type(e).__name__}: {e}")
        finally:
            if sock is not None:
                self._close_socket(sock)
                self._upstream_sockets.discard(sock)
            balancer.on_disconnect(nic["name"])

    async def _handle_socks(self, reader, writer, balancer, channel):
        task = asyncio.current_task()
        if task is not None:
            self._client_tasks.add(task)
        self._client_writers.add(writer)

        nic = None
        upstream_sock = None
        relay_tasks = []
        try:
            # SOCKS5 握手
            version, nmethods = await reader.readexactly(2)
            await reader.readexactly(nmethods)
            writer.write(b"\x05\x00")
            await writer.drain()

            version, cmd, rsv, atyp = await reader.readexactly(4)
            if cmd not in (1, 3):
                writer.write(b"\x05\x07\x00\x01\x00\x00\x00\x00\x00\x00")
                await writer.drain()
                writer.close()
                return

            loop = asyncio.get_running_loop()
            dst_domain = None
            if atyp == 1:
                dst_addr = socket.inet_ntoa(await reader.readexactly(4))
            elif atyp == 3:
                domain_len = ord(await reader.readexactly(1))
                dst_domain = (await reader.readexactly(domain_len)).decode()
                dst_addr = ""
            else:
                writer.close()
                return

            dst_port = struct.unpack("!H", await reader.readexactly(2))[0]
            target_display = dst_domain if dst_domain else dst_addr

            if cmd == 3:
                await self._start_udp_associate(reader, writer, balancer, channel)
                return

            # 域名解析必须先选定一张网卡，避免 FakeIP/系统 DNS 让解析走错出口；
            # TCP 建连本身随后仍可在未转发业务数据前切换一次备用网卡。
            nic = balancer.get_next_nic_for_domain(dst_domain or dst_addr)
            if nic is None:
                writer.close()
                return
            preferred_nic_name = nic["name"]
            if dst_domain:
                try:
                    dst_addr = await self._resolve_domain_via_nic(dst_domain, nic, loop)
                except Exception as e:
                    self.log_signal.emit(
                        f"[出站池-{channel}][DNS失败] {nic['name']} -> {dst_domain}: {type(e).__name__}: {e}"
                    )
                    writer.close()
                    return
            self.log_signal.emit(
                f"[出站池-{channel}][TCP] {nic['name']} -> {target_display}:{dst_port} ({dst_addr})"
            )

            def on_failure(failed_nic, error, stage):
                self.log_signal.emit(
                    f"[出站池-{channel}][{'连通' if stage == 'connect' else '绑定'}失败] "
                    f"{failed_nic['name']} -> {target_display}:{dst_port} ({dst_addr}) | "
                    f"{type(error).__name__}: {error}"
                )
                if stage == "connect":
                    get_tracker().on_connect_failure(
                        nic_name=failed_nic["name"],
                        domain=dst_domain or dst_addr,
                        port=dst_port,
                        failed_nic=failed_nic,
                        all_nics=self._selected_nics,
                        loop=self._loop,
                    )

            def on_retry(failed_nic, _error):
                self.log_signal.emit(
                    f"[出站池-{channel}][故障切换] [{failed_nic['name']}] 建连失败，"
                    "正在尝试另一张网卡"
                )

            try:
                upstream_sock, nic = await _connect_with_one_failover(
                    loop=loop,
                    balancer=balancer,
                    schedule_target=dst_domain or dst_addr,
                    dst_addr=dst_addr,
                    dst_port=dst_port,
                    create_socket=self._create_bound_upstream_socket,
                    upstream_sockets=self._upstream_sockets,
                    timeout=self.TCP_CONNECT_TIMEOUT,
                    on_failure=on_failure,
                    on_retry=on_retry,
                    first_nic=nic,
                )
            except Exception:
                writer.write(b"\x05\x05\x00\x01\x00\x00\x00\x00\x00\x00")
                await writer.drain()
                writer.close()
                return
            if nic["name"] != preferred_nic_name:
                self.log_signal.emit(
                    f"[出站池-{channel}][故障切换成功] "
                    f"{preferred_nic_name} -> {nic['name']} | "
                    f"{target_display}:{dst_port}"
                )

            writer.write(b"\x05\x00\x00\x01\x00\x00\x00\x00\x00\x00")
            await writer.drain()

            relay_tasks = [
                asyncio.create_task(self._relay_to_sock(reader, upstream_sock, loop)),
                asyncio.create_task(self._relay_from_sock_with_health(
                    upstream_sock,
                    writer,
                    loop,
                    f"{nic['name']} -> {target_display}:{dst_port}",
                )),
            ]
            self._client_tasks.update(relay_tasks)
            _, pending = await asyncio.wait(relay_tasks, return_when=asyncio.FIRST_COMPLETED)
            for t in pending:
                t.cancel()
        except (asyncio.IncompleteReadError, ConnectionResetError, asyncio.CancelledError):
            pass
        except Exception as e:
            self.log_signal.emit(f"[出站池-{channel}][连接异常] {type(e).__name__}: {e}")
        finally:
            try:
                writer.close()
            except Exception:
                pass
            if upstream_sock is not None:
                self._close_socket(upstream_sock)
                self._upstream_sockets.discard(upstream_sock)
            self._client_writers.discard(writer)
            if nic is not None:
                balancer.on_disconnect(nic["name"])
            for rt in relay_tasks:
                self._client_tasks.discard(rt)
            if task is not None:
                self._client_tasks.discard(task)

    async def _teardown(self):
        for server in self._servers:
            try:
                server.close()
            except Exception:
                pass
        for writer in list(self._client_writers):
            self._abort_writer(writer)
        for transport in list(self._udp_transports):
            try:
                transport.close()
            except Exception:
                pass
        for sock in list(self._upstream_sockets):
            self._close_socket(sock)

        if self._monitor_task is not None and not self._monitor_task.done():
            self._monitor_task.cancel()
        for task in list(self._client_tasks):
            if not task.done():
                task.cancel()
        if self._client_tasks:
            await asyncio.wait(list(self._client_tasks), timeout=self.STOP_TASK_TIMEOUT)

        for server in self._servers:
            try:
                await asyncio.wait_for(server.wait_closed(), timeout=self.SERVER_CLOSE_TIMEOUT)
            except Exception:
                pass

        self._servers = []
        self._client_tasks.clear()
        self._client_writers.clear()
        self._udp_transports.clear()
        self._upstream_sockets.clear()
        self._monitor_task = None

    def stop(self):
        self._stop_requested = True
        loop = self._loop
        event = self._stop_event
        if loop is not None and event is not None:
            loop.call_soon_threadsafe(event.set)

    # 复用 ProxyWorker 的遥测实现（聚合三通道整体网卡吞吐）
    _traffic_monitor = ProxyWorker._traffic_monitor
