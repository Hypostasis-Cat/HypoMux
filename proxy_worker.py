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
import socket
import struct
import threading
from ctypes import wintypes
from typing import List, Dict, Optional, Tuple
from urllib.parse import urlsplit

import psutil
from PySide6.QtCore import QThread, Signal

from utils.network_utils import get_adapter_if_indices


# IPv4 下的 IP_UNICAST_IF：强制指定物理网卡出口，绕过 Windows 默认路由判定。
IP_UNICAST_IF = 31
AF_INET = 2
TCP_TABLE_OWNER_PID_ALL = 5
ERROR_INSUFFICIENT_BUFFER = 122
NO_ERROR = 0


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

    def get_next_nic(self) -> Dict:
        with self._lock:
            nic = self.nics[self._current]
            self._current = (self._current + 1) % len(self.nics)
            return nic

    def on_connect(self, nic_name: str):
        with self._lock:
            self._active[nic_name] = self._active.get(nic_name, 0) + 1

    def on_disconnect(self, nic_name: str):
        with self._lock:
            if self._active.get(nic_name, 0) > 0:
                self._active[nic_name] -= 1

    def active_connections(self) -> Dict[str, int]:
        with self._lock:
            return dict(self._active)


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

    def __init__(
        self,
        selected_nics: List[Dict],
        listen_host: str = "127.0.0.1",
        listen_port: int = 10800,
        http_port: Optional[int] = None,
        parent=None,
    ):
        super().__init__(parent)
        self._selected_nics = [dict(nic) for nic in selected_nics]
        # 【WinError 10049 根治】用 GetAdaptersAddresses 把每张网卡的出口 IP 反查为
        # 权威 IfIndex（接口索引），存到 nic['if_index']。出站 socket 将用
        # IP_UNICAST_IF 死锁在该索引上，而非脆弱地 bind 到本地 IP。
        self._resolve_if_indices()
        self._listen_host = listen_host
        self._listen_port = listen_port
        self._http_port = http_port if http_port is not None else listen_port + 1

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
        asyncio.set_event_loop(self._loop)
        try:
            self._loop.run_until_complete(self._serve())
        except Exception as e:
            self.error_signal.emit(f"代理内核异常退出: {type(e).__name__}: {e}")
        finally:
            try:
                self._loop.close()
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
            else:
                # 3. 【L4 调度】在用户选中的网卡里轮询申请一张
                nic = self.balancer.get_next_nic()
                self.balancer.on_connect(nic["name"])
                self.log_signal.emit(
                    f"[调度分配] 新连接 -> [{nic['name']}] | 目标: {target_display}:{dst_port}"
                )

                # 4. 【L3 物理层绑定 -- 接口索引强绑定，根治 WinError 10049】
                #    先 IP_UNICAST_IF 锁死出口网卡（if_index），再 bind 源地址。
                try:
                    upstream_sock = self._create_bound_upstream_socket(nic)
                    self._upstream_sockets.add(upstream_sock)
                except Exception as e:
                    self.log_signal.emit(
                        f"[绑定崩溃] 网卡: {nic['name']} 接口强绑定失败 "
                        f"(IfIndex={nic.get('if_index', nic.get('index'))}): {e}。"
                        f"请检查该网卡是否已被禁用或拔出！"
                    )
                    writer.close()
                    if upstream_sock is not None:
                        upstream_sock.close()
                        self._upstream_sockets.discard(upstream_sock)
                        upstream_sock = None
                    return

            # 5. 连接目标
            try:
                await loop.sock_connect(upstream_sock, (dst_addr, dst_port))
            except Exception as e:
                if nic is None:
                    pass
                else:
                    self.log_signal.emit(
                        f"[连通失败] 网卡: {nic['name']} 无法连接目标 {target_display}: {e}"
                    )
                writer.write(b"\x05\x05\x00\x01\x00\x00\x00\x00\x00\x00")
                await writer.drain()
                writer.close()
                upstream_sock.close()
                self._upstream_sockets.discard(upstream_sock)
                upstream_sock = None
                return

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
        else:
            nic = self.balancer.get_next_nic()
            self.balancer.on_connect(nic["name"])
            # 接口索引强绑定（IP_UNICAST_IF 先于 bind），根治同网段 WinError 10049
            upstream_sock = self._create_bound_upstream_socket(nic)
        self._upstream_sockets.add(upstream_sock)

        try:
            await loop.sock_connect(upstream_sock, (dst_addr, dst_port))
        except Exception:
            if nic is not None:
                self.balancer.on_disconnect(nic["name"])
            upstream_sock.close()
            self._upstream_sockets.discard(upstream_sock)
            raise

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
