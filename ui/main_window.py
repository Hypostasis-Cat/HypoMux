"""
HypoMux 主窗口 - v3.0 (FluentWindow 侧边导航架构)

第三阶段全盘换装：MainWindow 继承 qfluentwidgets.FluentWindow，
左侧 Windows 11 风格侧边导航 + 四个子页面（首页/路由/体检/设置）。

MainWindow 仅作「后端宿主 + 调度中枢」：
- 持有 ProxyWorker 生命周期、ScanWorker、DiagnosticWorker、配置、系统托盘
- 四个子页面是纯视图，经 Qt 信号上抛意图、经公开方法接收回填

后端引擎（ProxyWorker / config_manager / autostart / diagnostic_runner）零改动，
仅把第一/二阶段的槽函数与数据流重新绑定到新的 Fluent 组件上。

关键特性：所有 Qt 与 qfluentwidgets 导入都延迟到工厂函数内，确保
QApplication 已存在，避免 "Must construct a QApplication before a QWidget"。
"""

import ctypes
import logging
import subprocess
import sys
from logging.handlers import RotatingFileHandler
from pathlib import Path
from typing import List, Dict
import winreg

from utils.network_utils import scan_network_adapters
from utils.config_manager import load_config, save_config
from utils.diagnostic_runner import run_diagnostic, DEFAULT_TARGET_IP
from proxy_worker import ProxyWorker, MultiPortProxyWorker
from utils.tun_manager import TunManager
from utils import singbox_config


DEFAULT_SOCKS_PORT = 10800
DEFAULT_HTTP_PORT = 10801


def _build_app_logger() -> logging.Logger:
    """构建用户目录滚动日志，替代首页可视控制台。"""
    logger = logging.getLogger("hypomux.app")
    if logger.handlers:
        return logger
    logger.setLevel(logging.INFO)
    log_dir = Path.home() / ".hypomux" / "logs"
    log_dir.mkdir(parents=True, exist_ok=True)
    handler = RotatingFileHandler(
        log_dir / "app.log",
        maxBytes=2 * 1024 * 1024,
        backupCount=3,
        encoding="utf-8",
    )
    handler.setFormatter(logging.Formatter("%(asctime)s %(levelname)s %(message)s"))
    logger.addHandler(handler)
    try:
        handler.doRollover()
    except Exception:
        pass
    logger.propagate = False
    return logger


APP_LOGGER = _build_app_logger()


def detect_foreign_tun_default_route() -> str:
    """检测会抢占默认路由的第三方虚拟隧道接口。"""
    pattern = "meta|clash|mihomo|tun|wintun|wireguard|tailscale|vpn|tap"
    command = (
        "Get-NetRoute -AddressFamily IPv4 -DestinationPrefix '0.0.0.0/0' "
        "-ErrorAction SilentlyContinue | "
        f"Where-Object {{ $_.InterfaceAlias -match '{pattern}' }} | "
        "Select-Object -First 1 -ExpandProperty InterfaceAlias"
    )
    try:
        startupinfo = None
        if hasattr(subprocess, "STARTUPINFO"):
            startupinfo = subprocess.STARTUPINFO()
            startupinfo.dwFlags |= subprocess.STARTF_USESHOWWINDOW
            startupinfo.wShowWindow = 0
        result = subprocess.run(
            ["powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", command],
            capture_output=True,
            text=True,
            timeout=3,
            startupinfo=startupinfo,
            creationflags=getattr(subprocess, "CREATE_NO_WINDOW", 0x08000000),
        )
        if result.returncode == 0:
            return (result.stdout or "").strip().splitlines()[0].strip() if result.stdout.strip() else ""
    except Exception:
        pass
    return ""


def get_steam_pids() -> List[int]:
    """返回正在运行的 steam.exe PID，用于开启加速前提醒。"""
    pids: List[int] = []
    try:
        import psutil
        for proc in psutil.process_iter(["pid", "name"]):
            try:
                name = (proc.info.get("name") or "").lower()
                if name == "steam.exe":
                    pids.append(int(proc.info["pid"]))
            except Exception:
                continue
    except Exception:
        try:
            import subprocess
            startupinfo = None
            if hasattr(subprocess, "STARTUPINFO"):
                startupinfo = subprocess.STARTUPINFO()
                startupinfo.dwFlags |= subprocess.STARTF_USESHOWWINDOW
                startupinfo.wShowWindow = 0
            output = subprocess.run(
                ["tasklist", "/FO", "CSV", "/NH"],
                capture_output=True, text=True, timeout=5,
                startupinfo=startupinfo,
            ).stdout
            for line in output.splitlines():
                parts = [part.strip().strip('"') for part in line.split(",")]
                if len(parts) >= 2 and parts[0].lower() == "steam.exe":
                    try:
                        pids.append(int(parts[1]))
                    except ValueError:
                        pass
        except Exception:
            pass
    return sorted(pids)


def set_system_proxy(
    enable: bool,
    socks_addr: str = f"127.0.0.1:{DEFAULT_SOCKS_PORT}",
    http_addr: str = f"127.0.0.1:{DEFAULT_HTTP_PORT}",
):
    """Enable or disable the current user's WinINet HTTP/HTTPS/SOCKS proxy."""
    key_path = r"Software\Microsoft\Windows\CurrentVersion\Internet Settings"
    with winreg.OpenKey(
        winreg.HKEY_CURRENT_USER, key_path, 0, winreg.KEY_WRITE,
    ) as key:
        winreg.SetValueEx(key, "ProxyEnable", 0, winreg.REG_DWORD, 1 if enable else 0)
        if enable:
            proxy_value = f"http={http_addr};https={http_addr};socks={socks_addr}"
            winreg.SetValueEx(key, "ProxyServer", 0, winreg.REG_SZ, proxy_value)
        else:
            winreg.SetValueEx(key, "ProxyServer", 0, winreg.REG_SZ, "")

    ctypes.windll.Wininet.InternetSetOptionW(0, 39, 0, 0)
    ctypes.windll.Wininet.InternetSetOptionW(0, 37, 0, 0)


def _first_valid_ipv4(raw) -> str:
    """从扫描结果里提取第一个有效的 IPv4 地址（兼容字符串/逗号/列表）。"""
    candidates: List[str] = []
    if isinstance(raw, list):
        for item in raw:
            candidates.extend(str(item).split(","))
    else:
        candidates.extend(str(raw).split(","))
    for cand in candidates:
        ip = cand.strip()
        parts = ip.split(".")
        if len(parts) == 4 and all(p.isdigit() for p in parts):
            return ip
    return ""


