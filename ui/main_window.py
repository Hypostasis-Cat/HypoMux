"""
HypoMux 主窗口界面 - v2.0 (QStackedWidget 翻页架构)
使用 QFluentWidgets 实现 Windows 11 Fluent Design 风格

关键特性：所有 Qt 和 qfluentwidgets 导入都延迟到 MainWindow 初始化时
确保 QApplication 已存在，避免 "Must construct a QApplication before a QWidget" 错误

【Phase 4A 重构】
接入系统级 HTTP/HTTPS/SOCKS 注册表锁，由 ProxyWorker 驱动双协议无感加速。
- 「一键加速」在双端口监听成功后接管 Windows 系统代理；
- 停止、窗口关闭、异常收尾均强制关闭系统代理；
- 实时网速、各网卡连接数、调度日志全部由 ProxyWorker 的
  traffic_signal / log_signal 驱动，点亮分流监控大屏。
"""

import ctypes
from typing import List, Dict
import winreg

from utils.network_utils import scan_network_adapters
from proxy_worker import ProxyWorker


DEFAULT_SOCKS_PORT = 10800
DEFAULT_HTTP_PORT = 10801


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
        winreg.HKEY_CURRENT_USER,
        key_path,
        0,
        winreg.KEY_WRITE,
    ) as key:
        winreg.SetValueEx(key, "ProxyEnable", 0, winreg.REG_DWORD, 1 if enable else 0)
        if enable:
            proxy_value = f"http={http_addr};https={http_addr};socks={socks_addr}"
            winreg.SetValueEx(key, "ProxyServer", 0, winreg.REG_SZ, proxy_value)
        else:
            winreg.SetValueEx(key, "ProxyServer", 0, winreg.REG_SZ, "")

    ctypes.windll.Wininet.InternetSetOptionW(0, 39, 0, 0)
    ctypes.windll.Wininet.InternetSetOptionW(0, 37, 0, 0)


