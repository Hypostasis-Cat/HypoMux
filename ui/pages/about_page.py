"""
HypoMux 关于页 (AboutPage) - 第四阶段任务3

承载从设置页迁出的版本信息、项目介绍、SignPath 代码签名致谢，以及赞助模块：
两张并排 CardWidget 分别渲染微信(support/wei.png) 与 支付宝(support/zhi.jpg) 收款码。

纯视图层，无任何后端依赖。全程使用 qfluentwidgets 原生组件，
深浅色主题自动适配；高亮文字用 themeColor() 着色并响应主题切换。
"""

import os

from PySide6.QtCore import Qt, QThread, QTimer, QUrl, Signal
from PySide6.QtGui import QDesktopServices, QPixmap
from PySide6.QtWidgets import (
    QApplication, QWidget, QVBoxLayout, QHBoxLayout, QGridLayout, QFrame,
)
from qfluentwidgets import (
    CardWidget, TitleLabel, SubtitleLabel, StrongBodyLabel, BodyLabel,
    IconWidget, ImageLabel, SingleDirectionScrollArea, PrimaryPushButton, PushButton, MessageBox,
    themeColor,
)

from ui.components import SurfaceCardWidget
from ui.i18n import tr
from ui.pages import resolve_icon
from ui.popup_material import apply_mica_popup
from utils.update_checker import (
    ReleaseInfo, UpdateError, download_installer, fetch_latest_release, is_newer_version,
)

REPO_URL = "https://github.com/Hypostasis-Cat/HypoMux"
QR_MAX_WIDTH = 180
SIGNPATH_LOGO_PATH = "support/SignPath/SignPath.png"
SIGNPATH_LOGO_HEIGHT = 38


class ReleaseCheckWorker(QThread):
    """Fetch GitHub release metadata off the GUI thread."""

    release_ready = Signal(object)
    failed = Signal(str)

    def run(self):
        try:
            self.release_ready.emit(fetch_latest_release())
        except UpdateError as exc:
            self.failed.emit(str(exc))
        except Exception:
            self.failed.emit(tr("about_update_unknown_error"))


class ReleaseDownloadWorker(QThread):
    """Download the selected installer without blocking the About page."""

    progress_changed = Signal(int, int)
    installer_ready = Signal(str)
    failed = Signal(str)

    def __init__(self, release: ReleaseInfo, parent=None):
        super().__init__(parent)
        self._release = release

    def run(self):
        try:
            installer = download_installer(
                self._release,
                progress=lambda downloaded, total: self.progress_changed.emit(downloaded, total),
            )
            self.installer_ready.emit(installer)
        except UpdateError as exc:
            self.failed.emit(str(exc))
        except Exception:
            self.failed.emit(tr("about_update_unknown_error"))


def _project_root() -> str:
    """返回项目根目录（ui/pages/ 的上两级）。"""
    return os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))


