"""
HypoMux - Windows 多网卡跃点数并发调度工具
主应用入口 (v2.1.0)

启动生命周期：
1. 在任何 UI 导入前纠偏工作目录
2. 创建 QApplication
3. 建立 QLocalServer 单实例锁
4. 检测管理员权限
5. 创建主窗口并接入唤醒信号
6. 运行事件循环
"""

import os
import sys
import ctypes
import subprocess


SINGLE_INSTANCE_KEY = "HypoMux_Single_Instance_Lock"
WAKE_MESSAGE = b"WAKE_UP"


def normalize_working_directory() -> str:
    """把工作目录锁定到程序自身目录，规避计划任务从 System32 拉起。"""
    if getattr(sys, "frozen", False) or ("__compiled__" in globals()):
        base_dir = os.path.dirname(os.path.abspath(sys.executable))
    else:
        base_dir = os.path.dirname(os.path.abspath(__file__))
    try:
        os.chdir(base_dir)
    except Exception:
        pass
    return base_dir


RUNTIME_DIR = normalize_working_directory()


def _startupinfo_no_window():
    """返回隐藏 Windows 子进程窗口的 STARTUPINFO。"""
    startupinfo = None
    if hasattr(subprocess, "STARTUPINFO"):
        startupinfo = subprocess.STARTUPINFO()
        startupinfo.dwFlags |= subprocess.STARTF_USESHOWWINDOW
        startupinfo.wShowWindow = 0
    return startupinfo


def _run_silent_command(command: list[str], timeout: int = 5):
    """静默执行启动前清理命令，任何异常都不阻断主程序启动。"""
    try:
        subprocess.run(
            command,
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
            timeout=timeout,
            startupinfo=_startupinfo_no_window(),
            creationflags=getattr(subprocess, "CREATE_NO_WINDOW", 0x08000000),
        )
    except Exception:
        pass


def force_evict_zombie_backends():
    """启动前清理上轮崩溃遗留的 sing-box 后端进程、TUN 虚拟适配器及残留路由。

    清理顺序：进程 → 适配器 → 路由。
    """
    _run_silent_command(["taskkill", "/F", "/IM", "sing-box.exe", "/T"], timeout=3)
    _run_silent_command([
        "powershell", "-NoProfile", "-Command",
        "$targets = @(Get-PnpDevice -Class Net -ErrorAction SilentlyContinue | "
        "Where-Object { $_.FriendlyName -eq 'HypoMux-Tun' -and $_.InstanceId -like '*WINTUN*' }); "
        "if ($targets.Count -gt 0) { "
        "$targets | Disable-PnpDevice -Confirm:$false -ErrorAction SilentlyContinue; "
        "Start-Sleep -Milliseconds 800; "
        "foreach ($d in $targets) { pnputil /remove-device $d.InstanceId 2>&1 | Out-Null } }",
    ], timeout=10)
    # 仅删除 HypoMux-Tun 的默认路由，绝不按固定网关误删其他 VPN 的路由。
    _run_silent_command(
        [
            "powershell", "-NoProfile", "-Command",
            "Get-NetRoute -AddressFamily IPv4 -DestinationPrefix '0.0.0.0/0' "
            "-ErrorAction SilentlyContinue | "
            "Where-Object { $_.InterfaceAlias -eq 'HypoMux-Tun' } | "
            "Remove-NetRoute -Confirm:$false -ErrorAction SilentlyContinue",
        ],
        timeout=5,
    )


from utils.network_utils import is_admin


def check_admin_privileges():
    """检测管理员权限。"""
    if is_admin():
        print("[INFO] 程序已以管理员身份运行")
        return True
    print("[INFO] 程序无管理员权限")
    return False


def register_windows_app_id():
    """为 Windows 注册独立的 AppUserModelID，避免任务栏图标被系统合并。"""
    try:
        ctypes.windll.shell32.SetCurrentProcessExplicitAppUserModelID(
            "hypostasiscat.hypomux.accelerator.v2"
        )
    except Exception:
        pass


def try_wake_existing_instance(QLocalSocket):
    """若已有实例运行，则发送唤醒消息并退出当前进程。"""
    socket = QLocalSocket()
    socket.connectToServer(SINGLE_INSTANCE_KEY)
    if socket.waitForConnected(200):
        socket.write(WAKE_MESSAGE)
        socket.flush()
        socket.waitForBytesWritten(500)
        socket.disconnectFromServer()
        return True
    return False


def create_single_instance_server(QLocalServer):
    """创建单实例本地服务；遇到陈旧锁时先清理再监听。"""
    server = QLocalServer()
    if not server.listen(SINGLE_INSTANCE_KEY):
        QLocalServer.removeServer(SINGLE_INSTANCE_KEY)
        if not server.listen(SINGLE_INSTANCE_KEY):
            return None
    return server


