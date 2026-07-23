"""
HypoMux 系统设置页 (SettingsPage) - 第三阶段 Fluent 换装

用 qfluentwidgets SettingCard 体系重建设置面板，分组：
- 全局设置：语言、主题、关闭行为
- 配置与启动：开机自启开关、配置文件位置
- 关于项目：版本、仓库链接、赞助

逻辑联动（后端零改动，仅重绑到 Fluent 组件）：
- 语言切换  -> QSettings 持久化 + language_changed 信号
- 主题切换  -> setTheme 即时切换 + QSettings 持久化
- 关闭行为  -> QSettings 持久化
- 端口      -> QSettings 持久化 + ports_changed 信号
- 开机自启  -> utils.autostart 写/删注册表（防御式回滚）
"""

import os

from PySide6.QtCore import Qt, Signal, QSettings
from PySide6.QtGui import QColor
from PySide6.QtWidgets import (
    QWidget, QVBoxLayout, QFrame, QButtonGroup,
)
from qfluentwidgets import (
    SettingCard, SettingCardGroup, ComboBox, SpinBox,
    PushSettingCard, PushButton, CaptionLabel, TitleLabel,
    LineEdit,
    RadioButton, SingleDirectionScrollArea, setTheme, setThemeColor, Theme,
)

from ui.i18n import tr
from ui.components import LocalizedColorPickerButton, LocalizedSwitchSettingCard
from ui.pages import resolve_icon
from utils.autostart import set_autostart, is_autostart_enabled
from utils.config_manager import (
    get_config_path, load_config, save_config, DEFAULT_DNS_SERVER, DEFAULT_DOH_PROVIDER,
)
from utils.blocked_domain_tracker import get_tracker

DEFAULT_SOCKS_PORT = 10800
DEFAULT_HTTP_PORT = 10801
DEFAULT_THEME_COLOR = "#0078d4"

_THEME_MAP = {0: Theme.AUTO, 1: Theme.LIGHT, 2: Theme.DARK}
_THEME_INDEX = {"auto": 0, "light": 1, "dark": 2}


