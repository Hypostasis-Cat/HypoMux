"""
HypoMux 关于页 (AboutPage) - 第四阶段任务3

承载从设置页迁出的版本信息、项目介绍，以及大厂级赞助模块：
两张并排 CardWidget 分别渲染微信(support/wei.png) 与 支付宝(support/zhi.jpg) 收款码。

纯视图层，无任何后端依赖。全程使用 qfluentwidgets 原生组件，
深浅色主题自动适配；高亮文字用 themeColor() 着色并响应主题切换。
"""

import os

from PySide6.QtCore import Qt, QTimer
from PySide6.QtGui import QPixmap
from PySide6.QtWidgets import (
    QWidget, QVBoxLayout, QHBoxLayout, QGridLayout, QFrame,
)
from qfluentwidgets import (
    CardWidget, ElevatedCardWidget, TitleLabel, SubtitleLabel, StrongBodyLabel, BodyLabel,
    HyperlinkButton, IconWidget, ImageLabel, SingleDirectionScrollArea,
    themeColor,
)

from ui.i18n import tr
from ui.pages import resolve_icon

REPO_URL = "https://github.com/Hypostasis-Cat/HypoMux"
QR_MAX_WIDTH = 180


def _project_root() -> str:
    """返回项目根目录（ui/pages/ 的上两级）。"""
    return os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))


class PaymentCard(ElevatedCardWidget):
    """单个收款码卡片：标题 + 高画质缩放二维码。"""

    def __init__(self, title: str, rel_path: str, parent=None):
        super().__init__(parent)
        layout = QVBoxLayout(self)
        layout.setContentsMargins(20, 20, 20, 20)
        layout.setSpacing(14)
        layout.setAlignment(Qt.AlignHCenter)

        self._title_label = StrongBodyLabel(title, self)
        self._title_label.setAlignment(Qt.AlignHCenter)
        layout.addWidget(self._title_label, 0, Qt.AlignHCenter)

        self._image_label = ImageLabel(self)
        self._image_label.setAlignment(Qt.AlignCenter)
        abs_path = os.path.join(_project_root(), rel_path)
        if os.path.exists(abs_path):
            pixmap = QPixmap(abs_path)
            if not pixmap.isNull():
                # 高画质缩放，限制最大宽度 180px
                scaled = pixmap.scaledToWidth(
                    QR_MAX_WIDTH, Qt.SmoothTransformation
                )
                self._image_label.setPixmap(scaled)
                self._image_label.setFixedSize(scaled.size())
            else:
                self._image_label.setText(tr("about_qr_missing"))
        else:
            self._image_label.setText(tr("about_qr_missing"))
        layout.addWidget(self._image_label, 0, Qt.AlignHCenter)

    def retranslate_ui(self, title: str):
        self._title_label.setText(title)