def create_main_window():
    """工厂函数：创建 MainWindow 实例（此时 QApplication 已存在）"""
    from PySide6.QtCore import Qt, QThread, Signal, Slot, QTimer, QSettings, QRect, QRectF, QPoint
    from PySide6.QtGui import QIcon, QAction, QFont, QPainter, QColor, QCursor
    from PySide6.QtWidgets import QApplication, QSystemTrayIcon, QMenu, QWidget
    from qfluentwidgets import (
        FluentWindow, NavigationItemPosition, InfoBar, InfoBarPosition,
        setThemeColor, setTheme, Theme, FluentIcon, qconfig, MessageBox,
    )
    setThemeColor("#0078d4")

    # 启动即应用持久化主题（浅色/深色/跟随系统）
    _theme_settings = QSettings("Hypostasis-Cat", "HypoMux")
    _theme_map = {"auto": Theme.AUTO, "light": Theme.LIGHT, "dark": Theme.DARK}
    setTheme(_theme_map.get(_theme_settings.value("theme", "auto"), Theme.AUTO))

    from ui.i18n import tr
    from ui.pages import resolve_icon
    from ui.pages.home_page import HomePage
    from ui.pages.routing_page import RoutingPage
    from ui.pages.tools_page import ToolsPage
    from ui.pages.settings_page import SettingsPage
    from ui.pages.about_page import AboutPage

    def _patch_navigation_icon_paint():
        try:
            from qfluentwidgets.components.navigation.navigation_widget import NavigationPushButton
            from qfluentwidgets.common.config import isDarkTheme
            from qfluentwidgets.common.icon import drawIcon
            from qfluentwidgets.common.color import autoFallbackThemeColor
        except Exception:
            return

        if getattr(NavigationPushButton, "_hypomux_icon_patch", False):
            return

        def paint_event(self, event):
            painter = QPainter(self)
            painter.setRenderHints(
                QPainter.Antialiasing
                | QPainter.TextAntialiasing
                | QPainter.SmoothPixmapTransform
            )
            painter.setPen(Qt.NoPen)

            if self.isPressed:
                painter.setOpacity(0.7)
            if not self.isEnabled():
                painter.setOpacity(0.4)

            c = 255 if isDarkTheme() else 0
            margins = self._margins()
            pl, pr = margins.left(), margins.right()
            global_rect = QRect(self.mapToGlobal(QPoint()), self.size())

            if self._canDrawIndicator():
                painter.setBrush(QColor(c, c, c, 6 if self.isEnter else 10))
                painter.drawRoundedRect(self.rect(), 5, 5)
                painter.setBrush(autoFallbackThemeColor(self.lightIndicatorColor, self.darkIndicatorColor))
                painter.drawRoundedRect(self.indicatorRect(), 1.5, 1.5)
            elif ((self.isEnter and global_rect.contains(QCursor.pos())) or self.isAboutSelected) and self.isEnabled():
                painter.setBrush(QColor(c, c, c, 6 if self.isAboutSelected else 10))
                painter.drawRoundedRect(self.rect(), 5, 5)

            icon_size = 20
            icon_x = 10 + pl
            icon_y = (self.height() - icon_size) / 2
            drawIcon(self._icon, painter, QRectF(icon_x, icon_y, icon_size, icon_size))

            if self.isCompacted:
                return

            painter.setFont(self.font())
            painter.setPen(self.textColor())
            left = 44 + pl if not self.icon().isNull() else pl + 16
            painter.drawText(
                QRectF(left, 0, self.width() - 13 - left - pr, self.height()),
                Qt.AlignVCenter,
                self.text(),
            )

        NavigationPushButton.paintEvent = paint_event
        NavigationPushButton._hypomux_icon_patch = True

    _patch_navigation_icon_paint()

    MAIN_WINDOW_TEXT = {
        "zh": {
            "log_scan_thread_error": "[ERROR] 扫描线程异常: {error}",
            "log_starting": "[启动] 准备启动双协议分流引擎，SOCKS {socks}，HTTP/HTTPS {http}，参与网卡: {nics}",
            "log_start_exception": "[启动] 分流引擎启动异常: {error}",
            "log_stop_requested": "[停止] 已发送安全停止指令，正在关闭监听并清理在途连接...",
            "log_proxy_disabled": "[系统代理] 已强制关闭 Windows 全局代理",
            "log_proxy_enabled": "[系统代理] 已接管 Windows 全局代理: http={http};https={http};socks={socks}",
            "log_proxy_enable_failed": "[系统代理] 启用失败，正在停止引擎: {error}",
            "log_error": "[错误] {message}",
            "log_start_failed_cleanup": "[系统代理] 启动失败，已强制关闭 Windows 全局代理",
            "log_start_cleanup_error": "[系统代理] 启动失败后的清理异常: {error}",
            "log_stopped": "[已停止] {message}",
            "log_stop_cleanup_error": "[系统代理] 停止后的清理异常: {error}",
            "log_stop_fallback": "[停止] 后台连接清理耗时过长，已释放界面并保持系统代理关闭",
            "log_stop_fallback_error": "[系统代理] 超时兜底清理异常: {error}",
            "log_close_cleanup_error": "[ERROR] 退出清理异常: {error}",
            "log_close_proxy_error": "[ERROR] 系统代理关闭异常: {error}",
            "log_steam_running": "[警告] 检测到 Steam 正在运行，请重启 Steam 客户端以使多链路加速完全生效。",
            "log_mode_changed": "[模式] 已切换为 {mode}",
            "log_tun_config_failed": "[TUN] 生成 sing-box 配置失败: {error}",
            "log_tun_dns_plan": "[TUN] DNS 上游: 系统自动出口 | 进程直连规则: {paths}",
            "log_tun_pool_ready": "[TUN] 出站池 ready: {info}",
            "log_tun_pool_failed": "[TUN] 出站池启动失败: {message}",
            "log_tun_pool_timeout": "[TUN] 出站池启动超时，已取消虚拟网卡接管",
            "log_diag_result": "[体检] {name} -> {status} · {loss_label} {loss}% · {jitter_label} {jitter}ms",
            "proxy_started_success": "已接管系统代理 · HTTP/HTTPS {http} · SOCKS {socks}",
        },
        "en": {
            "log_scan_thread_error": "[ERROR] Adapter scan thread error: {error}",
            "log_starting": "[Start] Starting dual-protocol engine, SOCKS {socks}, HTTP/HTTPS {http}, adapters: {nics}",
            "log_start_exception": "[Start] Engine start exception: {error}",
            "log_stop_requested": "[Stop] Stop requested, closing listeners and cleaning active connections...",
            "log_proxy_disabled": "[System Proxy] Windows global proxy has been disabled",
            "log_proxy_enabled": "[System Proxy] Windows global proxy enabled: http={http};https={http};socks={socks}",
            "log_proxy_enable_failed": "[System Proxy] Enable failed, stopping engine: {error}",
            "log_error": "[Error] {message}",
            "log_start_failed_cleanup": "[System Proxy] Start failed, Windows global proxy has been disabled",
            "log_start_cleanup_error": "[System Proxy] Cleanup after start failure failed: {error}",
            "log_stopped": "[Stopped] {message}",
            "log_stop_cleanup_error": "[System Proxy] Cleanup after stop failed: {error}",
            "log_stop_fallback": "[Stop] Background cleanup took too long; UI released and system proxy remains disabled",
            "log_stop_fallback_error": "[System Proxy] Timeout fallback cleanup failed: {error}",
            "log_close_cleanup_error": "[ERROR] Exit cleanup failed: {error}",
            "log_close_proxy_error": "[ERROR] System proxy cleanup failed: {error}",
            "log_steam_running": "[Warning] Steam is running. Please restart the Steam client for multi-link acceleration to take full effect.",
            "log_mode_changed": "[Mode] Switched to {mode}",
            "log_tun_config_failed": "[TUN] Failed to generate sing-box config: {error}",
            "log_tun_dns_plan": "[TUN] DNS upstream: automatic system outbound | Process direct rules: {paths}",
            "log_tun_pool_ready": "[TUN] Outbound pool ready: {info}",
            "log_tun_pool_failed": "[TUN] Outbound pool startup failed: {message}",
            "log_tun_pool_timeout": "[TUN] Outbound pool startup timed out; Virtual NIC takeover was cancelled",
            "log_diag_result": "[Health] {name} -> {status} · {loss_label} {loss}% · {jitter_label} {jitter}ms",
            "proxy_started_success": "System proxy enabled · HTTP/HTTPS {http} · SOCKS {socks}",
        },
    }

    def main_language() -> str:
        settings = QSettings("Hypostasis-Cat", "HypoMux")
        lang = settings.value("ui/language", settings.value("language", "zh"))
        return lang if lang in ("zh", "en") else "zh"

    def mw_tr(key: str, **kwargs) -> str:
        lang = main_language()
        text = MAIN_WINDOW_TEXT.get(lang, MAIN_WINDOW_TEXT["zh"]).get(key)
        if text is None:
            text = tr(key)
        if kwargs:
            try:
                return text.format(**kwargs)
            except (KeyError, ValueError):
                return text
        return text

    def localize_runtime_message(message: str) -> str:
        """Translate known backend runtime messages before showing them in the UI."""
        text = str(message)
        if main_language() != "en":
            return text

        prefix_map = {
            "代理内核异常退出: ": "Proxy engine exited unexpectedly: ",
            "无法监听 ": "Failed to listen on ",
            "多端口出站池异常退出: ": "Multi-port outbound pool exited unexpectedly: ",
            "多端口出站池监听失败: ": "Multi-port outbound pool failed to listen: ",
            "TUN 内核守护异常: ": "TUN kernel watchdog failed: ",
            "sing-box 配置不存在: ": "sing-box config does not exist: ",
            "启动 sing-box.exe 失败: ": "Failed to start sing-box.exe: ",
            "sing-box 内核意外退出 ": "sing-box kernel exited unexpectedly ",
        }
        exact_map = {
            "代理已停止": "Proxy stopped",
            "多端口出站池已停止": "Multi-port outbound pool stopped",
            "TUN 内核已停止": "TUN kernel stopped",
            "未找到 bin/sing-box.exe，无法启动虚拟网卡模式": "bin/sing-box.exe was not found, cannot start Virtual NIC mode",
        }
        if text in exact_map:
            return exact_map[text]
        for zh_prefix, en_prefix in prefix_map.items():
            if text.startswith(zh_prefix):
                return en_prefix + text[len(zh_prefix):]
        return text

    # ========== 后台扫描线程 ==========
    class ScanWorker(QThread):
        scan_finished = Signal(bool, list, str)

        def run(self):
            try:
                success, adapters, error_msg = scan_network_adapters()
                self.scan_finished.emit(success, adapters, error_msg)
            except Exception as e:
                print(mw_tr("log_scan_thread_error", error=e))
                self.scan_finished.emit(False, [], str(e))

    # ========== 后台诊断线程（第二阶段，原样保留）==========
    class DiagnosticWorker(QThread):
        result_ready = Signal(dict)
        all_finished = Signal()
        diag_error = Signal(str)

        def __init__(self, adapters, target_ip=DEFAULT_TARGET_IP, parent=None):
            super().__init__(parent)
            self._adapters = adapters
            self._target_ip = target_ip

        def run(self):
            import asyncio as _asyncio
            try:
                loop = _asyncio.new_event_loop()
                _asyncio.set_event_loop(loop)
                try:
                    loop.run_until_complete(self._run_all())
                finally:
                    loop.close()
            except Exception as e:
                self.diag_error.emit(str(e))

        async def _run_all(self):
            for nic in self._adapters:
                try:
                    result = await run_diagnostic(nic.get("ip", ""), self._target_ip)
                except Exception as e:
                    result = {
                        "status": "unavailable", "loss_rate": 100,
                        "avg_latency_ms": 0, "jitter_ms": 0,
                        "src_ip": nic.get("ip", ""), "note": str(e),
                    }
                result["name"] = nic.get("name", nic.get("ip", ""))
                result["ip"] = nic.get("ip", "")
                self.result_ready.emit(result)
            self.all_finished.emit()

    # ========== 主窗口 ==========
    class MainWindow(FluentWindow):
        """HypoMux 主窗口（FluentWindow 侧边导航）"""

        def __init__(self):
            set_system_proxy(False)
            super().__init__()

            # ===== 配置与运行状态 =====
            self._app_config = load_config()
            self._adapters: List[Dict] = []           # 扫描得到的网卡原始列表
            self._checked_aliases = set(self._app_config.get("selected_adapters", []))

            self.scan_worker = ScanWorker()
            self.diag_worker = None
            self.proxy_worker = None
            self._is_boosting = False
            self._pending_socks_addr = ""
            self._pending_http_addr = ""
            self._retired_proxy_workers = []
            self._last_up_mbps = 0.0
            self._last_conn_count = 0

            # 任务1/3：运行模式与多端口出站池 / TUN 内核
            self._run_mode = self._app_config.get("run_mode", "proxy")
            self._routing_rules = self._app_config.get("routing_rules", [])
            self._pool_worker = None      # MultiPortProxyWorker（TUN 模式下的出站池）
            self._tun_manager = None      # sing-box 内核侧车
            self._tun_active = False
            self._tun_starting = False
            self._shutdown_started = False
            self._force_exit = False
            self._engine_transitioning = False

            self._stop_fallback_timer = QTimer(self)
            self._stop_fallback_timer.setSingleShot(True)
            self._stop_fallback_timer.timeout.connect(self._force_finish_stop_ui)
            self._tun_pool_start_timer = QTimer(self)
            self._tun_pool_start_timer.setSingleShot(True)
            self._tun_pool_start_timer.timeout.connect(self._on_tun_pool_start_timeout)

            self._InfoBar = InfoBar
            self._InfoBarPosition = InfoBarPosition
            self._Qt = Qt

            # ===== 窗口外观 =====
            self.setWindowTitle("HypoMux")
            self.apply_standard_geometry()

            # 任务1：开启 Windows 11 原生 Mica/云母毛玻璃材质。
            # 不硬编码窗口背景 QSS，使浅色/深色模式均呈现高阶半透明质感。
            try:
                self.setMicaEffectEnabled(True)
            except Exception:
                pass

            # ===== 构建页面与导航 =====
            self._init_pages()
            self._init_navigation()
            self._refine_navigation_appearance()
            self._connect_page_signals()

            # ===== 系统托盘 =====
            self._init_system_tray()

            # ===== 后端信号 =====
            self.scan_worker.scan_finished.connect(self.on_scan_finished)

            # 任务4：监听主题切换，重绘高亮控件，根治深浅切换后蓝字坍塌为黑字
            try:
                qconfig.themeChanged.connect(self._on_theme_changed)
            except Exception:
                pass

            # ===== 启动扫描 =====
            self.load_adapters()
            self._sync_engine_ports()

        def _on_theme_changed(self, *args):
            """主题切换回调：延迟重绘高亮控件，避免取到旧主题色。"""
            self._refresh_theme_sensitive_pages()
            QTimer.singleShot(80, self._refresh_theme_sensitive_pages)

        def _refresh_theme_sensitive_pages(self):
            for page in (
                self.home_page,
                self.routing_page,
                self.tools_page,
                self.settings_page,
                self.about_page,
            ):
                refresh = getattr(page, "refresh_theme", None)
                if callable(refresh):
                    try:
                        refresh()
                    except Exception:
                        pass

        def apply_standard_geometry(self):
            """统一窗口标准尺寸，避免开机会话 DPI 初始化阶段尺寸漂移。"""
            self.setMinimumSize(960, 680)
            self.resize(1120, 800)

        # ---------- 页面与导航 ----------
        def _init_pages(self):
            self.home_page = HomePage(self)
            self.routing_page = RoutingPage(self)
            self.tools_page = ToolsPage(self)
            self.settings_page = SettingsPage(self)
            self.about_page = AboutPage(self)

        def _init_navigation(self):
            # 图标方案（用户确认）：HOME / GLOBAL(回退 GLOBE/IOT) / SPEED_HIGH / SETTING
            self.addSubInterface(
                self.home_page, FluentIcon.HOME, tr("nav_home")
            )
            self.addSubInterface(
                self.routing_page, resolve_icon("GLOBAL", "GLOBE", "IOT"), tr("nav_routing")
            )
            self.addSubInterface(
                self.tools_page, FluentIcon.SPEED_HIGH, tr("nav_tools")
            )
            # 任务3：系统设置挪到顶部主功能组（移除 BOTTOM）
            self.addSubInterface(
                self.settings_page, FluentIcon.SETTING, tr("nav_settings")
            )
            # 关于页归入主业务导航流，保持左下角视觉清爽
            self.addSubInterface(
                self.about_page, FluentIcon.INFO, tr("nav_about")
            )

        def _refine_navigation_appearance(self):
            """Refine the Fluent navigation bar without forcing a broken expanded state."""
            # 1) 砍掉左上角鸡肋的返回按钮
            try:
                self.navigationInterface.setReturnButtonVisible(False)
            except Exception:
                pass
            # 兼容部分版本提供的窗口级开关
            try:
                self.setBackButtonVisible(False)
            except Exception:
                pass

            # Keep collapse/expand behavior native, but make the expanded rail less wide.
            try:
                self.navigationInterface.setExpandWidth(220)
            except Exception:
                pass

            nav_font = QFont("Microsoft YaHei", 12)
            nav_font.setWeight(QFont.Normal)
            try:
                self.navigationInterface.setFont(nav_font)
                for btn in self.navigationInterface.findChildren(QWidget):
                    try:
                        btn.setFont(nav_font)
                    except Exception:
                        pass
                    class_name = btn.metaObject().className()
                    if "Navigation" not in class_name:
                        continue
            except Exception:
                pass

        def _connect_page_signals(self):
            # 首页
            self.home_page.engine_toggled.connect(self.on_engine_toggled)
            self.home_page.select_all_clicked.connect(self.on_select_all_clicked)
            self.home_page.deselect_all_clicked.connect(self.on_deselect_all_clicked)
            self.home_page.refresh_clicked.connect(self.load_adapters)
            self.home_page.adapter_checked.connect(self.on_adapter_checked)
            self.home_page.mode_changed.connect(self.on_mode_changed)
            # 工具页（任务2：体检页也能勾选网卡，并入选择流）
            self.tools_page.start_clicked.connect(self.on_diagnose_clicked)
            self.tools_page.adapter_checked.connect(self.on_adapter_checked)
            self.tools_page.refresh_clicked.connect(self.load_adapters)
            # 路由页（任务2：规则变更即持久化）
            self.routing_page.rules_changed.connect(self.on_routing_rules_changed)
            # 设置页
            self.settings_page.language_changed.connect(self._on_language_changed)
            self.settings_page.ports_changed.connect(self._on_settings_ports_changed)
            self.settings_page.info_message.connect(self.show_info)
            self.settings_page.success_message.connect(self.show_success)
            self.settings_page.warning_message.connect(self.show_warning)
            self.settings_page.dns_changed.connect(self._on_dns_changed)

            # 启动恢复：运行模式分段控件 + 路由规则表
            try:
                self.home_page.set_mode(self._run_mode)
                self.routing_page.load_rules(self._routing_rules)
            except Exception:
                pass

        # ---------- 系统托盘 ----------
        def _init_system_tray(self):
            import os
            self.tray_icon = QSystemTrayIcon(self)
            icon_path = os.path.join(
                os.path.dirname(os.path.dirname(os.path.abspath(__file__))),
                "assets", "icon.ico"
            )
            if os.path.exists(icon_path):
                self.tray_icon.setIcon(QIcon(icon_path))
            else:
                self.tray_icon.setIcon(self.windowIcon())
            self.tray_icon.setToolTip(tr("tray_tooltip"))

            tray_menu = QMenu()
            show_action = QAction(tr("tray_show_main"), self)
            show_action.triggered.connect(self._show_from_tray)
            tray_menu.addAction(show_action)
            tray_menu.addSeparator()
            exit_action = QAction(tr("tray_exit"), self)
            exit_action.triggered.connect(self._exit_from_tray)
            tray_menu.addAction(exit_action)

            self.tray_icon.setContextMenu(tray_menu)
            self.tray_icon.activated.connect(self._on_tray_activated)
            self.tray_icon.show()

        def _on_tray_activated(self, reason):
            if reason == QSystemTrayIcon.DoubleClick:
                self._show_from_tray()

        def _show_from_tray(self):
            self.apply_standard_geometry()
            self.show()
            self.setWindowState(self.windowState() & ~Qt.WindowMinimized | Qt.WindowActive)
            self.activateWindow()
            self.raise_()

        def _exit_from_tray(self):
            self._force_exit = True
            try:
                self.shutdown_backend_workers()
            finally:
                if hasattr(self, "tray_icon"):
                    self.tray_icon.hide()
                self.close()
                QApplication.quit()

        # ---------- 国际化刷新 ----------
        def _on_language_changed(self, lang_code: str):
            settings = QSettings("Hypostasis-Cat", "HypoMux")
            settings.setValue("ui/language", lang_code)
            settings.setValue("language", lang_code)
            settings.sync()
            self.setWindowTitle("HypoMux")
            self.home_page.retranslate_ui()
            self.routing_page.retranslate_ui()
            self.tools_page.retranslate_ui()
            self.settings_page.retranslate_ui()
            self.about_page.retranslate_ui()
            self.tray_icon.setToolTip(tr("tray_tooltip"))

        # ---------- 端口同步 ----------
        def _sync_engine_ports(self):
            socks = int(self._app_config.get("socks_port", DEFAULT_SOCKS_PORT))
            http = int(self._app_config.get("http_port", socks + 1))
            self.home_page.set_engine_state(self._is_boosting, socks, http)

        def _on_settings_ports_changed(self, socks_port: int, http_port: int):
            self._app_config["socks_port"] = socks_port
            self._app_config["http_port"] = http_port
            self._persist_config()
            self._sync_engine_ports()

        def _on_dns_changed(self, dns_server: str, doh_provider: str = "auto"):
            self._app_config["dns_server"] = dns_server
            self._app_config["doh_provider"] = doh_provider
            self._persist_config()
            if self._tun_active:
                self._regenerate_singbox_config()

        # ========== 配置持久化 ==========
        def _collect_config(self) -> dict:
            socks = int(self._app_config.get("socks_port", DEFAULT_SOCKS_PORT))
            return {
                "selected_adapters": sorted(self._checked_aliases),
                "socks_port": socks,
                "http_port": socks + 1,
                "run_mode": self._run_mode,
                "routing_rules": self._routing_rules,
                "dns_server": self._app_config.get("dns_server", "223.5.5.5"),
                "doh_provider": self._app_config.get("doh_provider", "auto"),
            }

        def _persist_config(self):
            try:
                self._app_config = self._collect_config()
                save_config(self._app_config)
            except Exception as e:
                print(f"[WARN] Failed to save config: {e}")

        def on_adapter_checked(self, alias: str, checked: bool):
            if checked:
                self._checked_aliases.add(alias)
            else:
                self._checked_aliases.discard(alias)
            # 任务2：跨屏双向同步——首页与体检页勾选状态实时一致
            self.home_page.set_card_checked(alias, checked)
            self.tools_page.set_card_checked(alias, checked)
            self._persist_config()

        def on_select_all_clicked(self):
            self._checked_aliases = {a["alias"] for a in self._adapters}
            self.home_page.set_all_checked(True)
            self.tools_page.set_all_checked(True)
            self._persist_config()

        def on_deselect_all_clicked(self):
            self._checked_aliases.clear()
            self.home_page.set_all_checked(False)
            self.tools_page.set_all_checked(False)
            self._persist_config()

        # ========== 选中网卡解析（供 ProxyWorker / 诊断使用）==========
        def get_selected_adapters(self) -> List[Dict]:
            """返回被勾选且拥有有效 IPv4 的网卡，保留出站池所需的类型元数据。"""
            selected = []
            for a in self._adapters:
                if a["alias"] not in self._checked_aliases:
                    continue
                ip = _first_valid_ipv4(a.get("ipv4", ""))
                if not ip:
                    continue
                selected.append({
                    "index": a["index"],
                    "name": a["alias"],
                    "ip": ip,
                    "dns_servers": a.get("dns_servers", []),
                    "iftype": a.get("iftype", -1),
                    "is_ppp": bool(a.get("is_ppp", False)),
                    "metric": a.get("metric", -1),
                })
            return selected

        # ========== 网卡扫描 ==========
        def load_adapters(self):
            if self._is_boosting:
                self.show_warning(tr("warn_boosting_refresh"))
                return
            self.home_page.refresh_btn.setEnabled(False)
            if self.scan_worker.isRunning():
                self.scan_worker.wait()
            self.scan_worker.start()

        @Slot(bool, list, str)
        def on_scan_finished(self, success: bool, adapters: list, error_msg: str):
            self.home_page.refresh_btn.setEnabled(True)
            if not success:
                self.show_error(tr("error_load_adapters", error=error_msg))
                return
            if not adapters:
                self.show_warning(tr("warn_no_adapters"))

            self._adapters = adapters
            # 预先为每张网卡补一个 'ip' 字段（首个有效 IPv4），供卡片显示
            for a in self._adapters:
                a["ip"] = _first_valid_ipv4(a.get("ipv4", ""))
            # 仅保留仍存在的勾选别名
            existing = {a["alias"] for a in self._adapters}
            self._checked_aliases &= existing
            # 任务2：首页 + 体检页同步重建网卡卡片（同一份数据 + 勾选态）
            self.home_page.rebuild_cards(self._adapters, sorted(self._checked_aliases))
            self.tools_page.rebuild_cards(self._adapters, sorted(self._checked_aliases))
            self.routing_page.set_available_adapters(self._adapters)

        # ========== 引擎开关（首页 SwitchButton）==========
        def on_engine_toggled(self, checked: bool):
            if self._engine_transitioning:
                self._sync_engine_ports()
                return
            if self._run_mode == "tun":
                already_running = self._tun_active or self._tun_starting
            else:
                already_running = self._is_boosting or self.proxy_worker is not None
            if checked == already_running:
                return
            self._engine_transitioning = True
            self.home_page.set_adapter_controls_enabled(False)
            # Give qfluentwidgets' SwitchButton animation time to finish before
            # running heavier startup/teardown work on the UI thread.
            QTimer.singleShot(280, lambda: self.home_page.set_engine_busy(True))
            QTimer.singleShot(340, lambda state=checked: self._apply_engine_toggle(state))

        def _apply_engine_toggle(self, checked: bool):
            # 任务3/4：虚拟网卡模式走 TUN 内核分支
            if self._run_mode == "tun":
                if checked and not self._tun_active:
                    self._start_tun_mode()
                elif not checked and self._tun_active:
                    self._stop_tun_mode()
                else:
                    self._finish_engine_transition()
                return
            # 系统代理模式（既有逻辑，零改动）
            if checked and not self._is_boosting:
                self._start_proxy()
            elif not checked and self._is_boosting:
                self._stop_proxy()
            else:
                self._finish_engine_transition()

        def _finish_engine_transition(self):
            self._engine_transitioning = False
            self.home_page.set_engine_busy(False)

        # ========== 任务4：运行模式切换 + UAC 防御 ==========
        def on_mode_changed(self, mode: str):
            """用户切换运行模式。切到 tun 立即做管理员权限物理探针。"""
            # 加速运行期间禁止切换内核模式；HomePage 已置灰，这里做防御式回滚。
            if self._is_boosting or self._tun_active:
                self.home_page.set_mode(self._run_mode)
                return

            if mode == "tun":
                if not self._is_admin():
                    # 未提权：弹高颜值纯文本 MessageBox，并安全回滚到代理模式
                    box = MessageBox(
                        tr("tun_need_admin_title"),
                        tr("tun_need_admin_content"),
                        self,
                    )
                    box.yesButton.setText(tr("mode_proxy"))
                    box.cancelButton.hide()
                    box.exec()
                    self._run_mode = "proxy"
                    self.home_page.set_mode("proxy")
                    self.home_page.set_engine_state(False)
                    self._persist_config()
                    return
            self._run_mode = mode
            self._persist_config()
            self.append_log(mw_tr(
                "log_mode_changed",
                mode=tr("mode_tun") if mode == "tun" else tr("mode_proxy"),
            ))

        @staticmethod
        def _is_admin() -> bool:
            """物理探针：是否以管理员权限运行。"""
            try:
                return bool(ctypes.windll.shell32.IsUserAnAdmin())
            except Exception:
                return False

        # ========== 任务2：路由规则变更 ==========
        def on_routing_rules_changed(self):
            self._routing_rules = self.routing_page.get_rules()
            self._persist_config()
            # 若 TUN 正在运行，热重生成配置（下次重启内核生效）
            if self._tun_active:
                self._regenerate_singbox_config()

        def _singbox_config_path(self):
            # 固定写入 ~/.hypomux/singbox-config.json：该目录对当前用户始终可写，
            # 且不依赖 __file__（onefile 打包态下 __file__ 会落到临时解包目录，
            # 与 sys.executable 解析出的 bin 目录分叉）。sing-box 以绝对路径加载，
            # 与工作目录无关。
            config_dir = Path.home() / ".hypomux"
            config_dir.mkdir(parents=True, exist_ok=True)
            return str(config_dir / "singbox-config.json")

        def _regenerate_singbox_config(self) -> bool:
            """据当前路由规则重新序列化 sing-box config.json。"""
            try:
                return singbox_config.generate_config_file(
                    self._routing_rules,
                    self._singbox_config_path(),
                    app_process_path=self._app_process_paths(),
                )
            except Exception as e:
                self.append_log(mw_tr("log_tun_config_failed", error=e))
                return False

        def _app_process_paths(self) -> List[str]:
            """收集源码运行/Nuitka 打包运行时可能出现的宿主进程路径。"""
            paths = []
            for raw in (
                sys.executable,
                sys.argv[0] if sys.argv else "",
                Path(sys.executable).with_name("HypoMux.exe"),
                Path(sys.executable).with_name("main.exe"),
                Path(sys.executable).with_name("python.exe"),
            ):
                try:
                    path = str(Path(raw).resolve())
                except Exception:
                    path = str(raw).strip()
                if path and path not in paths:
                    paths.append(path)
            return paths

        # ========== 任务3：虚拟网卡（TUN）模式启停 ==========
        def _start_tun_mode(self):
            # 二次权限确认（防御式熔断）
            if not self._is_admin():
                box = MessageBox(
                    tr("tun_need_admin_title"), tr("tun_need_admin_content"), self
                )
                box.cancelButton.hide()
                box.exec()
                self.home_page.set_engine_state(False)
                self._finish_engine_transition()
                return

            foreign_tun = detect_foreign_tun_default_route()
            if foreign_tun:
                self.show_warning(tr("warn_foreign_tun_route", name=foreign_tun))
                self.home_page.set_engine_state(False)
                self._finish_engine_transition()
                return

            selected = self.get_selected_adapters()
            if not selected:
                self.show_warning(tr("warn_no_selection"))
                self.home_page.set_engine_state(False)
                self._finish_engine_transition()
                return
            self.append_log(mw_tr(
                "log_tun_dns_plan",
                paths=", ".join(self._app_process_paths()),
            ))

            # 1) 先拉起 Python 多端口出站池（2001/2002/2003）
            try:
                self._pool_worker = MultiPortProxyWorker(selected_nics=selected)
                self._pool_worker.set_dns_servers([self._app_config.get("dns_server", "223.5.5.5")])
                self._pool_worker.set_doh_provider(self._app_config.get("doh_provider", "auto"))
                self._pool_worker.log_signal.connect(self.on_proxy_log)
                self._pool_worker.traffic_signal.connect(self.on_proxy_traffic)
                self._pool_worker.started_ok.connect(self._on_tun_pool_started)
                self._pool_worker.error_signal.connect(self._on_tun_pool_error)
                self._tun_starting = True
                self.home_page.engine_switch.setEnabled(False)
                self.home_page.set_adapter_controls_enabled(False)
                self.routing_page.set_controls_enabled(False)
                self.settings_page.set_controls_enabled(False)
                self.tools_page.set_controls_enabled(False)
                self._tun_pool_start_timer.start(5000)
                self._pool_worker.start()
            except Exception as e:
                self._pool_worker = None
                self._tun_starting = False
                self.show_error(tr("error_start_failed", error=e))
                self.home_page.set_engine_state(False)
                self._finish_engine_transition()
                return

        def _on_tun_pool_started(self, info: str):
            if not self._tun_starting or self._pool_worker is None:
                return
            self._tun_pool_start_timer.stop()
            self.append_log(mw_tr("log_tun_pool_ready", info=info))

            # 2) 生成 sing-box 配置
            if not self._regenerate_singbox_config():
                self._teardown_pool()
                self._tun_starting = False
                self._exit_boosting_ui()
                self.home_page.set_engine_state(False)
                self._finish_engine_transition()
                return

            # 3) 拉起 sing-box TUN 内核侧车
            self.append_log(tr("tun_starting"))
            self._tun_manager = TunManager(self._singbox_config_path())
            self._tun_manager.log_signal.connect(self.on_proxy_log)
            self._tun_manager.started_ok.connect(self._on_tun_started)
            self._tun_manager.error_signal.connect(self._on_tun_error)
            self._tun_manager.stopped.connect(self._on_tun_stopped)
            self._tun_active = True
            self.home_page.set_engine_state(True)
            self.routing_page.set_controls_enabled(False)
            self.settings_page.set_controls_enabled(False)
            self.tools_page.set_controls_enabled(False)
            self._tun_manager.start()

        def _on_tun_pool_error(self, message: str):
            if not self._tun_starting and not self._tun_active:
                return
            self._tun_pool_start_timer.stop()
            message = localize_runtime_message(message)
            self.append_log(mw_tr("log_tun_pool_failed", message=message))
            self.show_error(message)
            self._tun_starting = False
            self._teardown_pool()
            self._exit_boosting_ui()
            self.home_page.set_engine_state(False)
            self._finish_engine_transition()

        def _on_tun_pool_start_timeout(self):
            if not self._tun_starting or self._tun_active:
                return
            self.append_log(mw_tr("log_tun_pool_timeout"))
            self.show_error(tr("tun_pool_start_timeout"))
            self._tun_starting = False
            self._teardown_pool()
            self._exit_boosting_ui()
            self.home_page.set_engine_state(False)
            self._finish_engine_transition()

        def _on_tun_started(self, info: str):
            self._tun_starting = False
            self._enter_boosting_ui()
            self.home_page.set_engine_state(True)
            self._finish_engine_transition()
            self.show_success(tr("tun_started"))

        def _on_tun_error(self, message: str):
            message = localize_runtime_message(message)
            self.append_log(f"[TUN] {message}")
            self.show_error(message)
            self._stop_tun_mode()

        def _on_tun_stopped(self, message: str):
            self.append_log(localize_runtime_message(message) or tr("tun_stopped"))

        def _teardown_pool(self):
            if self._pool_worker is not None:
                try:
                    self._pool_worker.stop()
                    if self._pool_worker.isRunning():
                        self._pool_worker.wait(3000)
                except Exception:
                    pass
                self._pool_worker = None

        def _stop_tun_mode(self):
            try:
                self._tun_pool_start_timer.stop()
            except Exception:
                pass
            # 1) 杀 sing-box 内核 + 清路由
            if self._tun_manager is not None:
                try:
                    self._tun_manager.stop()
                    if self._tun_manager.isRunning():
                        self._tun_manager.wait(6000)
                    # 兜底强杀，杜绝残留导致断网
                    self._tun_manager.force_kill()
                except Exception:
                    pass
                self._tun_manager = None
            # 2) 关 Python 出站池
            self._teardown_pool()
            self._tun_active = False
            self._tun_starting = False
            self._exit_boosting_ui()
            self.home_page.set_engine_state(False)
            self._finish_engine_transition()

        def _start_proxy(self):
            selected = self.get_selected_adapters()
            if not selected:
                self.show_warning(tr("warn_no_selection"))
                # 回滚开关
                self.home_page.set_engine_state(False)
                self._finish_engine_transition()
                return
            if self.proxy_worker is not None:
                self._finish_engine_transition()
                return

            if get_steam_pids():
                self.show_warning(tr("warn_steam_running"))
                self.append_log(mw_tr("log_steam_running"))

            socks_port = int(self._app_config.get("socks_port", DEFAULT_SOCKS_PORT))
            http_port = socks_port + 1
            self._pending_socks_addr = f"127.0.0.1:{socks_port}"
            self._pending_http_addr = f"127.0.0.1:{http_port}"

            try:
                self.proxy_worker = ProxyWorker(
                    selected_nics=selected,
                    listen_host="127.0.0.1",
                    listen_port=socks_port,
                    http_port=http_port,
                )
                self.proxy_worker.log_signal.connect(self.on_proxy_log)
                self.proxy_worker.traffic_signal.connect(self.on_proxy_traffic)
                self.proxy_worker.started_ok.connect(self.on_proxy_started)
                self.proxy_worker.error_signal.connect(self.on_proxy_error)
                self.proxy_worker.stopped.connect(self.on_proxy_stopped)

                self._is_boosting = True
                self._enter_boosting_ui()
                nic_names = ", ".join(n["name"] for n in selected)
                self.append_log(mw_tr(
                    "log_starting", socks=self._pending_socks_addr,
                    http=self._pending_http_addr, nics=nic_names,
                ))
                self.home_page.set_engine_state(True, socks_port, http_port)
                self.proxy_worker.start()
            except Exception as e:
                try:
                    set_system_proxy(False)
                except Exception as ce:
                    self.append_log(mw_tr("log_start_cleanup_error", error=ce))
                self.proxy_worker = None
                self._is_boosting = False
                self._exit_boosting_ui()
                self.append_log(mw_tr("log_start_exception", error=e))
                self.show_error(tr("error_start_failed", error=e))
                self._finish_engine_transition()

        def _stop_proxy(self):
            try:
                if self.proxy_worker is None:
                    self._is_boosting = False
                    self._exit_boosting_ui()
                    self._finish_engine_transition()
                    return
                self.append_log(mw_tr("log_stop_requested"))
                self.home_page.set_controls_enabled(False)
                set_system_proxy(False)
                self.append_log(mw_tr("log_proxy_disabled"))
                QTimer.singleShot(300, self._finish_stop_proxy)
            except Exception:
                self._finish_stop_proxy()

        def _finish_stop_proxy(self):
            if self.proxy_worker is None:
                return
            self.proxy_worker.stop()
            self._stop_fallback_timer.start(6000)

        def _enter_boosting_ui(self):
            self.home_page.set_controls_enabled(False)
            self.routing_page.set_controls_enabled(False)
            self.settings_page.set_controls_enabled(False)
            self.tools_page.set_controls_enabled(False)

        def _exit_boosting_ui(self):
            self.home_page.engine_switch.setEnabled(True)
            self.home_page.set_engine_state(False)
            self.home_page.set_controls_enabled(True)
            self.routing_page.set_controls_enabled(True)
            self.settings_page.set_controls_enabled(True)
            self.tools_page.set_controls_enabled(True)
            self.home_page.reset_telemetry()
            self._last_up_mbps = 0.0
            self._last_conn_count = 0

        # ========== ProxyWorker 信号 ==========
        @Slot(str)
        def on_proxy_log(self, message: str):
            self.append_log(message)

        @Slot(dict)
        def on_proxy_traffic(self, payload: dict):
            total = payload.get("_total", {})
            down = total.get("down_mbps", 0.0)
            up = total.get("up_mbps", 0.0)
            conn = total.get("connections", 0)
            self._last_up_mbps = up
            self._last_conn_count = conn
            self.home_page.update_total(down, up, conn)
            self.home_page.update_telemetry(payload)

        @Slot(str)
        def on_proxy_started(self, endpoint: str):
            try:
                set_system_proxy(True, self._pending_socks_addr, self._pending_http_addr)
                self.append_log(mw_tr(
                    "log_proxy_enabled", http=self._pending_http_addr,
                    socks=self._pending_socks_addr,
                ))
            except Exception as e:
                self.append_log(mw_tr("log_proxy_enable_failed", error=e))
                self.show_error(tr("error_proxy_write", error=e))
                if self.proxy_worker is not None:
                    self.proxy_worker.stop()
                self._finish_engine_transition()
                return
            self._is_boosting = True
            self.home_page.engine_switch.setEnabled(True)
            self._finish_engine_transition()
            self.show_success(mw_tr(
                "proxy_started_success", http=self._pending_http_addr,
                socks=self._pending_socks_addr,
            ))

        @Slot(str)
        def on_proxy_error(self, message: str):
            message = localize_runtime_message(message)
            self.append_log(mw_tr("log_error", message=message))
            try:
                set_system_proxy(False)
                self.append_log(mw_tr("log_start_failed_cleanup"))
            except Exception as e:
                self.append_log(mw_tr("log_start_cleanup_error", error=e))
            self.show_error(message)
            self._finish_engine_transition()

        @Slot(str)
        def on_proxy_stopped(self, message: str):
            message = localize_runtime_message(message)
            self._stop_fallback_timer.stop()
            self.append_log(mw_tr("log_stopped", message=message))
            try:
                set_system_proxy(False)
            except Exception as e:
                self.append_log(mw_tr("log_stop_cleanup_error", error=e))
            self._is_boosting = False
            self._exit_boosting_ui()
            self._finish_engine_transition()
            if self.proxy_worker is not None:
                if self.proxy_worker.isRunning():
                    self.proxy_worker.wait(3000)
                self.proxy_worker = None

        def _force_finish_stop_ui(self):
            if self.proxy_worker is None or not self._is_boosting:
                return
            worker = self.proxy_worker
            self.append_log(mw_tr("log_stop_fallback"))
            try:
                set_system_proxy(False)
            except Exception as e:
                self.append_log(mw_tr("log_stop_fallback_error", error=e))
            self._is_boosting = False
            self._exit_boosting_ui()
            self._finish_engine_transition()
            self.proxy_worker = None
            try:
                worker.stopped.disconnect(self.on_proxy_stopped)
            except Exception:
                pass
            self._retired_proxy_workers.append(worker)
            worker.finished.connect(lambda w=worker: self._cleanup_retired_proxy_worker(w))

        def _cleanup_retired_proxy_worker(self, worker):
            try:
                self._retired_proxy_workers.remove(worker)
            except ValueError:
                pass

        # ========== 网卡体检（第二阶段诊断）==========
        def on_diagnose_clicked(self):
            if self.diag_worker is not None and self.diag_worker.isRunning():
                return
            selected = self.get_selected_adapters()
            if not selected:
                self.show_warning(tr("diag_no_selection"))
                return
            self.tools_page.begin_running()
            self.append_log(mw_tr("log_starting", socks="-", http="-",
                                  nics=", ".join(n["name"] for n in selected))
                            if False else tr("diag_running",
                                             name=", ".join(n["name"] for n in selected)))
            self.diag_worker = DiagnosticWorker(selected, DEFAULT_TARGET_IP)
            self.diag_worker.result_ready.connect(self.on_diag_result)
            self.diag_worker.all_finished.connect(self.on_diag_finished)
            self.diag_worker.diag_error.connect(self.on_diag_error)
            self.diag_worker.start()

        @Slot(dict)
        def on_diag_result(self, result: dict):
            self.tools_page.add_result(result)
            # 同步首页对应卡片的健康徽标
            self.home_page.update_health(result.get("name", ""), result.get("status", "unavailable"))
            status = result.get("status", "unavailable")
            name = result.get("name", "")
            self.append_log(mw_tr(
                "log_diag_result",
                name=name,
                status=tr("diag_status_" + status),
                loss_label=tr("diag_metric_loss"),
                loss=result.get("loss_rate", 0),
                jitter_label=tr("diag_metric_jitter"),
                jitter=result.get("jitter_ms", 0),
            ))

        @Slot()
        def on_diag_finished(self):
            self.tools_page.end_running()
            if self.diag_worker is not None:
                if self.diag_worker.isRunning():
                    self.diag_worker.wait(2000)
                self.diag_worker = None

        @Slot(str)
        def on_diag_error(self, message: str):
            self.tools_page.end_running()
            self.show_error(localize_runtime_message(message))
            self.diag_worker = None

        # ========== 日志 ==========
        def append_log(self, message: str):
            APP_LOGGER.info(str(message))

        # ========== 退出清理 ==========
        def shutdown_backend_workers(self):
            if self._shutdown_started:
                return
            self._shutdown_started = True
            try:
                self._persist_config()
            except Exception:
                pass
            try:
                self._stop_fallback_timer.stop()
            except Exception:
                pass
            try:
                self._tun_pool_start_timer.stop()
            except Exception:
                pass
            try:
                if self._tun_active or self._tun_manager is not None:
                    self._stop_tun_mode()
                elif self._pool_worker is not None:
                    self._teardown_pool()

                if self.proxy_worker is not None:
                    self.proxy_worker.stop()
                    if self.proxy_worker.isRunning():
                        self.proxy_worker.wait(6000)
                    self.proxy_worker = None
                    self._is_boosting = False

                for worker in list(self._retired_proxy_workers):
                    try:
                        worker.stop()
                        if worker.isRunning():
                            worker.wait(3000)
                    except Exception:
                        pass
                self._retired_proxy_workers.clear()

                try:
                    set_system_proxy(False)
                except Exception as e:
                    print(mw_tr("log_close_proxy_error", error=e))

                if self.scan_worker.isRunning():
                    self.scan_worker.wait(3000)
                if self.diag_worker is not None and self.diag_worker.isRunning():
                    self.diag_worker.wait(3000)
            except Exception as e:
                print(mw_tr("log_close_cleanup_error", error=e))
            finally:
                try:
                    startupinfo = None
                    if hasattr(subprocess, "STARTUPINFO"):
                        startupinfo = subprocess.STARTUPINFO()
                        startupinfo.dwFlags |= subprocess.STARTF_USESHOWWINDOW
                        startupinfo.wShowWindow = 0
                    subprocess.run(
                        ["taskkill", "/F", "/IM", "sing-box.exe", "/T"],
                        capture_output=True,
                        timeout=5,
                        startupinfo=startupinfo,
                        creationflags=getattr(subprocess, "CREATE_NO_WINDOW", 0x08000000),
                    )
                    # 防御式清理：仅删除 HypoMux-Tun 的默认路由，避免影响其他 VPN。
                    subprocess.run(
                        [
                            "powershell", "-NoProfile", "-Command",
                            "Get-NetRoute -AddressFamily IPv4 -DestinationPrefix '0.0.0.0/0' "
                            "-ErrorAction SilentlyContinue | "
                            "Where-Object { $_.InterfaceAlias -eq 'HypoMux-Tun' } | "
                            "Remove-NetRoute -Confirm:$false -ErrorAction SilentlyContinue",
                        ],
                        capture_output=True,
                        timeout=5,
                        startupinfo=startupinfo,
                        creationflags=getattr(subprocess, "CREATE_NO_WINDOW", 0x08000000),
                    )
                except Exception:
                    pass

        def closeEvent(self, event):
            settings = QSettings("Hypostasis-Cat", "HypoMux")
            close_behavior = settings.value("close_behavior", "tray", type=str)

            # 关闭前持久化配置
            self._persist_config()

            if close_behavior == "tray" and not self._force_exit:
                event.ignore()
                self.hide()
                if not hasattr(self, "_tray_tip_shown"):
                    self.tray_icon.showMessage(
                        "HypoMux", tr("tray_tooltip"),
                        QSystemTrayIcon.Information, 2000
                    )
                    self._tray_tip_shown = True
                return

            try:
                self.shutdown_backend_workers()
            finally:
                if hasattr(self, "tray_icon"):
                    self.tray_icon.hide()
                event.accept()

        # ========== InfoBar 提示 ==========
        def show_info(self, message: str):
            self._InfoBar.info(
                title=tr("infobar_info"), content=message, orient=self._Qt.Horizontal,
                position=self._InfoBarPosition.TOP_RIGHT, duration=2000, parent=self
            )

        def show_success(self, message: str):
            self._InfoBar.success(
                title=tr("infobar_success"), content=message, orient=self._Qt.Horizontal,
                position=self._InfoBarPosition.TOP_RIGHT, duration=2200, parent=self
            )

        def show_warning(self, message: str):
            self._InfoBar.warning(
                title=tr("infobar_warning"), content=message, orient=self._Qt.Horizontal,
                position=self._InfoBarPosition.TOP_RIGHT, duration=2200, parent=self
            )

        def show_error(self, message: str):
            self._InfoBar.error(
                title=tr("infobar_error"), content=message, orient=self._Qt.Horizontal,
                position=self._InfoBarPosition.TOP_RIGHT, duration=3000, parent=self
            )

    return MainWindow()
