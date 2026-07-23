"""
HypoMux UI Components - SlidingStackedWidget

Windows 11 阻尼感水平滑动动画页面容器。
基于 QPropertyAnimation + QParallelAnimationGroup 实现丝滑过渡。
"""

from PySide6.QtWidgets import QStackedWidget
from PySide6.QtGui import QColor, QPainter
from qfluentwidgets import (
    ColorPickerButton, SimpleCardWidget, SwitchSettingCard, themeColor,
)
from qfluentwidgets.common.config import isDarkTheme
from qfluentwidgets.components.dialog_box.color_dialog import ColorDialog
from PySide6.QtCore import (
    QPropertyAnimation, QParallelAnimationGroup,
    QEasingCurve, QPoint, Qt, Property,
)


class SurfaceCardWidget(SimpleCardWidget):
    """用于承载复杂内容的静态 Fluent 卡片。

    ``ElevatedCardWidget`` 在悬停时会将 ``QGraphicsDropShadowEffect``
    挂到整个卡片。Qt 会把所有子控件一起作为阴影源，在深色半透明背景上
    便可能把文字、图标和输入框渲染成块状光晕。这里保留 Fluent 的卡片
    颜色、边框和 8px 圆角；悬停时仅平滑提亮卡片自身背景，不使用复合
    阴影或位移动画。
    """

    def __init__(self, parent=None):
        super().__init__(parent)
        self.setBorderRadius(8)
        self._hover_border_opacity = 0.0
        self._hover_border_ani = QPropertyAnimation(self, b"hoverBorderOpacity", self)
        self._hover_border_ani.setDuration(160)
        self._hover_border_ani.setEasingCurve(QEasingCurve.OutCubic)

    def _hoverBackgroundColor(self):
        """提供可见但克制的悬停反馈，且只重绘卡片本身。"""
        return QColor(255, 255, 255, 21 if isDarkTheme() else 224)

    def _pressedBackgroundColor(self):
        """按下时回落，避免和可点击子控件抢夺视觉焦点。"""
        return QColor(255, 255, 255, 13 if isDarkTheme() else 190)

    def _get_hover_border_opacity(self):
        return self._hover_border_opacity

    def _set_hover_border_opacity(self, opacity):
        self._hover_border_opacity = max(0.0, min(float(opacity), 1.0))
        self.update()

    hoverBorderOpacity = Property(
        float, _get_hover_border_opacity, _set_hover_border_opacity
    )

    def _animate_hover_border(self, target):
        self._hover_border_ani.stop()
        self._hover_border_ani.setStartValue(self._hover_border_opacity)
        self._hover_border_ani.setEndValue(target)
        self._hover_border_ani.start()

    def enterEvent(self, event):
        super().enterEvent(event)
        self._animate_hover_border(1.0)

    def leaveEvent(self, event):
        super().leaveEvent(event)
        self._animate_hover_border(0.0)

    def paintEvent(self, event):
        super().paintEvent(event)
        if self._hover_border_opacity <= 0:
            return

        accent = themeColor()
        alpha = round((88 if isDarkTheme() else 108) * self._hover_border_opacity)
        painter = QPainter(self)
        painter.setRenderHint(QPainter.Antialiasing)
        painter.setBrush(Qt.NoBrush)
        painter.setPen(QColor(accent.red(), accent.green(), accent.blue(), alpha))
        painter.drawRoundedRect(
            self.rect().adjusted(1, 1, -1, -1), self.borderRadius, self.borderRadius
        )