class SettingsPage(QWidget):
    """系统设置页。

    Signals:
        language_changed(str)
        ports_changed(int, int)
        info_message(str)
        success_message(str)
        warning_message(str)
    """

    language_changed = Signal(str)
    ports_changed = Signal(int, int)
    info_message = Signal(str)
    success_message = Signal(str)
    warning_message = Signal(str)
    theme_color_changed = Signal()
    dns_changed = Signal(str, str)
    mica_effect_changed = Signal(bool)
    blocked_domain_settings_changed = Signal()
    blocked_domains_requested = Signal()
    force_tun_connectivity_bypass_changed = Signal(bool)

    def __init__(self, parent=None):
        super().__init__(parent)
        self.setObjectName("settingsPage")
        self._init_ui()

    def _init_ui(self):
        scroll = SingleDirectionScrollArea(self, orient=Qt.Vertical)
        scroll.setWidgetResizable(True)
        scroll.setFrameShape(QFrame.NoFrame)
        scroll.setStyleSheet("background: transparent;")

        container = QWidget()
        container.setStyleSheet("background: transparent;")
        root = QVBoxLayout(container)
        # 视觉爆改 3：大留白 + 干净大号加粗标题 + 标题下足量空行
        root.setContentsMargins(36, 28, 36, 28)
        root.setSpacing(20)

        settings = QSettings("Hypostasis-Cat", "HypoMux")

        # 顶部干净大号加粗标题
        self._page_title = TitleLabel(tr("nav_settings"), container)
        root.addWidget(self._page_title)
        root.addSpacing(8)

        # ===== 分组1：全局设置 =====
        self.global_group = SettingCardGroup(tr("settings_global"), container)

        # 语言
        self.lang_card = SettingCard(
            resolve_icon("LANGUAGE", "DICTIONARY"), tr("settings_language"), "", self.global_group
        )
        self.lang_combo = ComboBox(self.lang_card)
        self.lang_combo.addItem(tr("settings_language_zh"), userData="zh")
        self.lang_combo.addItem(tr("settings_language_en"), userData="en")
        saved_lang = settings.value("language", "zh")
        idx = self.lang_combo.findData(saved_lang)
        self.lang_combo.setCurrentIndex(idx if idx >= 0 else 0)
        self.lang_combo.currentIndexChanged.connect(self._on_language_changed)
        self.lang_card.hBoxLayout.addWidget(self.lang_combo, 0, Qt.AlignRight)
        self.lang_card.hBoxLayout.addSpacing(16)
        self.global_group.addSettingCard(self.lang_card)

        # 主题
        self.theme_card = SettingCard(
            resolve_icon("PALETTE", "CONSTRACT", "BRUSH"),
            tr("settings_theme"), tr("settings_theme_hint"), self.global_group
        )
        self.theme_combo = ComboBox(self.theme_card)
        self.theme_combo.addItem(tr("settings_theme_auto"), userData="auto")
        self.theme_combo.addItem(tr("settings_theme_light"), userData="light")
        self.theme_combo.addItem(tr("settings_theme_dark"), userData="dark")
        saved_theme = settings.value("theme", "auto")
        self.theme_combo.setCurrentIndex(_THEME_INDEX.get(saved_theme, 0))
        self.theme_combo.currentIndexChanged.connect(self._on_theme_changed)
        self.theme_card.hBoxLayout.addWidget(self.theme_combo, 0, Qt.AlignRight)
        self.theme_card.hBoxLayout.addSpacing(16)
        self.global_group.addSettingCard(self.theme_card)

        # 主题色可在默认 Fluent 蓝与自定义强调色间切换。自定义颜色会保留，
        # 因此切回自定义时无需重新选择。
        saved_color = QColor(settings.value("theme_color", DEFAULT_THEME_COLOR, type=str))
        if not saved_color.isValid():
            saved_color = QColor(DEFAULT_THEME_COLOR)
        saved_color_mode = settings.value("theme_color_mode", "", type=str)
        if saved_color_mode not in {"default", "custom"}:
            # 兼容第一个版本的主题色设置：非默认值视为用户已选的自定义色。
            saved_color_mode = (
                "custom" if saved_color.name().lower() != DEFAULT_THEME_COLOR else "default"
            )
        self._theme_color_mode = saved_color_mode
        self.theme_color_card = SettingCard(
            resolve_icon("PALETTE", "BRUSH", "CONSTRACT"),
            tr("settings_theme_color"),
            tr("settings_theme_color_hint"),
            self.global_group,
        )
        self.theme_color_mode_combo = ComboBox(self.theme_color_card)
        self.theme_color_mode_combo.addItem(
            tr("settings_theme_color_default"), userData="default"
        )
        self.theme_color_mode_combo.addItem(
            tr("settings_theme_color_custom"), userData="custom"
        )
        self.theme_color_mode_combo.setCurrentIndex(
            self.theme_color_mode_combo.findData(saved_color_mode)
        )
        self.theme_color_picker = LocalizedColorPickerButton(
            saved_color, tr("settings_theme_color"), self.theme_color_card
        )
        # 颜色按钮只用于复用 QFluentWidgets 的原生对话框；界面使用文字按钮，
        # 避免在默认模式仍显示容易误导的自定义色块。
        self.theme_color_picker.hide()
        self.theme_color_picker.colorChanged.connect(self._on_theme_color_changed)
        self.theme_color_choose_button = PushButton(
            tr("settings_theme_color_choose"), self.theme_color_card
        )
        self.theme_color_choose_button.setEnabled(saved_color_mode == "custom")
        self.theme_color_choose_button.clicked.connect(
            self.theme_color_picker.open_color_dialog
        )
        self.theme_color_mode_combo.currentIndexChanged.connect(
            self._on_theme_color_mode_changed
        )
        self.theme_color_card.hBoxLayout.addWidget(
            self.theme_color_choose_button, 0, Qt.AlignRight
        )
        self.theme_color_card.hBoxLayout.addSpacing(8)
        self.theme_color_card.hBoxLayout.addWidget(
            self.theme_color_mode_combo, 0, Qt.AlignRight
        )
        self.theme_color_card.hBoxLayout.addSpacing(16)
        self.global_group.addSettingCard(self.theme_color_card)

        # Windows 11 云母材质：默认开启；不支持的系统由主窗口安全降级。
        self.mica_card = LocalizedSwitchSettingCard(
            resolve_icon("BRUSH", "PALETTE", "CONSTRACT"),
            tr("settings_mica_effect"),
            tr("settings_mica_effect_hint"),
            parent=self.global_group,
        )
        self.mica_card.setChecked(settings.value("mica_enabled", True, type=bool))
        self.mica_card.checkedChanged.connect(self._on_mica_effect_changed)
        self.global_group.addSettingCard(self.mica_card)

        # 关闭行为（两个单选）
        self.close_card = SettingCard(
            resolve_icon("CLOSE", "POWER_BUTTON", "EMBED"),
            tr("settings_close_behavior"), "", self.global_group
        )
        self.close_tray_radio = RadioButton(tr("settings_close_to_tray"), self.close_card)
        self.close_exit_radio = RadioButton(tr("settings_close_to_exit"), self.close_card)
        self.close_group = QButtonGroup(self)
        self.close_group.addButton(self.close_tray_radio, 0)
        self.close_group.addButton(self.close_exit_radio, 1)
        saved_close = settings.value("close_behavior", "tray", type=str)
        if saved_close == "exit":
            self.close_exit_radio.setChecked(True)
        else:
            self.close_tray_radio.setChecked(True)
        self.close_group.buttonClicked.connect(self._on_close_behavior_changed)
        self.close_card.hBoxLayout.addWidget(self.close_tray_radio, 0, Qt.AlignRight)
        self.close_card.hBoxLayout.addSpacing(12)
        self.close_card.hBoxLayout.addWidget(self.close_exit_radio, 0, Qt.AlignRight)
        self.close_card.hBoxLayout.addSpacing(16)
        self.global_group.addSettingCard(self.close_card)

        # 端口
        self.port_card = SettingCard(
            resolve_icon("CONNECT", "GLOBE", "WIFI"),
            tr("settings_proxy_port"), "", self.global_group
        )
        self.socks_spin = SpinBox(self.port_card)
        self.socks_spin.setRange(1, 65534)
        self.socks_spin.setValue(settings.value("socks_port", DEFAULT_SOCKS_PORT, type=int))
        self.socks_spin.valueChanged.connect(self._on_port_changed)
        self.http_spin = SpinBox(self.port_card)
        self.http_spin.setRange(1, 65534)
        self.http_spin.setValue(settings.value("http_port", DEFAULT_HTTP_PORT, type=int))
        self.http_spin.valueChanged.connect(self._on_port_changed)
        self._socks_label = CaptionLabel(tr("settings_socks_label"), self.port_card)
        self._http_label = CaptionLabel(tr("settings_http_label"), self.port_card)
        self.port_card.hBoxLayout.addWidget(self._socks_label, 0, Qt.AlignRight)
        self.port_card.hBoxLayout.addWidget(self.socks_spin, 0, Qt.AlignRight)
        self.port_card.hBoxLayout.addSpacing(10)
        self.port_card.hBoxLayout.addWidget(self._http_label, 0, Qt.AlignRight)
        self.port_card.hBoxLayout.addWidget(self.http_spin, 0, Qt.AlignRight)
        self.port_card.hBoxLayout.addSpacing(16)
        self.global_group.addSettingCard(self.port_card)

        root.addWidget(self.global_group)

        # ===== 分组2：网络与 DNS 设置 =====
        app_config = load_config()
        self.network_group = SettingCardGroup(tr("settings_network_dns"), container)

        self.dns_card = SettingCard(
            resolve_icon("CONNECT", "GLOBE", "WIFI"),
            tr("settings_dns_server"),
            tr("settings_dns_fallback_hint"),
            self.network_group,
        )
        self.dns_edit = LineEdit(self.dns_card)
        self.dns_edit.setPlaceholderText(tr("settings_dns_placeholder"))
        self.dns_edit.setText(app_config.get("dns_server", DEFAULT_DNS_SERVER))
        self.dns_edit.editingFinished.connect(self._on_dns_edit_finished)
        self.dns_card.hBoxLayout.addWidget(self.dns_edit, 0, Qt.AlignRight)
        self.dns_card.hBoxLayout.addSpacing(16)
        self.network_group.addSettingCard(self.dns_card)

        self.doh_card = SettingCard(
            resolve_icon("GLOBE", "CONNECT", "WIFI"),
            tr("settings_doh_policy"),
            tr("settings_doh_hint"),
            self.network_group,
        )
        self.doh_combo = ComboBox(self.doh_card)
        self.doh_combo.addItem(tr("settings_doh_auto"), userData="auto")
        self.doh_combo.addItem(tr("settings_doh_alidns"), userData="alidns")
        self.doh_combo.addItem(tr("settings_doh_dnspod"), userData="dnspod")
        self.doh_combo.addItem(tr("settings_doh_google"), userData="google")
        saved_doh = str(app_config.get("doh_provider", DEFAULT_DOH_PROVIDER)).lower()
        doh_index = self.doh_combo.findData(saved_doh)
        self.doh_combo.setCurrentIndex(doh_index if doh_index >= 0 else 0)
        self.doh_combo.currentIndexChanged.connect(self._on_doh_policy_changed)
        self.doh_card.hBoxLayout.addWidget(self.doh_combo, 0, Qt.AlignRight)
        self.doh_card.hBoxLayout.addSpacing(16)
        self.network_group.addSettingCard(self.doh_card)
        root.addWidget(self.network_group)

        # ===== 分组3：高级网络 / 特殊网络环境 =====
        self.advanced_network_group = SettingCardGroup(tr("settings_advanced_network"), container)
        self.force_tun_card = LocalizedSwitchSettingCard(
            resolve_icon("WARNING", "INFO", "ERROR"),
            tr("settings_force_tun"),
            tr("settings_force_tun_hint"),
            parent=self.advanced_network_group,
        )
        self.force_tun_card.setChecked(
            bool(app_config.get("force_tun_connectivity_bypass", False))
        )
        self.force_tun_card.checkedChanged.connect(
            self._on_force_tun_connectivity_bypass_changed
        )
        self.advanced_network_group.addSettingCard(self.force_tun_card)

        tracker = get_tracker()
        self.blocked_enable_card = LocalizedSwitchSettingCard(
            resolve_icon("BLOCK", "CANCEL", "CLOSE"),
            tr("blocked_enable"),
            tr("blocked_enable_hint"),
            parent=self.advanced_network_group,
        )
        self.blocked_enable_card.setChecked(tracker.enabled)
        self.blocked_enable_card.checkedChanged.connect(self._on_blocked_enable_changed)
        self.advanced_network_group.addSettingCard(self.blocked_enable_card)

        self.blocked_expiry_card = LocalizedSwitchSettingCard(
            resolve_icon("HISTORY", "SYNC", "UPDATE"),
            tr("blocked_expiry_toggle"),
            tr("blocked_expiry_hint"),
            parent=self.advanced_network_group,
        )
        self.blocked_expiry_card.setChecked(tracker.use_expiry)
        self.blocked_expiry_card.checkedChanged.connect(self._on_blocked_expiry_changed)
        self.advanced_network_group.addSettingCard(self.blocked_expiry_card)

        self.blocked_manage_card = PushSettingCard(
            tr("settings_blocked_domains_open"),
            resolve_icon("BLOCK", "CANCEL", "CLOSE"),
            tr("settings_blocked_domains_manage"),
            tr("settings_blocked_domains_manage_hint"),
            self.advanced_network_group,
        )
        self.blocked_manage_card.clicked.connect(self.blocked_domains_requested.emit)
        self.advanced_network_group.addSettingCard(self.blocked_manage_card)
        root.addWidget(self.advanced_network_group)

        # ===== 分组4：配置与启动 =====
        self.startup_group = SettingCardGroup(tr("settings_config_group"), container)

        # 开机自启（SwitchSettingCard）
        self.autostart_card = LocalizedSwitchSettingCard(
            resolve_icon("POWER_BUTTON", "EMBED", "APPLICATION"),
            tr("settings_autostart"), tr("settings_autostart_hint"),
            parent=self.startup_group
        )
        try:
            self.autostart_card.setChecked(is_autostart_enabled())
        except Exception:
            self.autostart_card.setChecked(False)
        self.autostart_card.checkedChanged.connect(self._on_autostart_changed)
        self.startup_group.addSettingCard(self.autostart_card)

        # 配置文件位置
        try:
            cfg_path = str(get_config_path())
        except Exception:
            cfg_path = "~/.hypomux/config.json"
        self.config_path_card = PushSettingCard(
            "…",
            resolve_icon("FOLDER", "DOCUMENT"),
            tr("settings_config_path"), cfg_path, self.startup_group
        )
        self.config_path_card.clicked.connect(self._open_config_dir)
        self.startup_group.addSettingCard(self.config_path_card)

        root.addWidget(self.startup_group)

        # 任务3：关于/版本/赞助信息已迁移至独立的 AboutPage，设置页不再承载。

        root.addStretch()
        scroll.setWidget(container)

        page_layout = QVBoxLayout(self)
        page_layout.setContentsMargins(0, 0, 0, 0)
        page_layout.addWidget(scroll)

    # ========== 交互逻辑 ==========
    def _on_language_changed(self, index):
        settings = QSettings("Hypostasis-Cat", "HypoMux")
        lang_code = self.lang_combo.itemData(index)
        settings.setValue("language", lang_code)
        settings.setValue("ui/language", lang_code)
        settings.sync()
        self.retranslate_ui()
        self.language_changed.emit(lang_code)
        self.info_message.emit(tr("settings_lang_saved"))

    def _on_theme_changed(self, index):
        settings = QSettings("Hypostasis-Cat", "HypoMux")
        theme_code = self.theme_combo.itemData(index)
        settings.setValue("theme", theme_code)
        settings.sync()
        setTheme(_THEME_MAP.get(index, Theme.AUTO))

    def _on_theme_color_changed(self, color):
        """立即应用并持久化用户选定的 Fluent 主题色。"""
        color = QColor(color)
        if not color.isValid():
            return
        settings = QSettings("Hypostasis-Cat", "HypoMux")
        settings.setValue("theme_color", color.name())
        settings.sync()
        if self._theme_color_mode != "custom":
            return
        setThemeColor(color)
        self.theme_color_changed.emit()

    def _on_theme_color_mode_changed(self, index):
        """在默认 Fluent 蓝与保留的自定义主题色间即时切换。"""
        color_mode = self.theme_color_mode_combo.itemData(index)
        if color_mode not in {"default", "custom"}:
            return

        self._theme_color_mode = color_mode
        self.theme_color_choose_button.setEnabled(color_mode == "custom")
        settings = QSettings("Hypostasis-Cat", "HypoMux")
        settings.setValue("theme_color_mode", color_mode)
        settings.sync()
        setThemeColor(
            self.theme_color_picker.color
            if color_mode == "custom"
            else DEFAULT_THEME_COLOR
        )
        self.theme_color_changed.emit()

    def _on_mica_effect_changed(self, enabled: bool):
        settings = QSettings("Hypostasis-Cat", "HypoMux")
        settings.setValue("mica_enabled", bool(enabled))
        settings.sync()
        self.mica_effect_changed.emit(bool(enabled))

    def _on_close_behavior_changed(self, button):
        settings = QSettings("Hypostasis-Cat", "HypoMux")
        bid = self.close_group.id(button)
        settings.setValue("close_behavior", "exit" if bid == 1 else "tray")
        settings.sync()

    def _on_port_changed(self):
        settings = QSettings("Hypostasis-Cat", "HypoMux")
        socks = self.socks_spin.value()
        http = self.http_spin.value()
        settings.setValue("socks_port", socks)
        settings.setValue("http_port", http)
        settings.sync()
        self.ports_changed.emit(socks, http)

    @staticmethod
    def _is_valid_ipv4(value: str) -> bool:
        parts = str(value).strip().split(".")
        if len(parts) != 4:
            return False
        for part in parts:
            if not part.isdigit():
                return False
            number = int(part)
            if number < 0 or number > 255:
                return False
        return True

    def _on_dns_edit_finished(self):
        dns = self.dns_edit.text().strip()
        if not self._is_valid_ipv4(dns):
            self.dns_edit.setText(load_config().get("dns_server", DEFAULT_DNS_SERVER))
            self.warning_message.emit(tr("settings_dns_invalid"))
            return
        cfg = load_config()
        cfg["dns_server"] = dns
        if save_config(cfg):
            self.dns_changed.emit(dns, str(cfg.get("doh_provider", DEFAULT_DOH_PROVIDER)))
            self.success_message.emit(tr("settings_dns_saved"))
        else:
            self.warning_message.emit(tr("settings_dns_save_failed"))

    def _on_doh_policy_changed(self, index):
        provider = self.doh_combo.itemData(index) or DEFAULT_DOH_PROVIDER
        cfg = load_config()
        cfg["doh_provider"] = provider
        dns = str(cfg.get("dns_server", DEFAULT_DNS_SERVER))
        if save_config(cfg):
            self.dns_changed.emit(dns, provider)
            self.success_message.emit(tr("settings_dns_saved"))
        else:
            self.warning_message.emit(tr("settings_dns_save_failed"))

    def _on_blocked_enable_changed(self, checked: bool):
        tracker = get_tracker()
        tracker.enabled = checked
        tracker.save()
        self.blocked_domain_settings_changed.emit()

    def _on_force_tun_connectivity_bypass_changed(self, checked: bool):
        """Persist through MainWindow so the active TUN lifecycle has one config source."""
        self.force_tun_connectivity_bypass_changed.emit(bool(checked))

    def _on_blocked_expiry_changed(self, checked: bool):
        tracker = get_tracker()
        tracker.use_expiry = checked
        tracker.save()
        self.blocked_domain_settings_changed.emit()

    def _on_autostart_changed(self, checked: bool):
        ok = set_autostart(checked)
        settings = QSettings("Hypostasis-Cat", "HypoMux")
        if ok:
            settings.setValue("autostart", checked)
            settings.sync()
            if checked:
                self.success_message.emit(tr("settings_autostart_on"))
            else:
                self.info_message.emit(tr("settings_autostart_off"))
        else:
            # 回滚 UI（屏蔽信号避免递归）
            self.autostart_card.switchButton.blockSignals(True)
            self.autostart_card.setChecked(not checked)
            self.autostart_card.switchButton.blockSignals(False)
            self.warning_message.emit(tr("settings_autostart_failed"))

    def _open_config_dir(self):
        try:
            cfg = get_config_path()
            os.startfile(str(cfg.parent))
        except Exception:
            pass

    def set_controls_enabled(self, enabled: bool):
        """运行中锁定会影响底层网络栈的设置项。"""
        self.dns_edit.setEnabled(enabled)
        self.doh_combo.setEnabled(enabled)
        self.force_tun_card.setEnabled(enabled)
        self.blocked_enable_card.setEnabled(enabled)
        self.blocked_expiry_card.setEnabled(enabled)
        self.blocked_manage_card.setEnabled(enabled)

    def retranslate_ui(self):
        self._page_title.setText(tr("nav_settings"))
        self.global_group.titleLabel.setText(tr("settings_global"))
        self.lang_card.titleLabel.setText(tr("settings_language"))
        self.lang_combo.setItemText(0, tr("settings_language_zh"))
        self.lang_combo.setItemText(1, tr("settings_language_en"))
        self.theme_card.titleLabel.setText(tr("settings_theme"))
        self.theme_card.contentLabel.setText(tr("settings_theme_hint"))
        # 主题下拉项文本
        self.theme_combo.setItemText(0, tr("settings_theme_auto"))
        self.theme_combo.setItemText(1, tr("settings_theme_light"))
        self.theme_combo.setItemText(2, tr("settings_theme_dark"))
        self.theme_color_card.titleLabel.setText(tr("settings_theme_color"))
        self.theme_color_card.contentLabel.setText(tr("settings_theme_color_hint"))
        self.theme_color_mode_combo.setItemText(0, tr("settings_theme_color_default"))
        self.theme_color_mode_combo.setItemText(1, tr("settings_theme_color_custom"))
        self.theme_color_picker.title = tr("settings_theme_color")
        self.theme_color_choose_button.setText(tr("settings_theme_color_choose"))
        for card in (
            self.mica_card,
            self.force_tun_card,
            self.blocked_enable_card,
            self.blocked_expiry_card,
            self.autostart_card,
        ):
            card.refresh_switch_text()
        self.mica_card.titleLabel.setText(tr("settings_mica_effect"))
        self.mica_card.contentLabel.setText(tr("settings_mica_effect_hint"))
        self.close_card.titleLabel.setText(tr("settings_close_behavior"))
        self.close_tray_radio.setText(tr("settings_close_to_tray"))
        self.close_exit_radio.setText(tr("settings_close_to_exit"))
        self.port_card.titleLabel.setText(tr("settings_proxy_port"))
        self._socks_label.setText(tr("settings_socks_label"))
        self._http_label.setText(tr("settings_http_label"))
        self.network_group.titleLabel.setText(tr("settings_network_dns"))
        self.dns_card.titleLabel.setText(tr("settings_dns_server"))
        self.dns_card.contentLabel.setText(tr("settings_dns_fallback_hint"))
        self.dns_edit.setPlaceholderText(tr("settings_dns_placeholder"))
        self.doh_card.titleLabel.setText(tr("settings_doh_policy"))
        self.doh_card.contentLabel.setText(tr("settings_doh_hint"))
        self.doh_combo.setItemText(0, tr("settings_doh_auto"))
        self.doh_combo.setItemText(1, tr("settings_doh_alidns"))
        self.doh_combo.setItemText(2, tr("settings_doh_dnspod"))
        self.doh_combo.setItemText(3, tr("settings_doh_google"))
        self.advanced_network_group.titleLabel.setText(tr("settings_advanced_network"))
        self.force_tun_card.titleLabel.setText(tr("settings_force_tun"))
        self.force_tun_card.contentLabel.setText(tr("settings_force_tun_hint"))
        self.blocked_enable_card.titleLabel.setText(tr("blocked_enable"))
        self.blocked_enable_card.contentLabel.setText(tr("blocked_enable_hint"))
        self.blocked_expiry_card.titleLabel.setText(tr("blocked_expiry_toggle"))
        self.blocked_expiry_card.contentLabel.setText(tr("blocked_expiry_hint"))
        self.blocked_manage_card.titleLabel.setText(tr("settings_blocked_domains_manage"))
        self.blocked_manage_card.contentLabel.setText(tr("settings_blocked_domains_manage_hint"))
        self.blocked_manage_card.button.setText(tr("settings_blocked_domains_open"))
        self.startup_group.titleLabel.setText(tr("settings_config_group"))
        self.autostart_card.titleLabel.setText(tr("settings_autostart"))
        self.autostart_card.contentLabel.setText(tr("settings_autostart_hint"))
        self.config_path_card.titleLabel.setText(tr("settings_config_path"))