if __name__ == "__main__":
    register_windows_app_id()

    from PySide6.QtCore import QCoreApplication, QSettings, QTranslator, Qt
    from PySide6.QtGui import QIcon
    from PySide6.QtNetwork import QLocalServer, QLocalSocket
    from PySide6.QtWidgets import QApplication, QMessageBox

    qt_plugin_path = os.path.join(RUNTIME_DIR, "PySide6", "qt-plugins")
    icon_path = os.path.join(RUNTIME_DIR, "assets", "icon.ico")

    if os.path.exists(qt_plugin_path):
        QCoreApplication.addLibraryPath(qt_plugin_path)
        os.environ["QT_QPA_PLATFORM_PLUGIN_PATH"] = os.path.join(qt_plugin_path, "platforms")

    app = QApplication(sys.argv)
    app.setApplicationName("HypoMux")
    app.setApplicationVersion("2.1.0")
    app.setStyle("Fusion")

    if os.path.exists(icon_path):
        app.setWindowIcon(QIcon(icon_path))

    if try_wake_existing_instance(QLocalSocket):
        sys.exit(0)

    force_evict_zombie_backends()

    local_server = create_single_instance_server(QLocalServer)

    admin_check = check_admin_privileges()
    silent_mode = "--silent" in sys.argv[1:]
    if silent_mode:
        print("[INFO] 检测到 --silent 参数，进入静默托盘启动模式")

    settings = QSettings("Hypostasis-Cat", "HypoMux")
    language = settings.value("language", "zh")

    translator = QTranslator()
    if language == "en":
        i18n_path = os.path.join(RUNTIME_DIR, "i18n", "hypomux_en.qm")
        if os.path.exists(i18n_path):
            translator.load(i18n_path)
            app.installTranslator(translator)
            print("[INFO] 已加载英文语言包")
        else:
            print("[INFO] 英文语言包文件不存在，使用内置英文回退")

    from ui.main_window import create_main_window

    if not admin_check:
        is_compiled = getattr(sys, "frozen", False) or ("__compiled__" in globals())
        if is_compiled:
            print("[WARN] 权限拦截：打包程序未获得管理员权限")
            QMessageBox.critical(
                None,
                "需要管理员权限",
                "HypoMux 需要管理员权限来修改网卡配置与跃点数。\n\n请右键选择「以管理员身份运行」本程序。",
            )
            sys.exit(1)
        from utils.network_utils import elevate_privileges
        print("[INFO] 源码运行环境：正在请求本地 UAC 提权重启...")
        reply = QMessageBox.information(
            None,
            "需要管理员权限 (源码调试)",
            "当前正在以源码模式运行，HypoMux 需要请求管理员权限来继续调试。\n\n点击「确定」将尝试触发本地提权拉起。",
            QMessageBox.Ok | QMessageBox.Cancel,
        )
        if reply == QMessageBox.Ok:
            elevate_privileges()
        sys.exit(1)

    window = None

    try:
        window = create_main_window()
        if os.path.exists(icon_path):
            window.setWindowIcon(QIcon(icon_path))

        def wake_main_window():
            window.apply_standard_geometry()
            window.show()
            window.setWindowState(window.windowState() & ~Qt.WindowMinimized | Qt.WindowActive)
            window.raise_()
            window.activateWindow()

        def on_single_instance_message():
            if local_server is None:
                return
            while local_server.hasPendingConnections():
                client = local_server.nextPendingConnection()
                try:
                    if client.waitForReadyRead(500):
                        data = bytes(client.readAll())
                        if data == WAKE_MESSAGE:
                            wake_main_window()
                except Exception as ce:
                    print(f"[WARN] 跨进程单实例通信异常: {ce}")
                finally:
                    client.disconnectFromServer()
                    client.deleteLater()

        if local_server is not None:
            local_server.newConnection.connect(on_single_instance_message)

        def global_about_to_quit_cleanup():
            try:
                if local_server is not None:
                    local_server.close()
            except Exception as se:
                print(f"[WARN] 单实例监听关闭异常: {se}")
            try:
                if window is not None and hasattr(window, "shutdown_backend_workers"):
                    window.shutdown_backend_workers()
            except Exception as we:
                print(f"[WARN] 后台托管清理异常: {we}")

        app.aboutToQuit.connect(global_about_to_quit_cleanup)

        if silent_mode:
            print("[INFO] 静默模式已启动，主界面已最小化至系统托盘")
        else:
            wake_main_window()
            print("[INFO] 主界面已启动")
    except Exception as e:
        print(f"[ERROR] 创建主窗口失败: {e}")
        QMessageBox.critical(None, "启动失败", f"无法创建主窗口: {e}")
        sys.exit(1)

    sys.exit(app.exec())
