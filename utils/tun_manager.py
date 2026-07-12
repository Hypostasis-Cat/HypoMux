"""
HypoMux Wintun 内核侧车守护器 (TunManager) - 第三阶段下半场 · 任务3

把 bin/sing-box.exe 作为后台 Kernel Sidecar 异步拉起：
- asyncio.create_subprocess_exec 启动 `sing-box.exe run -c <config.json>`
- creationflags=CREATE_NO_WINDOW 彻底隐藏黑窗口
- 完善生命周期：stop()/closeEvent/强关/关机均强制 kill 残留进程，
  并清理 Windows 残留路由，防止 TUN 设备遗留导致用户断网挂死。

设计为 QThread：在独立子线程跑自己的 asyncio 事件循环，绝不阻塞 UI。
通过 Qt 信号回吐状态，与现有 ProxyWorker 信号风格保持一致。
"""

from __future__ import annotations

import asyncio
import os
import subprocess
import sys
from typing import Optional

from PySide6.QtCore import QThread, Signal

# CREATE_NO_WINDOW：彻底隐藏 sing-box 控制台黑窗口
_CREATE_NO_WINDOW = getattr(subprocess, "CREATE_NO_WINDOW", 0x08000000)
_TUN_INTERFACE_NAME = "HypoMux-Tun"
_PS_REMOVE_HYPOMUX_TUN_ADAPTER = (
    "$targets = @(Get-PnpDevice -Class Net -ErrorAction SilentlyContinue | "
    "Where-Object { $_.FriendlyName -eq 'HypoMux-Tun' -and $_.InstanceId -like '*WINTUN*' }); "
    "if ($targets.Count -gt 0) { "
    "$targets | Disable-PnpDevice -Confirm:$false -ErrorAction SilentlyContinue; "
    "Start-Sleep -Milliseconds 800; "
    "foreach ($d in $targets) { pnputil /remove-device $d.InstanceId 2>&1 | Out-Null } }"
)
_PS_REMOVE_HYPOMUX_TUN_ROUTES = (
    "Get-NetRoute -AddressFamily IPv4 -DestinationPrefix '0.0.0.0/0' "
    "-ErrorAction SilentlyContinue | "
    "Where-Object { $_.InterfaceAlias -eq 'HypoMux-Tun' } | "
    "Remove-NetRoute -Confirm:$false -ErrorAction SilentlyContinue"
)


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


def _safe_close_stream(stream):
    """尽力关闭 asyncio 子进程管道持有的底层传输句柄。"""
    try:
        transport = getattr(stream, "_transport", None)
        if transport is not None:
            transport.close()
    except Exception:
        pass


def _bin_dir() -> str:
    """返回 bin/ 目录绝对路径（兼容源码运行与 Nuitka 打包）。"""
    is_frozen = getattr(sys, "frozen", False) or ("__compiled__" in globals())
    if is_frozen:
        base = os.path.dirname(os.path.abspath(sys.executable or sys.argv[0]))
    else:
        # utils/tun_manager.py -> 项目根
        base = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
    return os.path.join(base, "bin")


def get_singbox_path() -> Optional[str]:
    """解析 sing-box.exe 绝对路径；找不到返回 None。"""
    candidate = os.path.join(_bin_dir(), "sing-box.exe")
    return candidate if os.path.isfile(candidate) else None


