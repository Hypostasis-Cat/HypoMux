"""
HypoMux 系统设置页 (SettingsPage) - 第三阶段 Fluent 换装

用 qfluentwidgets SettingCard 体系重建设置面板，分组：
- 个性化：主题、主题色、云母与自定义背景图
- 全局设置：语言、关闭行为
- 配置与启动：开机自启开关、配置文件位置
- 关于项目：版本、仓库链接、赞助

逻辑联动（后端零改动，仅重绑到 Fluent 组件）：
- 语言切换  -> QSettings 持久化 + language_changed 信号
- 个性化设置 -> 独立 PersonalizationSettingGroup 即时应用 + QSettings 持久化
- 关闭行为  -> QSettings 持久化
- 端口      -> QSettings 持久化 + ports_changed 信号
- 开机自启  -> utils.autostart 写/删注册表（防御式回滚）
"""

import os

from PySide6.QtCore import Qt, Signal, QSettings
from PySide6.QtWidgets import (
    QWidget, QVBoxLayout, QFrame, QButtonGroup,
)
from qfluentwidgets import (
    SettingCard, SettingCardGroup, ComboBox, SpinBox,
    PushSettingCard, CaptionLabel, TitleLabel,
    LineEdit,
    RadioButton, SingleDirectionScrollArea,
)

from ui.i18n import tr
from ui.components import (
    LocalizedSwitchSettingCard,
    TranslucentPushSettingCard,
    TranslucentSettingCard,
    register_content_card_control,
)
from ui.pages import resolve_icon
from ui.pages.personalization_group import PersonalizationSettingGroup
from utils.autostart import set_autostart, is_autostart_enabled
from utils.config_manager import (
    get_config_path, load_config, save_config, DEFAULT_DNS_SERVER, DEFAULT_DOH_PROVIDER,
)
from utils.blocked_domain_tracker import get_tracker