class AboutPage(QWidget):
    """关于页：项目信息 + 赞助收款码。"""

    def __init__(self, parent=None):
        super().__init__(parent)
        self.setObjectName("aboutPage")
        self._init_ui()

    def _init_ui(self):
        scroll = SingleDirectionScrollArea(self, orient=Qt.Vertical)
        scroll.setWidgetResizable(True)
        scroll.setFrameShape(QFrame.NoFrame)
        scroll.setStyleSheet("background: transparent;")

        container = QWidget()
        container.setStyleSheet("background: transparent;")
        root = QVBoxLayout(container)
        root.setContentsMargins(32, 24, 32, 24)
        root.setSpacing(14)

        # 顶部干净大号加粗标题
        self._page_title = TitleLabel(tr("nav_about"), container)
        root.addWidget(self._page_title)
        root.addSpacing(4)

        self._top_grid = QGridLayout()
        self._top_grid.setHorizontalSpacing(14)
        self._top_grid.setVerticalSpacing(14)

        # ===== 项目信息卡 =====
        info_card = CardWidget(container)
        info_layout = QVBoxLayout(info_card)
        info_layout.setContentsMargins(24, 20, 24, 20)
        info_layout.setSpacing(10)

        name_row = QHBoxLayout()
        name_row.setSpacing(12)
        self._app_icon = IconWidget(resolve_icon("CERTIFICATE", "APPLICATION"), info_card)
        self._app_icon.setFixedSize(28, 28)
        self._app_name = SubtitleLabel("HypoMux", info_card)
        name_row.addWidget(self._app_icon)
        name_row.addWidget(self._app_name)
        name_row.addStretch()
        info_layout.addLayout(name_row)

        self._version_label = BodyLabel(tr("settings_version"), info_card)
        info_layout.addWidget(self._version_label)

        self._intro_label = BodyLabel(tr("about_intro"), info_card)
        self._intro_label.setWordWrap(True)
        info_layout.addWidget(self._intro_label)

        self._repo_link = HyperlinkButton(REPO_URL, REPO_URL, info_card)
        info_layout.addWidget(self._repo_link, 0, Qt.AlignLeft)

        self._info_card = info_card

        # ===== 网络与合规声明 =====
        notice_card = CardWidget(container)
        notice_layout = QVBoxLayout(notice_card)
        notice_layout.setContentsMargins(24, 20, 24, 20)
        notice_layout.setSpacing(10)

        self._notice_title = SubtitleLabel(tr("about_notice_title"), notice_card)
        notice_layout.addWidget(self._notice_title)

        self._notice_text = BodyLabel(tr("about_notice_text"), notice_card)
        self._notice_text.setWordWrap(True)
        notice_layout.addWidget(self._notice_text)
        notice_layout.addStretch()

        self._notice_card = notice_card
        root.addLayout(self._top_grid)

        # ===== 赞助模块 =====
        sponsor_card = CardWidget(container)
        sponsor_layout = QVBoxLayout(sponsor_card)
        sponsor_layout.setContentsMargins(24, 20, 24, 20)
        sponsor_layout.setSpacing(10)
        self._sponsor_title = SubtitleLabel(tr("about_sponsorship_title"), sponsor_card)
        sponsor_layout.addWidget(self._sponsor_title)

        self._sponsor_text = BodyLabel(tr("settings_sponsorship_text"), sponsor_card)
        self._sponsor_text.setWordWrap(True)
        sponsor_layout.addWidget(self._sponsor_text)

        # 两张收款码卡片水平并排
        self._qr_grid = QGridLayout()
        self._qr_grid.setHorizontalSpacing(20)
        self._qr_grid.setVerticalSpacing(20)
        self._wechat_card = PaymentCard(tr("about_wechat"), "support/wei.png", sponsor_card)
        self._alipay_card = PaymentCard(tr("about_alipay"), "support/zhi.jpg", sponsor_card)
        sponsor_layout.addLayout(self._qr_grid)
        root.addWidget(sponsor_card)

        root.addStretch()
        scroll.setWidget(container)

        page_layout = QVBoxLayout(self)
        page_layout.setContentsMargins(0, 0, 0, 0)
        page_layout.addWidget(scroll)

        # 任务4：用 themeColor 给赞助标题着色（主题切换安全）
        self.refresh_theme()
        QTimer.singleShot(0, self._update_responsive_layout)

    def resizeEvent(self, event):
        super().resizeEvent(event)
        self._update_responsive_layout()

    def _update_responsive_layout(self):
        """窄窗口自动由双列切换为单列，避免文字和二维码被挤压。"""
        compact = self.width() < 860
        if compact == getattr(self, "_compact_layout", None):
            return
        self._compact_layout = compact

        for grid, widgets in (
            (self._top_grid, (self._info_card, self._notice_card)),
            (self._qr_grid, (self._wechat_card, self._alipay_card)),
        ):
            for widget in widgets:
                grid.removeWidget(widget)
            grid.setColumnStretch(0, 0)
            grid.setColumnStretch(1, 0)

        if compact:
            self._top_grid.addWidget(self._info_card, 0, 0)
            self._top_grid.addWidget(self._notice_card, 1, 0)
            self._qr_grid.addWidget(self._wechat_card, 0, 0)
            self._qr_grid.addWidget(self._alipay_card, 1, 0)
        else:
            self._top_grid.addWidget(self._info_card, 0, 0)
            self._top_grid.addWidget(self._notice_card, 0, 1)
            self._top_grid.setColumnStretch(0, 1)
            self._top_grid.setColumnStretch(1, 1)
            self._qr_grid.addWidget(self._wechat_card, 0, 0)
            self._qr_grid.addWidget(self._alipay_card, 0, 1)
            self._qr_grid.setColumnStretch(0, 1)
            self._qr_grid.setColumnStretch(1, 1)

    def refresh_theme(self):
        """任务4：主题切换时用最新 themeColor 重绘高亮标题。"""
        accent = themeColor().name()
        self._sponsor_title.setTextColor(accent, accent)

    def retranslate_ui(self):
        self._page_title.setText(tr("nav_about"))
        self._version_label.setText(tr("settings_version"))
        self._intro_label.setText(tr("about_intro"))
        self._notice_title.setText(tr("about_notice_title"))
        self._notice_text.setText(tr("about_notice_text"))
        self._sponsor_title.setText(tr("about_sponsorship_title"))
        self._sponsor_text.setText(tr("settings_sponsorship_text"))
        self._wechat_card.retranslate_ui(tr("about_wechat"))
        self._alipay_card.retranslate_ui(tr("about_alipay"))