class LocalizedColorPickerButton(ColorPickerButton):
    """项目内的 Fluent 选色按钮，为 ColorDialog 补齐应用语言。"""

    def __init__(self, color, title, parent=None, enableAlpha=False):
        super().__init__(color, title, parent, enableAlpha)
        # ColorPickerButton 默认把私有槽连接到 clicked；替换为本地化版本，
        # 仍使用原生 ColorDialog 和原有的确认后才写入逻辑。
        self.clicked.disconnect()
        self.clicked.connect(self.open_color_dialog)

    def open_color_dialog(self):
        """打开本地化后的 Fluent 选色对话框。"""
        from ui.i18n import tr

        dialog = ColorDialog(
            self.color, tr("settings_theme_color_dialog_title"),
            self.window(), self.enableAlpha,
        )
        dialog.yesButton.setText(tr("settings_theme_color_confirm"))
        dialog.cancelButton.setText(tr("settings_theme_color_cancel"))
        dialog.editLabel.setText(tr("settings_theme_color_edit"))
        dialog.redLabel.setText(tr("settings_theme_color_red"))
        dialog.greenLabel.setText(tr("settings_theme_color_green"))
        dialog.blueLabel.setText(tr("settings_theme_color_blue"))
        dialog.opacityLabel.setText(tr("settings_theme_color_opacity"))
        dialog.colorChanged.connect(self._on_dialog_color_changed)
        dialog.exec()

    def _on_dialog_color_changed(self, color):
        self.setColor(color)
        self.colorChanged.emit(color)


class LocalizedSwitchSettingCard(SwitchSettingCard):
    """保持原生 Fluent 开关外观，并让状态文字跟随应用语言。"""

    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self.refresh_switch_text()

    def setValue(self, isChecked: bool):
        super().setValue(isChecked)
        self.refresh_switch_text()

    def refresh_switch_text(self):
        from ui.i18n import tr

        self.switchButton.setText(
            tr("settings_switch_on")
            if self.switchButton.isChecked()
            else tr("settings_switch_off")
        )

class SlidingStackedWidget(QStackedWidget):
    """带水平滑动动画的 QStackedWidget 替代品。

    特性:
    - 350ms OutCubic 缓动曲线，模拟 Win11 阻尼感
    - 动画期间状态锁，防止高频点击导致界面卡死
    - 不销毁/重构任何子页面，仅操作 pos 属性
    """

    def __init__(self, parent=None):
        super().__init__(parent)
        self._is_animating = False
        self._duration = 350
        self._easing = QEasingCurve.OutCubic
        self._direction = Qt.Horizontal
        self._anim_group = None

    def slide_to_index(self, index: int):
        """滑动切换到指定页面索引。

        如果目标就是当前页，或动画正在播放中，则直接忽略。
        """
        if self._is_animating:
            return
        if index == self.currentIndex():
            return
        if index < 0 or index >= self.count():
            return

        self._is_animating = True

        # 确定滑动方向：目标在右侧则当前页左移，反之右移
        width = self.frameRect().width()
        current_widget = self.currentWidget()
        next_widget = self.widget(index)

        if index > self.currentIndex():
            # 向左滑：下一页从右侧进入
            offset_current = QPoint(-width, 0)
            offset_next = QPoint(width, 0)
        else:
            # 向右滑：下一页从左侧进入
            offset_current = QPoint(width, 0)
            offset_next = QPoint(-width, 0)

        # 将目标页预设为容器满尺寸，强制完成首次布局计算（消除初次塌陷闪烁）
        current_pos = current_widget.pos()
        height = self.frameRect().height()
        next_widget.setGeometry(0, 0, width, height)
        next_widget.ensurePolished()
        if next_widget.layout():
            next_widget.layout().activate()

        # 移到起始偏移位置并显示
        next_widget.move(current_pos + offset_next)
        next_widget.show()
        next_widget.raise_()

        # 当前页滑出动画
        anim_current = QPropertyAnimation(current_widget, b"pos")
        anim_current.setDuration(self._duration)
        anim_current.setEasingCurve(self._easing)
        anim_current.setStartValue(current_pos)
        anim_current.setEndValue(current_pos + offset_current)

        # 目标页滑入动画
        anim_next = QPropertyAnimation(next_widget, b"pos")
        anim_next.setDuration(self._duration)
        anim_next.setEasingCurve(self._easing)
        anim_next.setStartValue(current_pos + offset_next)
        anim_next.setEndValue(current_pos)

        # 并行执行
        self._anim_group = QParallelAnimationGroup(self)
        self._anim_group.addAnimation(anim_current)
        self._anim_group.addAnimation(anim_next)

        # 动画结束后收尾
        target_index = index

        def _on_finished():
            self.setCurrentIndex(target_index)
            current_widget.move(current_pos)  # 归位，防止后续布局错乱
            self._is_animating = False
            self._anim_group = None

        self._anim_group.finished.connect(_on_finished)
        self._anim_group.start()