DEFAULT_SOCKS_PORT = 10800
DEFAULT_HTTP_PORT = 10801
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
    background_image_changed = Signal(str)
    content_card_opacity_changed = Signal(int)
    blocked_domain_settings_changed = Signal()
    blocked_domains_requested = Signal()
    force_tun_connectivity_bypass_changed = Signal(bool)
    wfp_dns_egress_exemption_changed = Signal(bool)

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

        # 个性化设置独立封装，避免主题、云母和背景图逻辑散落在总设置页中。
        self.personalization_group = PersonalizationSettingGroup(container)
        self.personalization_group.theme_color_changed.connect(
            self.theme_color_changed.emit
        )
        self.personalization_group.mica_effect_changed.connect(
            self.mica_effect_changed.emit
        )
        self.personalization_group.background_image_changed.connect(
            self.background_image_changed.emit
        )
        self.personalization_group.content_card_opacity_changed.connect(
            self.content_card_opacity_changed.emit
        )
        self.personalization_group.warning_message.connect(self.warning_message.emit)
        root.addWidget(self.personalization_group)

        # ===== 分组1：全局设置 =====
        self.global_group = SettingCardGroup(tr("settings_global"), container)

        # 语言
        self.lang_card = TranslucentSettingCard(
            resolve_icon("LANGUAGE", "DICTIONARY"), tr("settings_language"), "", self.global_group
        )
        self.lang_combo = ComboBox(self.lang_card)
        self.lang_combo.addItem(tr("settings_language_zh"), userData="zh")
        self.lang_combo.addItem(tr("settings_language_en"), userData="en")
        saved_lang = settings.value("language", "zh")
        idx = self.lang_combo.findData(saved_lang)
        self.lang_combo.setCurrentIndex(idx if idx >= 0 else 0)
        register_content_card_control(self.lang_combo)
        self.lang_combo.currentIndexChanged.connect(self._on_language_changed)
        self.lang_card.hBoxLayout.addWidget(self.lang_combo, 0, Qt.AlignRight)
        self.lang_card.hBoxLayout.addSpacing(16)
        self.global_group.addSettingCard(self.lang_card)

        # 关闭行为（两个单选）
        self.close_card = TranslucentSettingCard(
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
        self.port_card = TranslucentSettingCard(
            resolve_icon("CONNECT", "GLOBE", "WIFI"),
            tr("settings_proxy_port"), "", self.global_group
        )
        self.socks_spin = SpinBox(self.port_card)
        self.socks_spin.setRange(1, 65534)
        self.socks_spin.setValue(settings.value("socks_port", DEFAULT_SOCKS_PORT, type=int))
        register_content_card_control(self.socks_spin)
        self.socks_spin.valueChanged.connect(self._on_port_changed)
        self.http_spin = SpinBox(self.port_card)
        self.http_spin.setRange(1, 65534)
        self.http_spin.setValue(settings.value("http_port", DEFAULT_HTTP_PORT, type=int))
        register_content_card_control(self.http_spin)
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

        self.dns_card = TranslucentSettingCard(
            resolve_icon("CONNECT", "GLOBE", "WIFI"),
            tr("settings_dns_server"),
            tr("settings_dns_fallback_hint"),
            self.network_group,
        )
        self.dns_edit = LineEdit(self.dns_card)
        self.dns_edit.setPlaceholderText(tr("settings_dns_placeholder"))
        self.dns_edit.setText(app_config.get("dns_server", DEFAULT_DNS_SERVER))
        register_content_card_control(self.dns_edit)
        self.dns_edit.editingFinished.connect(self._on_dns_edit_finished)
        self.dns_card.hBoxLayout.addWidget(self.dns_edit, 0, Qt.AlignRight)
        self.dns_card.hBoxLayout.addSpacing(16)
        self.network_group.addSettingCard(self.dns_card)

        self.doh_card = TranslucentSettingCard(
            resolve_icon("GLOBE", "CONNECT", "WIFI"),
            tr("settings_doh_policy"),
            tr("settings_doh_hint"),
            self.network_group,
        )
        self.doh_combo = ComboBox(self.doh_card)
        self.doh_combo.addItem(tr("settings_doh_auto"), userData="auto")
        self.doh_combo.addItem(tr("settings_doh_off"), userData="off")
        self.doh_combo.addItem(tr("settings_doh_alidns"), userData="alidns")
        self.doh_combo.addItem(tr("settings_doh_dnspod"), userData="dnspod")
        self.doh_combo.addItem(tr("settings_doh_google"), userData="google")
        saved_doh = str(app_config.get("doh_provider", DEFAULT_DOH_PROVIDER)).lower()
        doh_index = self.doh_combo.findData(saved_doh)
        self.doh_combo.setCurrentIndex(doh_index if doh_index >= 0 else 0)
        register_content_card_control(self.doh_combo)
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

        self.wfp_dns_exemption_card = LocalizedSwitchSettingCard(
            resolve_icon("SHIELD", "INFO", "WARNING"),
            tr("settings_wfp_dns_exemption"),
            tr("settings_wfp_dns_exemption_hint"),
            parent=self.advanced_network_group,
        )
        self.wfp_dns_exemption_card.setChecked(
            bool(app_config.get("wfp_dns_egress_exemption", True))
        )
        self.wfp_dns_exemption_card.checkedChanged.connect(
            self._on_wfp_dns_egress_exemption_changed
        )
        self.advanced_network_group.addSettingCard(self.wfp_dns_exemption_card)

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

        self.blocked_manage_card = TranslucentPushSettingCard(
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
        self.config_path_card = TranslucentPushSettingCard(
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

    def _on_wfp_dns_egress_exemption_changed(self, checked: bool):
        self.wfp_dns_egress_exemption_changed.emit(bool(checked))

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
        self.wfp_dns_exemption_card.setEnabled(enabled)
        self.blocked_enable_card.setEnabled(enabled)
        self.blocked_expiry_card.setEnabled(enabled)
        self.blocked_manage_card.setEnabled(enabled)

    def retranslate_ui(self):
        self._page_title.setText(tr("nav_settings"))
        self.personalization_group.retranslate_ui()
        self.global_group.titleLabel.setText(tr("settings_global"))
        self.lang_card.titleLabel.setText(tr("settings_language"))
        self.lang_combo.setItemText(0, tr("settings_language_zh"))
        self.lang_combo.setItemText(1, tr("settings_language_en"))
        for card in (
            self.force_tun_card,
            self.blocked_enable_card,
            self.blocked_expiry_card,
            self.autostart_card,
        ):
            card.refresh_switch_text()
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
        self.doh_combo.setItemText(1, tr("settings_doh_off"))
        self.doh_combo.setItemText(2, tr("settings_doh_alidns"))
        self.doh_combo.setItemText(3, tr("settings_doh_dnspod"))
        self.doh_combo.setItemText(4, tr("settings_doh_google"))
        self.advanced_network_group.titleLabel.setText(tr("settings_advanced_network"))
        self.force_tun_card.titleLabel.setText(tr("settings_force_tun"))
        self.force_tun_card.contentLabel.setText(tr("settings_force_tun_hint"))
        self.wfp_dns_exemption_card.titleLabel.setText(tr("settings_wfp_dns_exemption"))
        self.wfp_dns_exemption_card.contentLabel.setText(tr("settings_wfp_dns_exemption_hint"))
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
