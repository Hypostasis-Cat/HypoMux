"""HypoMux 弹窗的 Windows 云母材质与主题同步。"""

from __future__ import annotations

import sys

from PySide6.QtCore import QSettings, QTimer, Qt
from PySide6.QtGui import QColor


def apply_mica_popup(dialog, surface=None, enabled: bool | None = None):
    """按用户设置为项目内弹窗应用云母材质。"""
    if enabled is None:
        enabled = QSettings("Hypostasis-Cat", "HypoMux").value(
            "mica_enabled", True, type=bool
        )
    dialog._hypomux_popup_surface = surface or getattr(dialog, "widget", dialog)
    dialog._hypomux_popup_material = bool(enabled)
    dialog.setAttribute(Qt.WA_TranslucentBackground, bool(enabled))
    refresh_mica_popup(dialog)

    # 对尚未 show/exec 的对话框，延迟到原生窗口句柄创建后再应用云母。
    QTimer.singleShot(0, lambda: refresh_mica_popup(dialog))
    return dialog


def refresh_mica_popup(dialog):
    """按当前 qfluentwidgets 主题重绘弹窗，并尽力启用 Windows 云母。"""
    if dialog is None or not hasattr(dialog, "_hypomux_popup_material"):
        return

    try:
        from qfluentwidgets.common.config import isDarkTheme

        dark = bool(isDarkTheme())
        surface = getattr(dialog, "_hypomux_popup_surface", dialog)
        material_enabled = bool(getattr(dialog, "_hypomux_popup_material", False))
        if surface is dialog:
            background = "transparent" if material_enabled else (
                "rgb(32, 32, 32)" if dark else "rgb(248, 248, 248)"
            )
            dialog.setStyleSheet(f"QDialog {{ background: {background}; }}")
        else:
            tint = (
                "rgba(32, 32, 32, 218)" if dark else "rgba(248, 248, 248, 218)"
            ) if material_enabled else (
                "rgb(32, 32, 32)" if dark else "rgb(248, 248, 248)"
            )
            border = "rgba(255, 255, 255, 24)" if dark else "rgba(0, 0, 0, 18)"
            surface.setStyleSheet(
                f"QFrame#centerWidget {{ background: {tint}; border: 1px solid {border}; border-radius: 8px; }}"
            )

        if hasattr(dialog, "setMaskColor"):
            dialog.setMaskColor(QColor(0, 0, 0, 112 if dark else 72))
    except Exception:
        return

    if not material_enabled:
        return

    # MessageBoxBase 是嵌入父窗口的遮罩层，而非独立原生窗口。对它调用
    # 原生窗口材质 API 会破坏遮罩的鼠标事件与层级，表现为“选择进程”
    # 窗口假死。该类弹窗保留上面的云母色调中心表面即可。
    if surface is not dialog:
        return

    if sys.platform != "win32":
        return

    try:
        from qframelesswindow import WindowEffect

        effect = getattr(dialog, "_hypomux_window_effect", None)
        if effect is None:
            effect = WindowEffect(dialog)
            dialog._hypomux_window_effect = effect
        effect.setMicaEffect(dialog.winId(), dark)
    except Exception:
        pass