class TunManager(QThread):
    """sing-box TUN 内核侧车守护线程。

    Signals:
        log_signal(str)    -- 守护日志（喂给 UI 控制台）
        started_ok(str)    -- sing-box 成功拉起
        stopped(str)       -- sing-box 已完全停止
        error_signal(str)  -- 启动失败 / 内核异常退出
    """

    log_signal = Signal(str)
    started_ok = Signal(str)
    stopped = Signal(str)
    error_signal = Signal(str)

    KILL_TIMEOUT = 1.0
    STARTUP_STABLE_DELAY = 1.5

    def __init__(self, config_path: str, parent=None):
        super().__init__(parent)
        self._config_path = os.path.abspath(config_path)
        self._loop: Optional[asyncio.AbstractEventLoop] = None
        self._proc: Optional[asyncio.subprocess.Process] = None
        self._stop_event: Optional[asyncio.Event] = None
        self._reader_tasks: list[asyncio.Task] = []
        self._stop_requested = False

    # ---------- QThread 入口 ----------
    def run(self):
        self._loop = asyncio.new_event_loop()
        self._loop.set_exception_handler(_quiet_asyncio_exception_handler)
        asyncio.set_event_loop(self._loop)
        try:
            self._loop.run_until_complete(self._serve())
        except Exception as e:
            self.error_signal.emit(f"TUN 内核守护异常: {type(e).__name__}: {e}")
        finally:
            try:
                _shutdown_event_loop(self._loop)
            finally:
                self._loop = None
            self.stopped.emit("TUN 内核已停止")

    async def _serve(self):
        self._stop_event = asyncio.Event()
        if self._stop_requested:
            return

        exe = get_singbox_path()
        if not exe:
            self.error_signal.emit("未找到 bin/sing-box.exe，无法启动虚拟网卡模式")
            return
        if not os.path.isfile(self._config_path):
            self.error_signal.emit(f"sing-box 配置不存在: {self._config_path}")
            return

        work_dir = os.path.dirname(exe)
        await self._preflight_cleanup_singbox()
        await self._cleanup_tun_adapter()
        try:
            self._proc = await asyncio.create_subprocess_exec(
                exe, "run", "-c", self._config_path,
                cwd=work_dir,
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.PIPE,
                creationflags=_CREATE_NO_WINDOW,
            )
        except Exception as e:
            self.error_signal.emit(f"启动 sing-box.exe 失败: {e}")
            return

        self.log_signal.emit(
            f"[TUN] sing-box 内核进程已拉起 (PID={self._proc.pid})，等待稳定接管"
        )

        # 并发：读 stdout / stderr 日志 + 等待停止信号
        stdout_task = asyncio.create_task(self._pump_stream(self._proc.stdout, "stdout"))
        stderr_task = asyncio.create_task(self._pump_stream(self._proc.stderr, "stderr"))
        self._reader_tasks = [stdout_task, stderr_task]
        stop_task = asyncio.create_task(self._stop_event.wait())
        exit_task = asyncio.create_task(self._proc.wait())
        stable_task = asyncio.create_task(asyncio.sleep(self.STARTUP_STABLE_DELAY))

        done, pending = await asyncio.wait(
            {stop_task, exit_task, stable_task}, return_when=asyncio.FIRST_COMPLETED
        )

        if stable_task in done and exit_task not in done and stop_task not in done:
            self.log_signal.emit(
                f"[TUN] sing-box 内核已稳定运行，虚拟网卡 {_TUN_INTERFACE_NAME} 接管中"
            )
            self.started_ok.emit(f"pid={self._proc.pid}")
            done, pending = await asyncio.wait(
                {stop_task, exit_task}, return_when=asyncio.FIRST_COMPLETED
            )

        # 停止请求路径：先终止进程，让 stdout/stderr 自然 EOF，避免主动取消管道读导致 Proactor 噪声
        if stop_task in done and exit_task not in done:
            await self._terminate_process()
            await self._drain_log_tasks(stdout_task, stderr_task)

        # 若是进程自己先退出（崩溃/被外部杀），先短暂排空管道，再上报错误
        if exit_task in done and stop_task not in done:
            await self._drain_log_tasks(stdout_task, stderr_task)
            rc = exit_task.result()
            if rc != 0:
                self.error_signal.emit(f"sing-box 内核意外退出 (code={rc})")

        # 收尾：强杀进程 + 清理路由
        await self._terminate_process()
        await self._cancel_reader_tasks()
        for t in (stop_task, exit_task, stable_task):
            if not t.done():
                t.cancel()
        self._cleanup_routes()
        self._reader_tasks = []

    async def _cancel_reader_tasks(self):
        """先取消 stdout/stderr 读取任务，再释放 sing-box 进程句柄。"""
        tasks = [task for task in self._reader_tasks if task is not None and not task.done()]
        if not tasks:
            await asyncio.sleep(0.05)
            return
        for task in tasks:
            task.cancel()
        try:
            await asyncio.gather(*tasks, return_exceptions=True)
        except Exception:
            pass
        await asyncio.sleep(0.05)

    async def _pump_stream(self, stream: Optional[asyncio.StreamReader], name: str):
        """把 sing-box stdout/stderr 逐行转发到 UI 控制台。"""
        if stream is None:
            return
        try:
            while True:
                line = await stream.readline()
                if not line:
                    break
                text = line.decode("utf-8", errors="replace").rstrip()
                if text:
                    self.log_signal.emit(f"[sing-box:{name}] {text}")
        except asyncio.CancelledError:
            pass
        except Exception as e:
            self.log_signal.emit(f"[TUN] 读取 sing-box {name} 日志异常: {e}")

    async def _drain_log_tasks(self, *tasks: asyncio.Task):
        """进程异常退出后给日志管道一个短窗口，尽量先把 Fatal/Error 刷到 UI。"""
        pending = [t for t in tasks if not t.done()]
        if not pending:
            return
        try:
            await asyncio.wait(pending, timeout=1.5)
        except Exception:
            pass

    async def _preflight_cleanup_singbox(self):
        """启动前防御性清理上一轮残留的 sing-box.exe。"""
        self.log_signal.emit("[TUN] 正在清理残留 sing-box 内核进程")
        try:
            si = None
            if hasattr(subprocess, "STARTUPINFO"):
                si = subprocess.STARTUPINFO()
                si.dwFlags |= subprocess.STARTF_USESHOWWINDOW
                si.wShowWindow = 0
            proc = await asyncio.create_subprocess_exec(
                "taskkill", "/F", "/IM", "sing-box.exe", "/T",
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.PIPE,
                startupinfo=si,
                creationflags=_CREATE_NO_WINDOW,
            )
            stdout, stderr = await asyncio.wait_for(proc.communicate(), timeout=5)
            for raw in (stdout, stderr):
                if not raw:
                    continue
                text = self._decode_process_output(raw).strip()
                if text:
                    self.log_signal.emit(f"[taskkill] {text}")
        except asyncio.TimeoutError:
            self.log_signal.emit("[TUN] 启动前 taskkill 清理超时，继续尝试拉起内核")
        except FileNotFoundError:
            self.log_signal.emit("[TUN] 未找到 taskkill，跳过启动前残留进程清理")
        except Exception as e:
            self.log_signal.emit(f"[TUN] 启动前清理 sing-box 残留进程异常: {e}")

    async def _cleanup_tun_adapter(self):
        """删除残留的 HypoMux-Tun 虚拟适配器（上次崩溃/强杀遗留的僵尸设备）。

        同时匹配 FriendlyName=HypoMux-Tun 和 InstanceId *WINTUN*，避免影响
        其他软件（例如 Clash、WireGuard、Tailscale）创建的 Wintun 设备。
        pnputil 是 Windows 内置工具始终可用，不依赖 PowerShell Remove-* cmdlet。
        """
        cleanup_command = (
            "Write-Output '检查残留 HypoMux-Tun 设备'; "
            + _PS_REMOVE_HYPOMUX_TUN_ADAPTER
        )
        try:
            si = None
            if hasattr(subprocess, "STARTUPINFO"):
                si = subprocess.STARTUPINFO()
                si.dwFlags |= subprocess.STARTF_USESHOWWINDOW
                si.wShowWindow = 0
            proc = await asyncio.create_subprocess_exec(
                "powershell", "-NoProfile", "-Command", cleanup_command,
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.PIPE,
                startupinfo=si,
                creationflags=_CREATE_NO_WINDOW,
            )
            stdout, stderr = await asyncio.wait_for(proc.communicate(), timeout=12)
            # 始终输出诊断信息（无论 returncode，诊断信息在 stdout 中）
            for raw in (stdout, stderr):
                if not raw:
                    continue
                text = self._decode_process_output(raw).strip()
                if text:
                    self.log_signal.emit(f"[TUN] 虚拟网卡适配器清理: {text}")
            # 等待 Windows 完成异步设备移除
            await asyncio.sleep(0.5)
        except asyncio.TimeoutError:
            self.log_signal.emit("[TUN] 虚拟网卡适配器清理超时，继续尝试拉起内核")
        except FileNotFoundError:
            self.log_signal.emit("[TUN] 未找到 PowerShell，跳过适配器清理")
        except Exception as e:
            self.log_signal.emit(f"[TUN] 虚拟网卡适配器清理异常: {e}")

    @staticmethod
    def _decode_process_output(raw: bytes) -> str:
        """解码 Windows 子进程输出，兼容 UTF-8 与系统 OEM 代码页。"""
        for encoding in ("utf-8", "mbcs", "gbk"):
            try:
                return raw.decode(encoding)
            except Exception:
                continue
        return raw.decode("utf-8", errors="replace")

    async def _terminate_process(self):
        """温和终止后 1 秒内未退出则强杀 sing-box，并释放管道句柄。"""
        proc = self._proc
        if proc is None:
            return
        try:
            if proc.returncode is None:
                try:
                    proc.terminate()
                    await asyncio.wait_for(proc.wait(), timeout=self.KILL_TIMEOUT)
                except (asyncio.TimeoutError, ProcessLookupError, OSError) as e:
                    if not isinstance(e, ProcessLookupError):
                        self.log_signal.emit("[TUN] sing-box 温和终止超时，执行强制 kill")
                    try:
                        if proc.returncode is None:
                            proc.kill()
                        await asyncio.wait_for(proc.wait(), timeout=self.KILL_TIMEOUT)
                    except Exception:
                        self._taskkill_singbox()
                except Exception as e:
                    self.log_signal.emit(f"[TUN] 终止 sing-box 异常: {e}")
                    try:
                        if proc.returncode is None:
                            proc.kill()
                        await asyncio.wait_for(proc.wait(), timeout=self.KILL_TIMEOUT)
                    except Exception:
                        self._taskkill_singbox()
        finally:
            _safe_close_stream(getattr(proc, "stdout", None))
            _safe_close_stream(getattr(proc, "stderr", None))
            _safe_close_stream(getattr(proc, "stdin", None))

    def _taskkill_singbox(self):
        """兜底：用 taskkill 按映像名强杀所有 sing-box.exe。"""
        try:
            si = None
            if hasattr(subprocess, "STARTUPINFO"):
                si = subprocess.STARTUPINFO()
                si.dwFlags |= subprocess.STARTF_USESHOWWINDOW
                si.wShowWindow = 0
            subprocess.run(
                ["taskkill", "/F", "/IM", "sing-box.exe", "/T"],
                capture_output=True, timeout=5, startupinfo=si,
                creationflags=_CREATE_NO_WINDOW,
            )
        except Exception:
            pass

    def _cleanup_routes(self):
        """清理 Windows 残留路由，防止 TUN 设备遗留导致断网挂死。

        sing-box auto_route 退出时通常会自清，这里做防御式兜底：
        仅删除接口别名为 HypoMux-Tun 的默认路由，绝不影响其他 VPN/TUN 的路由。
        """
        try:
            si = None
            if hasattr(subprocess, "STARTUPINFO"):
                si = subprocess.STARTUPINFO()
                si.dwFlags |= subprocess.STARTF_USESHOWWINDOW
                si.wShowWindow = 0
            subprocess.run(
                ["powershell", "-NoProfile", "-Command", _PS_REMOVE_HYPOMUX_TUN_ROUTES],
                capture_output=True, timeout=5, startupinfo=si,
                creationflags=_CREATE_NO_WINDOW,
            )
        except Exception:
            pass

    # ---------- 主线程安全停止 ----------
    def stop(self):
        """从主线程请求停止（不阻塞 UI）。"""
        self._stop_requested = True
        loop = self._loop
        event = self._stop_event
        if loop is not None:
            if event is not None:
                loop.call_soon_threadsafe(event.set)

    def force_kill(self):
        """同步兜底强杀：用于 closeEvent 等必须立即清理的场景。"""
        self._taskkill_singbox()
        self._cleanup_routes()
        self._force_cleanup_tun_adapter()

    def _force_cleanup_tun_adapter(self):
        """同步删除残留 HypoMux-Tun 适配器（force_kill 兜底路径）。

        同时匹配 FriendlyName=HypoMux-Tun 和 InstanceId *WINTUN*，避免误删
        其他应用创建的 Wintun 设备。
        """
        try:
            si = None
            if hasattr(subprocess, "STARTUPINFO"):
                si = subprocess.STARTUPINFO()
                si.dwFlags |= subprocess.STARTF_USESHOWWINDOW
                si.wShowWindow = 0
            subprocess.run(
                [
                    "powershell", "-NoProfile", "-Command",
                    _PS_REMOVE_HYPOMUX_TUN_ADAPTER,
                ],
                capture_output=True, timeout=10,
                startupinfo=si,
                creationflags=_CREATE_NO_WINDOW,
            )
        except Exception:
            pass