def create_main_window():
    """工厂函数：创建 MainWindow 实例（此时 QApplication 已存在）"""
    # 延迟导入所有 Qt 和 qfluentwidgets
    from PySide6.QtWidgets import (
        QMainWindow, QWidget, QVBoxLayout, QHBoxLayout, QLabel,
        QTableWidget, QTableWidgetItem, QHeaderView, QSpinBox, QCheckBox,
        QFrame, QToolButton, QPlainTextEdit,
        QAbstractItemView, QSystemTrayIcon, QMenu,
    )
    from PySide6.QtCore import Qt, QThread, Signal, Slot, QRectF, QTimer, QSettings
    from PySide6.QtGui import QFont, QIcon, QPainterPath, QRegion, QAction
    from qfluentwidgets import (
        PushButton, PrimaryPushButton, InfoBar, InfoBarPosition,
        setThemeColor,
    )
    setThemeColor("#0078d4")

    from ui.settings_page import SettingsWidget
    from ui.i18n import tr
    from ui.components import SlidingStackedWidget

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

    # ========== 后台扫描线程 ==========
    class ScanWorker(QThread):
        """后台网卡扫描工作线程。"""
        scan_finished = Signal(bool, list, str)

        def run(self):
            try:
                success, adapters, error_msg = scan_network_adapters()
                self.scan_finished.emit(success, adapters, error_msg)
            except Exception as e:
                print(mw_tr("log_scan_thread_error", error=e))
                self.scan_finished.emit(False, [], str(e))

    # ========== 网卡表格组件 ==========

    class NetworkAdapterTableWidget(QTableWidget):
        """网卡列表表格组件。

        列：选择 / 网卡别名 / IPv4 地址 / 实时速度 / 实时连接数
        其中「实时速度」和「实时连接数」由 ProxyWorker 的 traffic_signal 实时回填。
        """

        # 列索引常量，避免魔法数字散落
        COL_CHECK = 0
        COL_ALIAS = 1
        COL_IPV4 = 2
        COL_SPEED = 3
        COL_CONN = 4

        def __init__(self):
            super().__init__()
            # 行 -> 网卡完整信息（index/alias/ipv4 原始值）
            self.adapter_rows: List[Dict] = []
            self.init_ui()

        def init_ui(self):
            self.setColumnCount(5)
            self.retranslate_ui()
            self.verticalHeader().setVisible(False)
            self.verticalHeader().setDefaultSectionSize(50)

            header = self.horizontalHeader()
            header.setSectionResizeMode(self.COL_CHECK, QHeaderView.ResizeToContents)
            header.setSectionResizeMode(self.COL_ALIAS, QHeaderView.Stretch)
            header.setSectionResizeMode(self.COL_IPV4, QHeaderView.Stretch)
            header.setSectionResizeMode(self.COL_SPEED, QHeaderView.ResizeToContents)
            header.setSectionResizeMode(self.COL_CONN, QHeaderView.ResizeToContents)

            self.setShowGrid(False)
            self.setAlternatingRowColors(False)
            self.setSelectionBehavior(QTableWidget.SelectRows)
            self.setSelectionMode(QAbstractItemView.NoSelection)
            self.setFocusPolicy(Qt.NoFocus)
            self.viewport().setFocusPolicy(Qt.NoFocus)
            self.setStyleSheet("""
                QTableWidget {
                    background: #f8fafc;
                    border: 1px solid rgba(0, 78, 140, 0.08);
                    border-radius: 8px;
                    padding: 4px;
                    gridline-color: rgba(0, 78, 140, 0.05);
                }
                QTableWidget::viewport {
                    background: #f8fafc;
                }
                QTableWidget::item {
                    padding: 12px 8px;
                    color: #243447;
                    border-bottom: 1px solid rgba(0, 78, 140, 0.06);
                }
                QTableWidget::item:hover {
                    background: rgba(0, 120, 212, 0.055);
                    border-radius: 4px;
                }
                QTableWidget::item:selected {
                    background: transparent;
                    color: #243447;
                    font-weight: 500;
                    border-radius: 4px;
                }
                QTableWidget::item:focus {
                    background: transparent;
                    color: #243447;
                    border-bottom: 1px solid rgba(0, 78, 140, 0.06);
                    outline: none;
                }
                QHeaderView::section {
                    background: #eef4fa;
                    color: #526579;
                    padding: 10px;
                    border: none;
                    border-bottom: 1px solid rgba(0, 78, 140, 0.10);
                    font-weight: bold;
                    font-size: 13px;
                }
                QScrollBar:vertical {
                    background: transparent;
                    width: 10px;
                    margin: 8px 2px 8px 2px;
                }
                QScrollBar::handle:vertical {
                    background: rgba(148, 163, 184, 120);
                    border-radius: 5px;
                    min-height: 24px;
                }
                QScrollBar::handle:vertical:hover {
                    background: rgba(100, 116, 139, 170);
                }
                QScrollBar::add-line:vertical, QScrollBar::sub-line:vertical {
                    height: 0;
                }
                QCheckBox {
                    spacing: 8px;
                    color: #334155;
                    padding-left: 6px;
                    background: transparent;
                }
                QCheckBox::indicator {
                    width: 18px;
                    height: 18px;
                    border-radius: 5px;
                    border: 1px solid #94a3b8;
                    background: white;
                }
                QCheckBox::indicator:hover {
                    border: 1px solid #3b82f6;
                    background: #f8fbff;
                }
                QCheckBox::indicator:checked {
                    border: 1px solid #0078d4;
                    background: #0078d4;
                    image: none;
                }
                QCheckBox::indicator:checked:hover {
                    border: 1px solid #0066b3;
                    background: #0066b3;
                }
            """)
            self.setSortingEnabled(False)
            self.setEditTriggers(QTableWidget.NoEditTriggers)

        def retranslate_ui(self):
            self.setHorizontalHeaderLabels([
                mw_tr("col_select"),
                mw_tr("col_alias"),
                mw_tr("col_ipv4"),
                mw_tr("col_speed"),
                mw_tr("col_conn"),
            ])

        def clear_table(self):
            self.setRowCount(0)
            self.adapter_rows = []

        @staticmethod
        def _first_valid_ipv4(raw) -> str:
            """从扫描结果里提取第一个有效的 IPv4 地址。

            扫描返回的 ipv4 可能是：
            - 字符串 "192.168.1.10"
            - 逗号分隔的多 IP 字符串 "192.168.1.10, 10.0.0.5"
            - 字符串列表 ["192.168.1.10", "10.0.0.5"]
            统一取第一个非空、形如 a.b.c.d 的地址。
            """
            candidates: List[str] = []
            if isinstance(raw, list):
                for item in raw:
                    candidates.extend(str(item).split(","))
            else:
                candidates.extend(str(raw).split(","))

            for cand in candidates:
                ip = cand.strip()
                # 简单校验：4 段、全部为数字
                parts = ip.split(".")
                if len(parts) == 4 and all(p.isdigit() for p in parts):
                    return ip
            return ""

        def add_adapter_row(self, adapter_info: Dict):
            row = self.rowCount()
            self.insertRow(row)

            ipv4_display = adapter_info['ipv4']
            if isinstance(ipv4_display, list):
                ipv4_display = ', '.join(str(x) for x in ipv4_display)

            # 记录该行的结构化信息，供 get_selected_adapters / 实时数据回填使用
            self.adapter_rows.append({
                'index': adapter_info['index'],
                'alias': adapter_info['alias'],
                'name': adapter_info['alias'],
                'ipv4_raw': adapter_info['ipv4'],
                'ip': self._first_valid_ipv4(adapter_info['ipv4']),
            })

            checkbox = QCheckBox()
            checkbox.setStyleSheet("""
                QCheckBox {
                    spacing: 8px;
                    color: #334155;
                    padding-left: 6px;
                    background: transparent;
                }
                QCheckBox::indicator {
                    width: 18px;
                    height: 18px;
                    border-radius: 5px;
                    border: 1px solid #94a3b8;
                    background: white;
                }
                QCheckBox::indicator:hover {
                    border: 1px solid #3b82f6;
                    background: #f8fbff;
                }
                QCheckBox::indicator:checked {
                    border: 1px solid #0078d4;
                    background: #0078d4;
                    image: none;
                }
                QCheckBox::indicator:checked:hover {
                    border: 1px solid #0066b3;
                    background: #0066b3;
                }
            """)
            self.setCellWidget(row, self.COL_CHECK, checkbox)

            alias_item = QTableWidgetItem(adapter_info['alias'])
            alias_item.setFlags(alias_item.flags() & ~Qt.ItemIsEditable & ~Qt.ItemIsSelectable)
            self.setItem(row, self.COL_ALIAS, alias_item)

            ipv4_item = QTableWidgetItem(str(ipv4_display))
            ipv4_item.setFlags(ipv4_item.flags() & ~Qt.ItemIsEditable & ~Qt.ItemIsSelectable)
            self.setItem(row, self.COL_IPV4, ipv4_item)

            speed_item = QTableWidgetItem("0.00")
            speed_item.setFlags(speed_item.flags() & ~Qt.ItemIsEditable & ~Qt.ItemIsSelectable)
            speed_item.setTextAlignment(Qt.AlignCenter)
            self.setItem(row, self.COL_SPEED, speed_item)

            conn_item = QTableWidgetItem("—")
            conn_item.setFlags(conn_item.flags() & ~Qt.ItemIsEditable & ~Qt.ItemIsSelectable)
            conn_item.setTextAlignment(Qt.AlignCenter)
            self.setItem(row, self.COL_CONN, conn_item)
            self.setCurrentCell(-1, -1)

        def get_selected_adapters(self) -> List[Dict]:
            """返回被勾选且拥有有效 IPv4 的网卡，供 ProxyWorker 使用。

            每项形如 {'index': int, 'name': str, 'ip': str}，
            其中 name 为网卡别名（与 psutil.net_io_counters(pernic=True) 的键一致），
            ip 为第一个有效 IPv4（已处理逗号分隔的多 IP 情况）。
            没有有效 IPv4 的网卡会被跳过，因为物理绑定必须有真实出口 IP。
            """
            selected = []
            for row in range(self.rowCount()):
                checkbox = self.cellWidget(row, self.COL_CHECK)
                if checkbox and checkbox.isChecked():
                    info = self.adapter_rows[row]
                    if not info['ip']:
                        continue
                    selected.append({
                        'index': info['index'],
                        'name': info['alias'],
                        'ip': info['ip'],
                    })
            return selected

        def update_traffic(self, payload: Dict):
            """根据 traffic_signal 快照回填每张网卡的实时速度与连接数。

            payload 以网卡别名为键（含 '_total' 汇总项）。
            """
            for row, info in enumerate(self.adapter_rows):
                stats = payload.get(info['alias'])
                speed_item = self.item(row, self.COL_SPEED)
                conn_item = self.item(row, self.COL_CONN)
                if stats is None:
                    if speed_item is not None:
                        speed_item.setText("0.00")
                    if conn_item is not None:
                        conn_item.setText("—")
                else:
                    if speed_item is not None:
                        speed_item.setText(f"{stats.get('down_mbps', 0.0):.2f}")
                    if conn_item is not None:
                        conn_item.setText(str(stats.get('connections', 0)))

        def reset_traffic(self):
            """停止加速后，把实时速度和连接数列清零显示。"""
            for row in range(self.rowCount()):
                speed_item = self.item(row, self.COL_SPEED)
                conn_item = self.item(row, self.COL_CONN)
                if speed_item is not None:
                    speed_item.setText("0.00")
                if conn_item is not None:
                    conn_item.setText("—")

        def select_all(self):
            for row in range(self.rowCount()):
                checkbox = self.cellWidget(row, self.COL_CHECK)
                if checkbox:
                    checkbox.setChecked(True)

        def deselect_all(self):
            for row in range(self.rowCount()):
                checkbox = self.cellWidget(row, self.COL_CHECK)
                if checkbox:
                    checkbox.setChecked(False)

        def set_checkboxes_enabled(self, enabled: bool):
            """加速运行期间禁止改动网卡勾选，避免调度集合中途变化。"""
            for row in range(self.rowCount()):
                checkbox = self.cellWidget(row, self.COL_CHECK)
                if checkbox:
                    checkbox.setEnabled(enabled)

    # ========== 主窗口 ==========
    class MainWindow(QMainWindow):
        """HypoMux 主窗口"""
        def __init__(self):
            set_system_proxy(False)
            super().__init__()
            self.setWindowTitle(mw_tr("window_title"))
            self.setWindowIcon(QIcon())
            self.setFixedSize(1280, 820)
            self.setAttribute(Qt.WA_TranslucentBackground, True)
            self.setWindowFlags(Qt.FramelessWindowHint | Qt.Window)
            self._drag_pos = None

            # 扫描线程
            self.scan_worker = ScanWorker()
            # 当前运行中的代理引擎（None 表示未启动）
            self.proxy_worker = None
            self._is_boosting = False
            self._pending_socks_addr = ""
            self._pending_http_addr = ""
            self._status_key = "status_loading"
            self._status_kwargs = {}
            self._retired_proxy_workers = []
            self._stop_fallback_timer = QTimer(self)
            self._stop_fallback_timer.setSingleShot(True)
            self._stop_fallback_timer.timeout.connect(self._force_finish_stop_ui)

            # 系统托盘初始化
            self._init_system_tray()

            self.connect_worker_signals()
            self.init_ui()
            self.load_adapters()

        def connect_worker_signals(self):
            self.scan_worker.scan_finished.connect(self.on_scan_finished)

        def _init_system_tray(self):
            """初始化系统托盘图标和右键菜单"""
            import os
            self.tray_icon = QSystemTrayIcon(self)

            # 加载托盘图标（优先项目 icon，回退到默认）
            icon_path = os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))), "assets", "icon.ico")
            if os.path.exists(icon_path):
                self.tray_icon.setIcon(QIcon(icon_path))
            else:
                # 回退到应用窗口图标
                self.tray_icon.setIcon(self.windowIcon())

            self.tray_icon.setToolTip(mw_tr("tray_tooltip"))

            # 创建托盘右键菜单
            tray_menu = QMenu()
            tray_menu.setStyleSheet("""
                QMenu {
                    background: white;
                    border: 1px solid #d0d0d0;
                    border-radius: 6px;
                    padding: 4px;
                }
                QMenu::item {
                    padding: 8px 24px 8px 12px;
                    border-radius: 4px;
                    color: #1a1a1a;
                }
                QMenu::item:selected {
                    background: #e6f3ff;
                    color: #0078d4;
                }
            """)

            # 显示主界面
            show_action = QAction(mw_tr("tray_show_main"), self)
            show_action.triggered.connect(self._show_from_tray)
            tray_menu.addAction(show_action)

            tray_menu.addSeparator()

            # 退出程序
            exit_action = QAction(mw_tr("tray_exit"), self)
            exit_action.triggered.connect(self._exit_from_tray)
            tray_menu.addAction(exit_action)

            self.tray_icon.setContextMenu(tray_menu)
            # 双击托盘图标也显示主界面
            self.tray_icon.activated.connect(self._on_tray_activated)

            # 显示托盘图标
            self.tray_icon.show()

        def _on_tray_activated(self, reason):
            """托盘图标被激活（双击）"""
            if reason == QSystemTrayIcon.DoubleClick:
                self._show_from_tray()

        def _show_from_tray(self):
            """从系统托盘恢复显示主窗口"""
            self.show()
            self.activateWindow()
            self.raise_()

        def _exit_from_tray(self):
            """从系统托盘彻底退出程序（绕过托盘逻辑）"""
            # 临时设置为直接退出，避免 closeEvent 再次隐藏窗口
            settings = QSettings("Hypostasis-Cat", "HypoMux")
            original_behavior = settings.value("close_behavior", "tray", type=str)
            settings.setValue("close_behavior", "exit")

            # 执行关闭（这次会走彻底退出逻辑）
            self.close()

            # 恢复原设置（虽然进程即将退出，但保持状态一致性）
            settings.setValue("close_behavior", original_behavior)

        def init_ui(self):
            self.setStyleSheet("background: transparent;")

            central_widget = QWidget()
            central_widget.setStyleSheet("background: transparent;")
            self.setCentralWidget(central_widget)

            root_layout = QVBoxLayout(central_widget)
            root_layout.setContentsMargins(0, 0, 0, 0)
            root_layout.setSpacing(0)

            shell = QFrame()
            shell.setObjectName("shellCard")
            shell_layout = QVBoxLayout(shell)
            shell_layout.setContentsMargins(20, 20, 20, 20)
            shell_layout.setSpacing(14)

            shell.setStyleSheet("""
                QFrame#shellCard {
                    background: #f7fbff;
                    border: 1px solid rgba(0, 0, 0, 0.08);
                    border-radius: 12px;
                }
                QLabel#pageTitle {
                    color: #1a1a1a;
                    font-weight: 700;
                }
                QLabel#pageSubtitle {
                    color: #5f5f5f;
                }
                QLabel#statusBadge {
                    background: #e7f2fd;
                    color: #0066b3;
                    border: 1px solid rgba(0, 102, 179, 0.14);
                    border-radius: 4px;
                    padding: 6px 12px;
                    font-weight: 600;
                }
                QLineEdit, QSpinBox, QComboBox {
                    background: #ffffff;
                    border: 1px solid rgba(0, 0, 0, 0.10);
                    border-radius: 8px;
                    padding: 8px 10px;
                    color: #1a1a1a;
                }
                QSpinBox {
                    padding-right: 10px;
                }
                QSpinBox::up-button, QSpinBox::down-button {
                    width: 0;
                    height: 0;
                    border: none;
                    background: transparent;
                }
                QSpinBox::up-arrow, QSpinBox::down-arrow {
                    width: 0;
                    height: 0;
                    image: none;
                }
                QCheckBox {
                    spacing: 8px;
                    color: #2d2d2d;
                }
            """)

            # ---------- 标题栏 ----------
            title_bar = QHBoxLayout()
            title_bar.setContentsMargins(0, 0, 0, 0)
            title_bar.setSpacing(8)

            title_box = QVBoxLayout()
            title_box.setSpacing(4)

            self.refresh_btn = QToolButton()
            self.refresh_btn.setText("↻")
            self.refresh_btn.setFixedSize(36, 32)
            self.refresh_btn.setStyleSheet(self._tool_btn_style())
            self.refresh_btn.clicked.connect(self.load_adapters)

            self.min_btn = QToolButton()
            self.min_btn.setText("—")
            self.min_btn.setFixedSize(36, 32)
            self.min_btn.setStyleSheet(self._tool_btn_style())
            self.min_btn.clicked.connect(self.showMinimized)

            self.close_btn = QToolButton()
            self.close_btn.setText("×")
            self.close_btn.setFixedSize(36, 32)
            self.close_btn.setStyleSheet(self._tool_btn_style(danger=True))
            self.close_btn.clicked.connect(self.close)

            self.settings_btn = QToolButton()
            self.settings_btn.setText(mw_tr("settings_btn"))
            self.settings_btn.setFixedHeight(32)
            self.settings_btn.setMinimumWidth(52)
            self.settings_btn.setStyleSheet("""
                QToolButton {
                    background: rgba(255, 255, 255, 220);
                    color: #0078d4;
                    border: 1px solid rgba(0, 120, 212, 0.18);
                    border-radius: 10px;
                    font-size: 13px;
                    font-weight: 600;
                }
                QToolButton:hover {
                    background: rgba(239, 246, 255, 240);
                    border: 1px solid rgba(0, 120, 212, 0.35);
                }
            """)
            self.settings_btn.clicked.connect(self._open_settings)

            title_label = QLabel("HypoMux")
            title_label.setObjectName("pageTitle")
            title_font = QFont()
            title_font.setPointSize(22)
            title_font.setBold(True)
            title_label.setFont(title_font)

            self.subtitle_label = QLabel("")
            self.subtitle_label.setObjectName("pageSubtitle")
            subtitle_font = QFont()
            subtitle_font.setPointSize(10)
            self.subtitle_label.setFont(subtitle_font)

            title_box.addWidget(title_label)
            title_box.addWidget(self.subtitle_label)

            self.status_label = QLabel(mw_tr("status_loading"))
            self.status_label.setObjectName("statusBadge")
            self.status_label.setAlignment(Qt.AlignCenter)

            title_bar.addLayout(title_box)
            title_bar.addStretch()
            title_bar.addWidget(self.status_label)
            title_bar.addWidget(self.settings_btn)
            title_bar.addWidget(self.refresh_btn)
            title_bar.addWidget(self.min_btn)
            title_bar.addWidget(self.close_btn)
            shell_layout.addLayout(title_bar)

            # ---------- 数据大屏：合并下行总速度 ----------
            dashboard = QFrame()
            dashboard.setStyleSheet("""
                QFrame {
                    background: #f8fafc;
                    border: 1px solid rgba(0, 78, 140, 0.08);
                    border-radius: 6px;
                }
            """)
            dash_layout = QHBoxLayout(dashboard)
            dash_layout.setContentsMargins(20, 14, 20, 14)
            dash_layout.setSpacing(24)

            self.speed_value_label = QLabel("0.00")
            speed_font = QFont()
            speed_font.setPointSize(30)
            speed_font.setBold(True)
            self.speed_value_label.setFont(speed_font)
            self.speed_value_label.setStyleSheet("color: #0066b3;")

            self.speed_caption = QLabel("")
            self.speed_caption.setStyleSheet("color: #616161; font-weight: 600;")

            speed_box = QVBoxLayout()
            speed_box.setSpacing(2)
            speed_box.addWidget(self.speed_value_label)
            speed_box.addWidget(self.speed_caption)

            self.up_value_label = QLabel(mw_tr("up_format", value=0.0))
            self.up_value_label.setStyleSheet("color: #616161; font-weight: 600;")
            self.conn_value_label = QLabel(mw_tr("conn_format", value=0))
            self.conn_value_label.setStyleSheet("color: #616161; font-weight: 600;")

            meta_box = QVBoxLayout()
            meta_box.setSpacing(6)
            meta_box.addStretch()
            meta_box.addWidget(self.up_value_label)
            meta_box.addWidget(self.conn_value_label)
            meta_box.addStretch()

            dash_layout.addLayout(speed_box)
            dash_layout.addStretch()
            dash_layout.addLayout(meta_box)
            shell_layout.addWidget(dashboard)

            # ---------- 网卡表格 ----------
            self.table_widget = NetworkAdapterTableWidget()
            shell_layout.addWidget(self.table_widget, stretch=3)


            # ---------- 实时控制台 ----------
            self.console_caption = QLabel("")
            self.console_caption.setStyleSheet("color: #526579; font-weight: 600;")
            shell_layout.addWidget(self.console_caption)

            self.console = QPlainTextEdit()
            self.console.setReadOnly(True)
            self.console.setMaximumBlockCount(500)  # 限制行数，避免长时间运行内存膨胀
            self.console.setStyleSheet("""
                QPlainTextEdit {
                    background: #f8fafc;
                    color: #334155;
                    border: 1px solid rgba(0, 78, 140, 0.08);
                    border-radius: 6px;
                    padding: 10px;
                    font-family: 'Consolas', 'Segoe UI Mono', monospace;
                    font-size: 12px;
                }
                QPlainTextEdit QScrollBar:vertical {
                    background: transparent;
                    width: 10px;
                    margin: 8px 3px 8px 0;
                    border: none;
                }
                QPlainTextEdit QScrollBar::handle:vertical {
                    background: rgba(100, 116, 139, 0.38);
                    border-radius: 5px;
                    min-height: 28px;
                }
                QPlainTextEdit QScrollBar::handle:vertical:hover {
                    background: rgba(71, 85, 105, 0.55);
                }
                QPlainTextEdit QScrollBar::add-line:vertical,
                QPlainTextEdit QScrollBar::sub-line:vertical {
                    height: 0;
                    width: 0;
                    border: none;
                    background: transparent;
                }
                QPlainTextEdit QScrollBar::add-page:vertical,
                QPlainTextEdit QScrollBar::sub-page:vertical {
                    background: transparent;
                }
                QPlainTextEdit QScrollBar:horizontal {
                    background: transparent;
                    height: 10px;
                    margin: 0 8px 3px 8px;
                    border: none;
                }
                QPlainTextEdit QScrollBar::handle:horizontal {
                    background: rgba(100, 116, 139, 0.32);
                    border-radius: 5px;
                    min-width: 28px;
                }
                QPlainTextEdit QScrollBar::add-line:horizontal,
                QPlainTextEdit QScrollBar::sub-line:horizontal {
                    width: 0;
                    height: 0;
                    border: none;
                    background: transparent;
                }
                QPlainTextEdit QScrollBar::add-page:horizontal,
                QPlainTextEdit QScrollBar::sub-page:horizontal {
                    background: transparent;
                }
            """)
            shell_layout.addWidget(self.console, stretch=2)

            # ---------- 操作栏 ----------
            action_layout = QHBoxLayout()
            action_layout.setSpacing(12)

            self.select_all_btn = PushButton(mw_tr("select_all"))
            self.select_all_btn.setMinimumHeight(42)
            self.select_all_btn.setMaximumWidth(100)
            self.select_all_btn.clicked.connect(self.on_select_all_clicked)

            self.deselect_all_btn = PushButton(mw_tr("deselect_all"))
            self.deselect_all_btn.setMinimumHeight(42)
            self.deselect_all_btn.setMaximumWidth(100)
            self.deselect_all_btn.clicked.connect(self.on_deselect_all_clicked)

            self.port_label = QLabel("")
            self.port_label.setStyleSheet("color: #526579; font-weight: 600;")
            self.port_spinbox = QSpinBox()
            self.port_spinbox.setMinimum(1)
            self.port_spinbox.setMaximum(65534)
            self.port_spinbox.setValue(DEFAULT_SOCKS_PORT)
            self.port_spinbox.setMinimumWidth(110)
            self.port_spinbox.setButtonSymbols(QSpinBox.NoButtons)

            self.boost_btn = PrimaryPushButton(mw_tr("boost_start"))
            self.boost_btn.setMinimumHeight(42)
            self.boost_btn.setMinimumWidth(118)
            self.boost_btn.setMaximumWidth(140)
            self.boost_btn.clicked.connect(self.on_boost_clicked)
            self.boost_btn.setEnabled(False)
            self._apply_boost_button_style(active=False)

            action_layout.addWidget(self.select_all_btn)
            action_layout.addWidget(self.deselect_all_btn)
            action_layout.addStretch()
            action_layout.addWidget(self.port_label)
            action_layout.addWidget(self.port_spinbox)
            action_layout.addWidget(self.boost_btn)

            shell_layout.addLayout(action_layout)

            # ========== QStackedWidget 翻页容器 ==========
            self.stacked_widget = SlidingStackedWidget()
            self.stacked_widget.addWidget(shell)  # Index 0: 主大屏

            # ---------- Index 1: 设置面板（独立模块） ----------
            self.settings_widget = SettingsWidget()
            self.settings_widget.back_clicked.connect(self._back_to_dashboard)
            self.settings_widget.info_message.connect(self.show_info)
            self.settings_widget.success_message.connect(self.show_success)
            self.settings_widget.warning_message.connect(self.show_warning)
            self.settings_widget.ports_changed.connect(self._on_settings_ports_changed)
            self.settings_widget.language_changed.connect(self._on_language_changed)
            self.stacked_widget.addWidget(self.settings_widget)  # Index 1

            self.stacked_widget.setCurrentIndex(0)
            root_layout.addWidget(self.stacked_widget)

            self._InfoBar = InfoBar
            self._InfoBarPosition = InfoBarPosition
            self._Qt = Qt
            self.retranslate_ui()

        # ---------- 样式辅助 ----------
        def _apply_boost_button_style(self, active: bool = False):
            """Keep the boost button visually consistent across start/stop states."""
            if active:
                normal_bg = "#d13438"
                hover_bg = "#c42b1c"
                pressed_bg = "#a4262c"
                border = "#b3262d"
            else:
                normal_bg = "#0078d4"
                hover_bg = "#106ebe"
                pressed_bg = "#005a9e"
                border = "#006cbe"

            self.boost_btn.setStyleSheet(f"""
                QPushButton {{
                    background: {normal_bg};
                    color: white;
                    border: 1px solid {border};
                    border-radius: 8px;
                    padding: 0 18px;
                    font-size: 13px;
                    font-weight: 600;
                }}
                QPushButton:hover {{
                    background: {hover_bg};
                    border: 1px solid {hover_bg};
                }}
                QPushButton:pressed {{
                    background: {pressed_bg};
                    border: 1px solid {pressed_bg};
                    padding-top: 1px;
                }}
                QPushButton:disabled {{
                    background: #eef2f7;
                    color: #94a3b8;
                    border: 1px solid #d7dee8;
                }}
            """)

        def _tool_btn_style(self, danger: bool = False) -> str:
            hover = (
                "background: rgba(254, 242, 242, 240);"
                "border: 1px solid rgba(248, 113, 113, 220);"
                "color: #b91c1c;"
                if danger else
                "background: rgba(239, 246, 255, 240);"
                "border: 1px solid rgba(191, 219, 254, 240);"
            )
            return f"""
                QToolButton {{
                    background: rgba(255, 255, 255, 220);
                    color: #334155;
                    border: 1px solid rgba(226, 232, 240, 220);
                    border-radius: 10px;
                    font-size: 18px;
                    font-weight: 600;
                }}
                QToolButton:hover {{ {hover} }}
            """

        # ---------- 设置面板交互逻辑 ----------
        def _open_settings(self):
            """切换到设置页面"""
            self.stacked_widget.slide_to_index(1)

        def _back_to_dashboard(self):
            """切回主大屏"""
            self.stacked_widget.slide_to_index(0)

        def _on_settings_ports_changed(self, socks_port: int, http_port: int):
            """设置面板端口变更后同步到主界面操作栏"""
            self.port_spinbox.setValue(socks_port)

        def _on_language_changed(self, lang_code: str):
            settings = QSettings("Hypostasis-Cat", "HypoMux")
            settings.setValue("ui/language", lang_code)
            settings.setValue("language", lang_code)
            settings.sync()
            self.retranslate_ui()

        def _set_status(self, key: str, **kwargs):
            self._status_key = key
            self._status_kwargs = dict(kwargs)
            self.status_label.setText(mw_tr(key, **kwargs))

        def retranslate_ui(self):
            self.setWindowTitle(mw_tr("window_title"))
            self.subtitle_label.setText(mw_tr("subtitle"))
            self.settings_btn.setText(mw_tr("settings_btn"))
            self.table_widget.retranslate_ui()
            self.speed_caption.setText(mw_tr("speed_caption"))
            self.console_caption.setText(mw_tr("console_caption"))
            self.select_all_btn.setText(mw_tr("select_all"))
            self.deselect_all_btn.setText(mw_tr("deselect_all"))
            self.port_label.setText(mw_tr("port_label"))
            self.boost_btn.setText(mw_tr("boost_stop" if self._is_boosting else "boost_start"))
            self.up_value_label.setText(mw_tr("up_format", value=getattr(self, "_last_up_mbps", 0.0)))
            self.conn_value_label.setText(mw_tr("conn_format", value=getattr(self, "_last_conn_count", 0)))
            self.status_label.setText(mw_tr(self._status_key, **self._status_kwargs))
            if hasattr(self, "settings_widget"):
                self.settings_widget.retranslate_ui()

        def mousePressEvent(self, event):
            if event.button() == Qt.LeftButton:
                self._drag_pos = event.globalPosition().toPoint() - self.frameGeometry().topLeft()
                event.accept()
            else:
                super().mousePressEvent(event)

        def mouseMoveEvent(self, event):
            if event.buttons() & Qt.LeftButton and self._drag_pos is not None:
                self.move(event.globalPosition().toPoint() - self._drag_pos)
                event.accept()
            else:
                super().mouseMoveEvent(event)

        def mouseReleaseEvent(self, event):
            self._drag_pos = None
            super().mouseReleaseEvent(event)

        def resizeEvent(self, event):
            super().resizeEvent(event)
            path = QPainterPath()
            path.addRoundedRect(QRectF(self.rect()), 12, 12)
            self.setMask(QRegion(path.toFillPolygon().toPolygon()))

        # ========== 控制台日志 ==========
        def append_log(self, message: str):
            self.console.appendPlainText(message)

        # ========== 网卡扫描 ==========
        def load_adapters(self):
            # 加速运行期间禁止刷新网卡，避免抽掉正在分流的网卡
            if self._is_boosting:
                self.show_warning(mw_tr("warn_boosting_refresh"))
                return

            self.table_widget.clear_table()
            self._set_status("status_loading")
            self.boost_btn.setEnabled(False)
            self.refresh_btn.setEnabled(False)

            if self.scan_worker.isRunning():
                self.scan_worker.wait()

            self.scan_worker.start()

        @Slot(bool, list, str)
        def on_scan_finished(self, success: bool, adapters: list, error_msg: str):
            if success:
                if not adapters:
                    self._set_status("status_no_adapters")
                    self.show_warning(mw_tr("warn_no_adapters"))
                else:
                    for adapter in adapters:
                        self.table_widget.add_adapter_row(adapter)
                    self._set_status("status_loaded", count=len(adapters))
                    self.boost_btn.setEnabled(True)
                self.refresh_btn.setEnabled(True)
            else:
                self._set_status("status_load_failed")
                self.refresh_btn.setEnabled(True)
                self.show_error(mw_tr("error_load_adapters", error=error_msg))

        def on_select_all_clicked(self):
            self.table_widget.select_all()

        def on_deselect_all_clicked(self):
            self.table_widget.deselect_all()

        # ========== 一键加速 / 停止（ProxyWorker 启停） ==========
        def on_boost_clicked(self):
            if self._is_boosting:
                self._stop_proxy()
            else:
                self._start_proxy()

        def _start_proxy(self):
            selected = self.table_widget.get_selected_adapters()
            if not selected:
                self.show_warning(mw_tr("warn_no_selection"))
                return
            if self.proxy_worker is not None:
                return

            if get_steam_pids():
                self.show_warning(mw_tr("warn_steam_running"))
                self.append_log(mw_tr("log_steam_running"))

            socks_port = self.port_spinbox.value()
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
                nic_names = ", ".join(n['name'] for n in selected)
                self.append_log(mw_tr(
                    "log_starting",
                    socks=self._pending_socks_addr,
                    http=self._pending_http_addr,
                    nics=nic_names,
                ))
                self._set_status("status_starting")
                self.proxy_worker.start()
            except Exception as e:
                try:
                    set_system_proxy(False)
                except Exception as cleanup_error:
                    self.append_log(mw_tr("log_start_cleanup_error", error=cleanup_error))
                self.proxy_worker = None
                self._is_boosting = False
                self._exit_boosting_ui()
                self.append_log(mw_tr("log_start_exception", error=e))
                self.show_error(mw_tr("error_start_failed", error=e))

        def _stop_proxy(self):
            try:
                if self.proxy_worker is None:
                    self._is_boosting = False
                    self._exit_boosting_ui()
                    return
                self.append_log(mw_tr("log_stop_requested"))
                self._set_status("status_stopping")
                self.boost_btn.setEnabled(False)  # 停止完成（on_proxy_stopped）后再恢复

                # ===== 退出生命周期：严格按序，根治"停止加速后 Steam 离线"=====
                # 步骤 1+2：先把注册表 ProxyEnable 置 0，并立即调用 WinINet
                #          InternetSetOption(SETTINGS_CHANGED + REFRESH) 强刷全局网络栈缓存。
                #          set_system_proxy(False) 内部已完成这两步。
                set_system_proxy(False)
                self.append_log(mw_tr("log_proxy_disabled"))

                # 步骤 3：短暂保留本地监听，给 Steam 的重连与 WinINet 刷新留出窗口。
                # 步骤 4：延时结束后再彻底关闭本地 Socket 监听、释放端口。
                QTimer.singleShot(300, self._finish_stop_proxy)
            except Exception:
                # 兜底：即便上面异常，也要确保监听被叫停
                self._finish_stop_proxy()

        def _finish_stop_proxy(self):
            """退出生命周期步骤 4：延时结束后安全关闭本地 Socket 监听服务。"""
            if self.proxy_worker is None:
                return
            # ProxyWorker.stop() 内部用 loop.call_soon_threadsafe 安全叫停子线程的 asyncio loop
            self.proxy_worker.stop()
            self._stop_fallback_timer.start(6000)

        def _enter_boosting_ui(self):
            self.boost_btn.setText(mw_tr("boost_stop"))
            self._apply_boost_button_style(active=True)
            self.select_all_btn.setEnabled(False)
            self.deselect_all_btn.setEnabled(False)
            self.refresh_btn.setEnabled(False)
            self.port_spinbox.setEnabled(False)
            self.table_widget.set_checkboxes_enabled(False)

        def _exit_boosting_ui(self):
            self.boost_btn.setText(mw_tr("boost_start"))
            self._apply_boost_button_style(active=False)
            self.boost_btn.setEnabled(True)
            self.select_all_btn.setEnabled(True)
            self.deselect_all_btn.setEnabled(True)
            self.refresh_btn.setEnabled(True)
            self.port_spinbox.setEnabled(True)
            self.table_widget.set_checkboxes_enabled(True)
            self.table_widget.reset_traffic()
            self.speed_value_label.setText("0.00")
            self._last_up_mbps = 0.0
            self._last_conn_count = 0
            self.up_value_label.setText(mw_tr("up_format", value=0.0))
            self.conn_value_label.setText(mw_tr("conn_format", value=0))

        @Slot(str)
        def on_proxy_log(self, message: str):
            self.append_log(message)

        @Slot(dict)
        def on_proxy_traffic(self, payload: dict):
            # 点亮数据大屏：合并下行总速度 + 上行 + 总连接数
            total = payload.get("_total", {})
            down = total.get('down_mbps', 0.0)
            up = total.get('up_mbps', 0.0)
            conn = total.get('connections', 0)
            self._last_up_mbps = up
            self._last_conn_count = conn
            self.speed_value_label.setText(f"{down:.2f}")
            self.up_value_label.setText(mw_tr("up_format", value=up))
            self.conn_value_label.setText(mw_tr("conn_format", value=conn))
            self._set_status("status_running_live", down=down, conn=conn)
            # 各网卡实时速度和连接数回填到表格
            self.table_widget.update_traffic(payload)

        @Slot(str)
        def on_proxy_started(self, endpoint: str):
            try:
                set_system_proxy(True, self._pending_socks_addr, self._pending_http_addr)
                self.append_log(mw_tr(
                    "log_proxy_enabled",
                    http=self._pending_http_addr,
                    socks=self._pending_socks_addr,
                ))
            except Exception as e:
                self.append_log(mw_tr("log_proxy_enable_failed", error=e))
                self.show_error(mw_tr("error_proxy_write", error=e))
                if self.proxy_worker is not None:
                    self.proxy_worker.stop()
                return

            self._is_boosting = True
            self.boost_btn.setEnabled(True)
            self._set_status("status_running", endpoint=endpoint)
            self.show_success(mw_tr(
                "proxy_started_success",
                http=self._pending_http_addr,
                socks=self._pending_socks_addr,
            ))

        @Slot(str)
        def on_proxy_error(self, message: str):
            self.append_log(mw_tr("log_error", message=message))
            self._set_status("status_start_failed")
            try:
                set_system_proxy(False)
                self.append_log(mw_tr("log_start_failed_cleanup"))
            except Exception as e:
                self.append_log(mw_tr("log_start_cleanup_error", error=e))
            self.show_error(message)
            # 启动失败后内核会走 stopped 流程收尾，这里只提示

        @Slot(str)
        def on_proxy_stopped(self, message: str):
            self._stop_fallback_timer.stop()
            self.append_log(mw_tr("log_stopped", message=message))
            try:
                set_system_proxy(False)
            except Exception as e:
                self.append_log(mw_tr("log_stop_cleanup_error", error=e))
            self._is_boosting = False
            self._set_status("status_stopped")
            self._exit_boosting_ui()
            # 等待子线程完全结束并释放引用，便于下次重新启动
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
            self._set_status("status_stopped")
            self._exit_boosting_ui()
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

        def closeEvent(self, event):
            """退出清理：根据用户设置决定最小化到托盘或彻底退出。"""
            settings = QSettings("Hypostasis-Cat", "HypoMux")
            close_behavior = settings.value("close_behavior", "tray", type=str)

            # 如果设置为最小化到托盘，则隐藏窗口而不真正退出
            if close_behavior == "tray":
                event.ignore()  # 阻止默认关闭行为
                self.hide()     # 隐藏主窗口
                # 可选：首次最小化时提示用户
                if not hasattr(self, '_tray_tip_shown'):
                    self.tray_icon.showMessage(
                        "HypoMux",
                        mw_tr("tray_tooltip"),
                        QSystemTrayIcon.Information,
                        2000
                    )
                    self._tray_tip_shown = True
                return

            # 否则执行彻底退出
            try:
                if self.proxy_worker is not None:
                    self.proxy_worker.stop()
                    if self.proxy_worker.isRunning():
                        self.proxy_worker.wait(3000)
                    self.proxy_worker = None
                if self.scan_worker.isRunning():
                    self.scan_worker.wait(3000)
            except Exception as e:
                print(mw_tr("log_close_cleanup_error", error=e))
            finally:
                try:
                    set_system_proxy(False)
                except Exception as e:
                    print(mw_tr("log_close_proxy_error", error=e))
                # 清理系统托盘图标
                if hasattr(self, 'tray_icon'):
                    self.tray_icon.hide()
            super().closeEvent(event)

        # ========== InfoBar 提示 ==========
        def show_info(self, message: str):
            self._InfoBar.info(
                title=mw_tr("infobar_info"), content=message, orient=self._Qt.Horizontal,
                position=self._InfoBarPosition.TOP_RIGHT, duration=2000, parent=self
            )

        def show_success(self, message: str):
            self._InfoBar.success(
                title=mw_tr("infobar_success"), content=message, orient=self._Qt.Horizontal,
                position=self._InfoBarPosition.TOP_RIGHT, duration=2200, parent=self
            )

        def show_warning(self, message: str):
            self._InfoBar.warning(
                title=mw_tr("infobar_warning"), content=message, orient=self._Qt.Horizontal,
                position=self._InfoBarPosition.TOP_RIGHT, duration=2200, parent=self
            )

        def show_error(self, message: str):
            self._InfoBar.error(
                title=mw_tr("infobar_error"), content=message, orient=self._Qt.Horizontal,
                position=self._InfoBarPosition.TOP_RIGHT, duration=3000, parent=self
            )

    return MainWindow()