class PaymentCard(SurfaceCardWidget):
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

    info_message = Signal(str)
    error_message = Signal(str)
    install_ready = Signal(str)

    def __init__(self, parent=None):
        super().__init__(parent)
        self.setObjectName("aboutPage")
        self._check_worker = None
        self._download_worker = None
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

        version_row = QHBoxLayout()
        version_row.setSpacing(8)
        self._version_label = StrongBodyLabel(
            tr("settings_version", version=self._current_version()), info_card
        )
        version_row.addWidget(self._version_label)
        version_row.addStretch()
        self._github_btn = PushButton(
            resolve_icon("GITHUB", "LINK"), tr("about_open_github"), info_card
        )
        self._github_btn.setToolTip(REPO_URL)
        self._github_btn.clicked.connect(
            lambda: QDesktopServices.openUrl(QUrl(REPO_URL))
        )
        version_row.addWidget(self._github_btn)
        self._check_update_btn = PrimaryPushButton(
            resolve_icon("SYNC", "UPDATE"), tr("about_check_update"), info_card
        )
        # 按两种状态中较宽的文案锁定按钮宽度。否则右侧按钮以右边缘对齐时，
        # 在“检查更新”与“正在检查…”之间切换会导致左边缘来回跳动。
        self._lock_update_button_width()
        self._check_update_btn.clicked.connect(self._check_for_updates)
        version_row.addWidget(self._check_update_btn)
        info_layout.addLayout(version_row)

        self._update_status = BodyLabel("", info_card)
        # 更新状态出现/消失时不能增减布局行，否则卡片里的版本、介绍等文字会
        # 整体上下跳动。始终预留一行，只在需要时填入状态文案。
        self._update_status.setFixedHeight(self._update_status.fontMetrics().height())
        info_layout.addWidget(self._update_status)

        self._intro_label = BodyLabel(tr("about_intro"), info_card)
        self._intro_label.setWordWrap(True)
        info_layout.addWidget(self._intro_label)

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

        # ===== SignPath 代码签名致谢 =====
        signpath_card = CardWidget(container)
        signpath_layout = QHBoxLayout(signpath_card)
        signpath_layout.setContentsMargins(24, 18, 24, 18)
        signpath_layout.setSpacing(18)

        self._signpath_logo = ImageLabel(signpath_card)
        self._signpath_logo.setAlignment(Qt.AlignCenter)
        logo_path = os.path.join(_project_root(), SIGNPATH_LOGO_PATH)
        logo = QPixmap(logo_path)
        if not logo.isNull():
            logo = logo.scaledToHeight(SIGNPATH_LOGO_HEIGHT, Qt.SmoothTransformation)
            self._signpath_logo.setPixmap(logo)
            self._signpath_logo.setFixedSize(logo.size())
            signpath_layout.addWidget(self._signpath_logo, 0, Qt.AlignVCenter)

        signpath_copy = QVBoxLayout()
        signpath_copy.setSpacing(4)
        self._signpath_title = SubtitleLabel(tr("about_signpath_title"), signpath_card)
        signpath_copy.addWidget(self._signpath_title)
        self._signpath_text = BodyLabel(tr("about_signpath_text"), signpath_card)
        self._signpath_text.setWordWrap(True)
        signpath_copy.addWidget(self._signpath_text)
        signpath_layout.addLayout(signpath_copy, 1)
        root.addWidget(signpath_card)

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

        # 任务4：用 themeColor 给高亮标题着色（主题切换安全）
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
        self._signpath_title.setTextColor(accent, accent)

    @staticmethod
    def _current_version() -> str:
        app = QApplication.instance()
        return (app.applicationVersion() if app is not None else "") or "0.0.0"

    def _lock_update_button_width(self):
        """Keep the update action stationary while its status text changes."""
        original_text = self._check_update_btn.text()
        widths = []
        for text in (tr("about_check_update"), tr("about_checking_update")):
            self._check_update_btn.setText(text)
            widths.append(self._check_update_btn.sizeHint().width())
        self._check_update_btn.setText(original_text)
        self._check_update_btn.setFixedWidth(max(widths))

    def _set_update_state(self, text: str = "", *, checking: bool = False):
        self._check_update_btn.setEnabled(not checking)
        self._check_update_btn.setText(
            tr("about_checking_update") if checking else tr("about_check_update")
        )
        self._update_status.setText(text)

    def _check_for_updates(self):
        if self._check_worker is not None:
            return
        self._set_update_state(tr("about_checking_update"), checking=True)
        worker = ReleaseCheckWorker(self)
        worker.release_ready.connect(self._on_release_checked)
        worker.failed.connect(self._on_update_check_failed)
        worker.finished.connect(self._finish_update_check)
        worker.finished.connect(worker.deleteLater)
        self._check_worker = worker
        worker.start()

    def _finish_update_check(self):
        self._check_worker = None
        if self._download_worker is None:
            self._set_update_state()

    def _on_update_check_failed(self, reason: str):
        self.error_message.emit(tr("about_update_check_failed", error=reason))

    def _on_release_checked(self, release: ReleaseInfo):
        current = self._current_version()
        if not is_newer_version(release.tag_name, current):
            self.info_message.emit(tr("about_update_current", version=current))
            return

        notes = release.notes.strip()
        if len(notes) > 900:
            notes = f"{notes[:900].rstrip()}…"
        content = tr(
            "about_update_available_content",
            current=current,
            latest=release.tag_name.lstrip("vV"),
            notes=notes or tr("about_update_notes_empty"),
        )
        dialog = MessageBox(tr("about_update_available_title"), content, self)
        dialog.yesButton.setText(tr("about_update_now"))
        dialog.cancelButton.setText(tr("about_update_later"))
        apply_mica_popup(dialog)
        if dialog.exec():
            self._download_release(release)

    def _download_release(self, release: ReleaseInfo):
        if self._download_worker is not None:
            return
        self._set_update_state(tr("about_update_downloading", percent=0), checking=True)
        worker = ReleaseDownloadWorker(release, self)
        worker.progress_changed.connect(self._on_download_progress)
        worker.installer_ready.connect(self._on_installer_ready)
        worker.failed.connect(self._on_download_failed)
        worker.finished.connect(self._finish_update_download)
        worker.finished.connect(worker.deleteLater)
        self._download_worker = worker
        worker.start()

    def _on_download_progress(self, downloaded: int, total: int):
        if total <= 0:
            return
        percent = min(99, int(downloaded * 100 / total))
        self._update_status.setText(tr("about_update_downloading", percent=percent))

    def _on_installer_ready(self, installer_path: str):
        self._update_status.setText(tr("about_update_installing"))
        self.install_ready.emit(installer_path)

    def _on_download_failed(self, reason: str):
        self.error_message.emit(tr("about_update_download_failed", error=reason))

    def _finish_update_download(self):
        self._download_worker = None
        self._set_update_state()

    def prepare_for_shutdown(self):
        """Avoid Qt destroying a still-running update worker during app exit."""
        for worker in (self._check_worker, self._download_worker):
            if worker is not None and worker.isRunning():
                worker.wait(9000)

    def retranslate_ui(self):
        self._page_title.setText(tr("nav_about"))
        self._version_label.setText(
            tr("settings_version", version=self._current_version())
        )
        if self._check_worker is None and self._download_worker is None:
            self._check_update_btn.setText(tr("about_check_update"))
        self._lock_update_button_width()
        self._github_btn.setText(tr("about_open_github"))
        self._intro_label.setText(tr("about_intro"))
        self._notice_title.setText(tr("about_notice_title"))
        self._notice_text.setText(tr("about_notice_text"))
        self._signpath_title.setText(tr("about_signpath_title"))
        self._signpath_text.setText(tr("about_signpath_text"))
        self._sponsor_title.setText(tr("about_sponsorship_title"))
        self._sponsor_text.setText(tr("settings_sponsorship_text"))
        self._wechat_card.retranslate_ui(tr("about_wechat"))
        self._alipay_card.retranslate_ui(tr("about_alipay"))
