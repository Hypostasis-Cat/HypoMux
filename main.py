"""
NetBooster - Windows 多网卡跃点数并发调度工具
主应用入口

严格的启动生命周期：
1. 检测管理员权限（纯逻辑，无 Qt 依赖）
2. 创建 QApplication（最首要的 Qt 对象）
3. 延迟导入 MainWindow（此时 QApplication 已就位）
4. 初始化界面并运行事件循环
"""

import sys
import os
import ctypes

# 仅导入非 Qt 模块 - 严禁在此处导入任何 UI 相关模块
from utils.network_utils import is_admin, elevate_privileges


def check_admin_privileges():
    """
    检测管理员权限（纯逻辑函数，不创建任何 Qt 对象）
    
    Returns:
        bool: True 表示已有管理员权限，False 表示需要提权
    """
    if is_admin():
        print("[INFO] 程序已以管理员身份运行")
        return True
    else:
        print("[INFO] 程序无管理员权限")
        return False


def show_privilege_prompt_and_elevate(app):
    """
    在 QApplication 创建后显示提权对话框
    
    Args:
        app: 已创建的 QApplication 实例
        
    Returns:
        bool: 用户确认提权返回 True，否则返回 False
    """
    # 延迟导入 QMessageBox - 此时 QApplication 已存在
    from PySide6.QtWidgets import QMessageBox
    
    print("[INFO] 程序需要管理员权限，正在请求 UAC 提权...")
    
    reply = QMessageBox.information(
        None,
        "需要管理员权限",
        "NetBooster 需要管理员权限来修改网卡配置。\n\n点击「确定」将触发 Windows UAC 提权请求。",
        QMessageBox.Ok | QMessageBox.Cancel
    )
    
    if reply == QMessageBox.Ok:
        if elevate_privileges():
            print("[INFO] 已发起 UAC 提权请求，原进程退出")
            return False
        else:
            QMessageBox.critical(
                None,
                "提权失败",
                "无法请求管理员权限。NetBooster 需要管理员权限运行。\n\n请以管理员身份运行此程序。"
            )
            return False
    else:
        print("[INFO] 用户取消了 UAC 提权请求")
        return False


def register_windows_app_id():
    """为 Windows 注册独立的 AppUserModelID，避免任务栏图标被系统合并。"""
    try:
        ctypes.windll.shell32.SetCurrentProcessExplicitAppUserModelID(
            "netbooster.dynamicmetric.accelerator.v1"
        )
    except Exception:
        pass


if __name__ == "__main__":
    # ========== 第一步：检测管理员权限（纯逻辑，无 Qt） ==========
    admin_check = check_admin_privileges()
    
    # ========== 第二步：立刻创建 QApplication（在任何 QWidget 之前）==========
    register_windows_app_id()
    from PySide6.QtCore import QCoreApplication
    from PySide6.QtGui import QIcon
    from PySide6.QtWidgets import QApplication, QMessageBox
    runtime_dir = os.path.dirname(os.path.abspath(__file__))
    qt_plugin_path = os.path.join(runtime_dir, "PySide6", "qt-plugins")
    icon_path = os.path.join(runtime_dir, "assets", "icon.ico")
    if os.path.exists(qt_plugin_path):
        QCoreApplication.addLibraryPath(qt_plugin_path)
        os.environ["QT_QPA_PLATFORM_PLUGIN_PATH"] = os.path.join(qt_plugin_path, "platforms")
    app = QApplication(sys.argv)
    if os.path.exists(icon_path):
        app.setWindowIcon(QIcon(icon_path))
    
    # ========== 第三步：延迟导入 MainWindow（现在 QApplication 已存在）==========
    from ui.main_window import create_main_window
    
    # ========== 第四步：若无管理员权限，显示提权对话框 ==========
    if not admin_check:
        if not show_privilege_prompt_and_elevate(app):
            sys.exit(1)
    
    # ========== 第五步：配置应用属性 ==========
    app.setApplicationName("NetBooster")
    app.setApplicationVersion("1.0.0")
    app.setStyle("Fusion")
    
    # ========== 第六步：创建并显示主窗口（工厂函数模式）==========
    try:
        window = create_main_window()
        if os.path.exists(icon_path):
            window.setWindowIcon(QIcon(icon_path))
        window.show()
        print("[INFO] 主界面已启动")
    except Exception as e:
        print(f"[ERROR] 创建主窗口失败: {e}")
        QMessageBox.critical(None, "启动失败", f"无法创建主窗口: {e}")
        sys.exit(1)
    
    # ========== 第七步：运行应用事件循环 ==========
    sys.exit(app.exec())
