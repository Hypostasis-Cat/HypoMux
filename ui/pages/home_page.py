"""
HypoMux 首页数据看板 (HomePage)

首页只负责运行模式、总开关、网卡行列表与遥测矩阵展示。后台日志不再进入
主界面文本框，由 MainWindow 写入用户目录日志文件。
"""

from typing import Dict, List

from PySide6.QtCore import Qt, Signal
from PySide6.QtWidgets import QWidget, QVBoxLayout, QHBoxLayout, QFrame, QSizePolicy
from qfluentwidgets import (
    ElevatedCardWidget, SwitchButton, StrongBodyLabel, SpinBox,
    BodyLabel, CaptionLabel, SubtitleLabel, DisplayLabel, CheckBox,
    PushButton, TransparentToolButton, InfoBadge, InfoLevel, IconWidget,
    SingleDirectionScrollArea, SmoothScrollArea, FluentIcon, themeColor, SegmentedWidget,
    HorizontalSeparator, ToolTipFilter, IndeterminateProgressRing,
)

from ui.i18n import tr


_HEALTH_LEVEL = {
    "available": InfoLevel.SUCCESS,
    "unstable": InfoLevel.WARNING,
    "unavailable": InfoLevel.ERROR,
}
_DEFAULT_INFO_LEVEL = getattr(
    InfoLevel,
    "INFORMATION",
    getattr(InfoLevel, "INFOAMTION", None),
)
if _DEFAULT_INFO_LEVEL is None:
    _DEFAULT_INFO_LEVEL = InfoLevel.WARNING


class AdapterRow(QWidget):
    """扁平化网卡行。"""

    toggled = Signal(str, bool)
    weight_changed = Signal(str, int)

    def __init__(self, adapter: Dict, schedule_weight: int = 1, parent=None):
        super().__init__(parent)
        self.setFixedHeight(52)
        self._alias = adapter.get("alias", "")
        self._ip = adapter.get("ip", "") or adapter.get("ipv4", "")
        self._is_ppp = bool(adapter.get("is_ppp", False))
        self._last_status = None
        self._last_speed_mbps = 0.0
        self._last_connections = 0

        layout = QHBoxLayout(self)
        layout.setContentsMargins(12, 8, 12, 8)
        layout.setSpacing(12)

        self.checkbox = CheckBox(self)
        self.checkbox.toggled.connect(self._on_toggled)
        self._icon = IconWidget(FluentIcon.CONNECT if self._is_ppp else FluentIcon.WIFI, self)
        self._icon.setFixedSize(18, 18)
        self._name_label = StrongBodyLabel(self._alias, self)
        self._name_label.setMinimumWidth(150)
        self._name_label.setSizePolicy(QSizePolicy.Expanding, QSizePolicy.Preferred)
        ip_text = f"{self._ip}  ·  {tr('home_card_ppp')}" if self._is_ppp else self._ip
        self._ip_label = CaptionLabel(ip_text, self)
        self._ip_label.setMinimumWidth(150)
        self._speed_label = BodyLabel(tr("home_row_traffic", speed=0.0, conn=0), self)
        self._speed_label.setMinimumWidth(180)

        # 调度权重输入
        self._bw_unit = CaptionLabel(tr("home_bw_column"), self)
        self._bw_spin = SpinBox(self)
        self._bw_spin.setRange(1, 100)
        self._bw_spin.setValue(max(1, min(int(schedule_weight or 1), 100)))
        # Fluent SpinBox 内置独立的上下按钮，需要保留足够宽度给数字编辑区。
        self._bw_spin.setFixedWidth(142)
        self._bw_spin.setToolTip(tr("home_bw_column_hint"))
        self._bw_spin.installEventFilter(ToolTipFilter(self._bw_spin))
        self._bw_spin.valueChanged.connect(self._on_bw_changed)

        self._health_badge = InfoBadge.info(tr("home_health_unknown"), self)

        layout.addWidget(self.checkbox)
        layout.addWidget(self._icon)
        layout.addWidget(self._name_label, 2)
        layout.addWidget(self._ip_label, 2)
        layout.addWidget(self._speed_label, 2)
        layout.addWidget(self._bw_unit)
        layout.addWidget(self._bw_spin)
        layout.addStretch()
        layout.addWidget(self._health_badge, 0, Qt.AlignRight)
        self._apply_active_style(False)

    @property
    def alias(self) -> str:
        return self._alias

    @property
    def schedule_weight(self) -> int:
        return self._bw_spin.value()

    def _on_bw_changed(self, value: int):
        self.weight_changed.emit(self._alias, value)

    def _on_toggled(self, checked: bool):
        self._apply_active_style(checked)
        self.toggled.emit(self._alias, checked)

    def set_checked(self, checked: bool):
        self.checkbox.blockSignals(True)
        self.checkbox.setChecked(checked)
        self.checkbox.blockSignals(False)
        self._apply_active_style(checked)

    def is_checked(self) -> bool:
        return self.checkbox.isChecked()

    def _apply_active_style(self, active: bool):
        if active:
            accent = themeColor().name()
            self._name_label.setTextColor(accent, accent)
            self._speed_label.setTextColor(accent, accent)
        else:
            # 不用 setStyleSheet("") 清空：那会覆盖 qfluentwidgets
            # 为深浅主题维护的文字颜色，导致深色模式回退成黑字。
            self._name_label.setTextColor()
            self._speed_label.setTextColor()

    def refresh_theme(self):
        self._apply_active_style(self.is_checked())

    def update_telemetry(self, speed_mbps: float, connections: int):
        self._last_speed_mbps = float(speed_mbps or 0.0)
        self._last_connections = int(connections or 0)
        self._speed_label.setText(tr("home_row_traffic", speed=speed_mbps, conn=connections))

    def reset_telemetry(self):
        self.update_telemetry(0.0, 0)

    def update_health(self, status: str):
        self._last_status = status
        level = _HEALTH_LEVEL.get(status, _DEFAULT_INFO_LEVEL)
        text = tr(f"diag_status_{status}") if status in _HEALTH_LEVEL else tr("home_health_unknown")
        new_badge = InfoBadge.make(text, self, level=level)
        layout = self.layout()
        layout.replaceWidget(self._health_badge, new_badge)
        self._health_badge.deleteLater()
        self._health_badge = new_badge

    def retranslate_ui(self):
        ip_text = f"{self._ip}  ·  {tr('home_card_ppp')}" if self._is_ppp else self._ip
        self._ip_label.setText(ip_text)
        self._speed_label.setText(tr(
            "home_row_traffic",
            speed=self._last_speed_mbps,
            conn=self._last_connections,
        ))
        self._bw_unit.setText(tr("home_bw_column"))
        self._bw_spin.setToolTip(tr("home_bw_column_hint"))
        self.update_health(self._last_status or "")


class MetricCard(ElevatedCardWidget):
    """四宫格遥测小卡。"""

    def __init__(self, title_key: str, value: str, parent=None):
        super().__init__(parent)
        self._title_key = title_key
        layout = QVBoxLayout(self)
        layout.setContentsMargins(18, 16, 18, 16)
        layout.setSpacing(8)
        self.title_label = CaptionLabel(tr(title_key), self)
        self.value_label = StrongBodyLabel(value, self)
        layout.addWidget(self.title_label)
        layout.addWidget(self.value_label)
        layout.addStretch()

    def set_value(self, value: str):
        self.value_label.setText(value)

    def retranslate_ui(self):
        self.title_label.setText(tr(self._title_key))


class KernelMetricCard(MetricCard):
    def __init__(self, parent=None):
        super().__init__("home_metric_kernel", tr("home_kernel_proxy"), parent)
        self._mode = "proxy"
        self._running = False

    def set_mode(self, mode: str, running: bool):
        self._mode = mode
        self._running = running
        if not running:
            self.set_value(tr("home_kernel_idle"))
        elif mode == "tun":
            self.set_value(tr("home_kernel_tun"))
        else:
            self.set_value(tr("home_kernel_proxy"))

    def retranslate_ui(self):
        super().retranslate_ui()
        self.set_mode(self._mode, self._running)


class HomePage(QWidget):
    """首页数据看板。"""

    engine_toggled = Signal(bool)
    select_all_clicked = Signal()
    deselect_all_clicked = Signal()
    refresh_clicked = Signal()
    adapter_checked = Signal(str, bool)
    adapter_weight_changed = Signal(str, int)
    mode_changed = Signal(str)
    weighted_toggled = Signal(bool)

    def __init__(self, parent=None):
        super().__init__(parent)
        self.setObjectName("homePage")
        self._cards: Dict[str, AdapterRow] = {}
        self._adapter_controls_enabled = True
        self._accelerated_bytes = 0.0
        self._last_down_mbps = 0.0
        self._last_up_mbps = 0.0
        self._last_connections = 0
        self._current_mode = "tun"
        self._last_socks_port = 10800
        self._last_http_port = 10801
        self._engine_running = False
        self._engine_busy = False
        self._init_ui()

    def _init_ui(self):
        scroll = SingleDirectionScrollArea(self, orient=Qt.Vertical)
        scroll.setWidgetResizable(True)
        scroll.setFrameShape(QFrame.NoFrame)
        scroll.setStyleSheet("background: transparent;")

        container = QWidget()
        container.setStyleSheet("background: transparent;")
        root = QVBoxLayout(container)
        root.setContentsMargins(24, 24, 24, 24)
        root.setSpacing(20)

        root.addWidget(self._build_engine_card())
        root.addWidget(self._build_adapters_card(), 1)
        root.addLayout(self._build_metric_matrix())

        scroll.setWidget(container)
        page_layout = QVBoxLayout(self)
        page_layout.setContentsMargins(0, 0, 0, 0)
        page_layout.addWidget(scroll)

    def _build_engine_card(self) -> QWidget:
        card = ElevatedCardWidget(self)
        card.setFixedHeight(236)
        layout = QHBoxLayout(card)
        layout.setContentsMargins(36, 24, 36, 24)
        layout.setSpacing(28)

        left = QVBoxLayout()
        left.setSpacing(12)
        self._engine_title = SubtitleLabel(tr("home_engine_title"), card)

        mode_row = QHBoxLayout()
        mode_row.setSpacing(10)
        self._mode_label = CaptionLabel(tr("mode_label"), card)
        self.mode_segment = SegmentedWidget(card)
        self.mode_segment.addItem("proxy", tr("mode_proxy"))
        self.mode_segment.addItem("tun", tr("mode_tun"))
        self.mode_segment.setCurrentItem("tun")
        self.mode_segment.currentItemChanged.connect(self._on_mode_changed)
        mode_row.addWidget(self._mode_label)
        mode_row.addWidget(self.mode_segment)
        mode_row.addStretch()

        controls = QVBoxLayout()
        controls.setSpacing(8)

        switch_row = QHBoxLayout()
        switch_row.setSpacing(12)
        engine_text = QVBoxLayout()
        engine_text.setSpacing(0)
        self._engine_label = StrongBodyLabel(tr("home_engine_control"), card)
        self._engine_hint = CaptionLabel(tr("home_engine_control_hint"), card)
        engine_text.addWidget(self._engine_label)
        engine_text.addWidget(self._engine_hint)
        self.engine_switch = SwitchButton(card)
        self.engine_switch.setOnText("")
        self.engine_switch.setOffText("")
        # 说明文字已固定展示在右侧，避免悬停 Tooltip 在部分主题下出现黑色空框。
        self.engine_switch.setToolTip("")
        self.engine_switch.checkedChanged.connect(self.engine_toggled.emit)
        self._engine_loading = IndeterminateProgressRing(card)
        self._engine_loading.setFixedSize(18, 18)
        self._engine_loading.setVisible(False)
        self._engine_status = CaptionLabel(tr("home_engine_switch_off"), card)
        # 两行共用同一右侧状态列：加载圈位于列外，不会推挤状态文字。
        self._engine_status.setFixedWidth(128)
        self._engine_status.setAlignment(Qt.AlignLeft | Qt.AlignVCenter)
        switch_row.addWidget(self.engine_switch)
        switch_row.addLayout(engine_text)
        switch_row.addStretch()
        switch_row.addWidget(self._engine_loading)
        switch_row.addWidget(self._engine_status)

        scheduler_row = QHBoxLayout()
        scheduler_row.setSpacing(12)
        self._weighted_switch = SwitchButton(card)
        self._weighted_switch.setOnText("")
        self._weighted_switch.setOffText("")
        self._weighted_switch.setToolTip("")
        self._weighted_switch.checkedChanged.connect(self._on_weighted_toggled)
        scheduler_text = QVBoxLayout()
        scheduler_text.setSpacing(0)
        self._weighted_label = StrongBodyLabel(tr("home_weighted_toggle"), card)
        self._weighted_hint = CaptionLabel(tr("home_weighted_hint"), card)
        scheduler_text.addWidget(self._weighted_label)
        scheduler_text.addWidget(self._weighted_hint)
        self._weighted_status = CaptionLabel(tr("home_engine_switch_off"), card)
        self._weighted_status.setFixedWidth(128)
        self._weighted_status.setAlignment(Qt.AlignLeft | Qt.AlignVCenter)
        scheduler_row.addWidget(self._weighted_switch)
        scheduler_row.addLayout(scheduler_text)
        scheduler_row.addStretch()
        scheduler_row.addWidget(self._weighted_status)

        controls.addLayout(switch_row)
        controls.addLayout(scheduler_row)

        self._engine_ports = CaptionLabel("", card)
        self._engine_ports.setContentsMargins(0, 2, 0, 0)
        left.addWidget(self._engine_title)
        left.addLayout(mode_row)
        left.addLayout(controls)
        left.addWidget(self._engine_ports)
        left.addStretch()

        right = QVBoxLayout()
        right.setSpacing(4)
        right.setAlignment(Qt.AlignRight | Qt.AlignVCenter)
        speed_row = QHBoxLayout()
        speed_row.setSpacing(8)
        speed_row.setAlignment(Qt.AlignRight | Qt.AlignBottom)
        self._speed_value = DisplayLabel("0.00", card)
        self._speed_value.setAlignment(Qt.AlignRight | Qt.AlignBottom)
        hero_font = self._speed_value.font()
        hero_font.setPointSize(36)
        hero_font.setBold(True)
        self._speed_value.setFont(hero_font)
        self._speed_unit = SubtitleLabel(tr("home_speed_unit"), card)
        self._speed_unit.setAlignment(Qt.AlignRight | Qt.AlignBottom)
        self._apply_hero_color()
        speed_row.addStretch()
        speed_row.addWidget(self._speed_value)
        speed_row.addWidget(self._speed_unit)
        self._speed_caption = CaptionLabel(tr("home_total_speed"), card)
        self._speed_caption.setAlignment(Qt.AlignRight)
        self._up_conn = CaptionLabel(tr("home_up_conn", up=0.0, conn=0), card)
        self._up_conn.setAlignment(Qt.AlignRight)
        right.addStretch()
        right.addLayout(speed_row)
        right.addWidget(self._speed_caption)
        right.addWidget(self._up_conn)
        right.addStretch()

        layout.addLayout(left, 1)
        layout.addLayout(right)
        return card

    def _build_adapters_card(self) -> QWidget:
        card = ElevatedCardWidget(self)
        layout = QVBoxLayout(card)
        layout.setContentsMargins(20, 18, 20, 18)
        layout.setSpacing(12)

        bar = QHBoxLayout()
        bar.setSpacing(12)
        self._adapters_title = SubtitleLabel(tr("home_adapters_title"), card)
        self.select_all_btn = PushButton(tr("home_select_all"), card)
        self.select_all_btn.clicked.connect(self.select_all_clicked.emit)
        self.deselect_all_btn = PushButton(tr("home_deselect_all"), card)
        self.deselect_all_btn.clicked.connect(self.deselect_all_clicked.emit)
        self.refresh_btn = TransparentToolButton(FluentIcon.SYNC, card)
        self.refresh_btn.setToolTip(tr("home_refresh_tip"))
        self.refresh_btn.clicked.connect(self.refresh_clicked.emit)
        bar.addWidget(self._adapters_title)
        bar.addStretch()
        bar.addWidget(self.select_all_btn)
        bar.addWidget(self.deselect_all_btn)
        bar.addWidget(self.refresh_btn)
        layout.addLayout(bar)

        self._rows_host = QWidget(card)
        self._rows_layout = QVBoxLayout(self._rows_host)
        self._rows_layout.setContentsMargins(0, 0, 0, 0)
        self._rows_layout.setSpacing(4)
        self._rows_layout.setAlignment(Qt.AlignTop)
        self._rows_scroll = SmoothScrollArea(card)
        self._rows_scroll.setWidgetResizable(True)
        self._rows_scroll.setAlignment(Qt.AlignTop)
        self._rows_scroll.setFrameShape(QFrame.NoFrame)
        self._rows_scroll.setStyleSheet("background: transparent;")
        # 列表随卡片可用空间伸缩；网卡较多时保留内部滚动，避免固定高度
        # 在少量网卡时留下生硬的大块空白。
        self._rows_scroll.setMinimumHeight(112)
        self._rows_scroll.setSizePolicy(QSizePolicy.Expanding, QSizePolicy.Expanding)
        self._rows_scroll.setWidget(self._rows_host)
        self._empty_label = BodyLabel(tr("home_no_adapters"), card)
        self._empty_label.setAlignment(Qt.AlignCenter)
        layout.addWidget(self._empty_label)
        layout.addWidget(self._rows_scroll, 1)
        return card

    def _build_metric_matrix(self) -> QHBoxLayout:
        row = QHBoxLayout()
        row.setSpacing(16)
        self.conn_metric = MetricCard("home_metric_connections", tr("home_metric_connections_value", value=0), self)
        self.traffic_metric = MetricCard("home_metric_accelerated", "0.00 MB", self)
        self.latency_metric = MetricCard("home_metric_latency", tr("home_metric_latency_value", value=0), self)
        self.kernel_metric = KernelMetricCard(self)
        for card in (self.conn_metric, self.traffic_metric, self.latency_metric, self.kernel_metric):
            row.addWidget(card, 1)
        return row

    def rebuild_cards(self, adapters: List[Dict], checked_aliases: List[str], schedule_weights: Dict[str, int] = None):
        for row in self._cards.values():
            row.setParent(None)
            row.deleteLater()
        self._cards.clear()
        while self._rows_layout.count():
            item = self._rows_layout.takeAt(0)
            widget = item.widget()
            if widget is not None:
                widget.setParent(None)

        weights = schedule_weights or {}
        checked_set = set(checked_aliases or [])
        for index, adapter in enumerate(adapters):
            alias = adapter.get("alias", "")
            weight = weights.get(alias, 1)
            row = AdapterRow(adapter, schedule_weight=int(weight or 1), parent=self._rows_host)
            if alias in checked_set:
                row.set_checked(True)
            row.checkbox.setEnabled(self._adapter_controls_enabled)
            row.toggled.connect(self.adapter_checked.emit)
            row.weight_changed.connect(self.adapter_weight_changed.emit)
            self._rows_layout.addWidget(row)
            self._cards[row.alias] = row
            if index < len(adapters) - 1:
                self._rows_layout.addWidget(HorizontalSeparator(self._rows_host))

        has_cards = bool(self._cards)
        self._empty_label.setVisible(not has_cards)
        self._rows_scroll.setVisible(has_cards)

    def set_all_checked(self, checked: bool):
        for row in self._cards.values():
            row.set_checked(checked)

    def set_card_checked(self, alias: str, checked: bool):
        row = self._cards.get(alias)
        if row is not None and row.is_checked() != checked:
            row.set_checked(checked)

    def _apply_hero_color(self):
        accent = themeColor().name()
        self._speed_value.setTextColor(accent, accent)
        self._speed_unit.setTextColor(accent, accent)

    def refresh_theme(self):
        self._apply_hero_color()
        for row in self._cards.values():
            row.refresh_theme()

    def update_total(self, down_mbps: float, up_mbps: float, connections: int):
        self._speed_value.setText(f"{down_mbps:.2f}")
        self._up_conn.setText(tr("home_up_conn", up=up_mbps, conn=connections))
        self.conn_metric.set_value(tr("home_metric_connections_value", value=connections))
        self._last_down_mbps = max(0.0, float(down_mbps or 0.0))
        self._last_up_mbps = max(0.0, float(up_mbps or 0.0))
        self._last_connections = int(connections or 0)
        if self._engine_running:
            self._accelerated_bytes += self._last_down_mbps * 1024 * 1024
            self.traffic_metric.set_value(self._format_bytes(self._accelerated_bytes))

    def update_telemetry(self, payload: Dict):
        for alias, row in self._cards.items():
            stats = payload.get(alias)
            if stats is None:
                row.reset_telemetry()
            else:
                row.update_telemetry(stats.get("down_mbps", 0.0), stats.get("connections", 0))

    def reset_telemetry(self):
        self._speed_value.setText("0.00")
        self._up_conn.setText(tr("home_up_conn", up=0.0, conn=0))
        self.conn_metric.set_value(tr("home_metric_connections_value", value=0))
        self._last_up_mbps = 0.0
        self._last_connections = 0
        for row in self._cards.values():
            row.reset_telemetry()

    def update_health(self, alias: str, status: str):
        row = self._cards.get(alias)
        if row is not None:
            row.update_health(status)

    def set_engine_state(self, running: bool, socks_port: int = 0, http_port: int = 0):
        was_running = self._engine_running
        self._engine_running = running
        if running and not was_running:
            self._accelerated_bytes = 0.0
            self.traffic_metric.set_value("0.00 MB")
        self.engine_switch.blockSignals(True)
        self.engine_switch.setChecked(running)
        self.engine_switch.blockSignals(False)
        self.engine_switch.setEnabled(not self._engine_busy)
        self.mode_segment.setEnabled(not running)
        self.set_adapter_controls_enabled(not running)
        self._engine_status.setText(tr("home_engine_switch_on" if running else "home_engine_switch_off"))
        self.kernel_metric.set_mode(self._current_mode, running)
        if socks_port and http_port:
            self._last_socks_port = socks_port
            self._last_http_port = http_port
            self._engine_ports.setText(tr("home_engine_ports", socks=socks_port, http=http_port))

    def set_adapter_controls_enabled(self, enabled: bool):
        self._adapter_controls_enabled = enabled
        self.select_all_btn.setEnabled(enabled)
        self.deselect_all_btn.setEnabled(enabled)
        self.refresh_btn.setEnabled(enabled)
        self._rows_scroll.setEnabled(enabled)
        self._rows_host.setEnabled(enabled)
        for row in self._cards.values():
            row.checkbox.setEnabled(enabled)

    def set_controls_enabled(self, enabled: bool):
        self.mode_segment.setEnabled(enabled)
        self.engine_switch.setEnabled(not self._engine_busy)
        self.set_adapter_controls_enabled(enabled)
        self._weighted_switch.setEnabled(enabled)

    def set_engine_busy(self, busy: bool):
        """启动/停止过程中锁住开关，同时明确展示加载态而非静默置灰。"""
        self._engine_busy = busy
        self.engine_switch.setEnabled(not busy)
        self._engine_loading.setVisible(busy)
        if busy:
            self._engine_status.setText(tr("home_engine_transitioning"))
        else:
            self._engine_status.setText(
                tr("home_engine_switch_on" if self._engine_running else "home_engine_switch_off")
            )

    def set_engine_startup_status(self, text: str):
        """更新虚拟网卡启动阶段文字；仅在加载态中覆盖状态。"""
        if self._engine_busy:
            self._engine_loading.setVisible(True)
            self._engine_status.setText(str(text))

    def _on_mode_changed(self, key: str):
        self._current_mode = key
        self.kernel_metric.set_mode(key, self._engine_running)
        self.mode_changed.emit(key)

    def _on_weighted_toggled(self, enabled: bool):
        state = tr("home_engine_switch_on" if enabled else "home_engine_switch_off")
        self._weighted_status.setText(state)
        self.weighted_toggled.emit(enabled)

    def current_mode(self) -> str:
        return self.mode_segment.currentRouteKey() or "tun"

    def set_mode(self, key: str):
        self._current_mode = key
        self.mode_segment.blockSignals(True)
        self.mode_segment.setCurrentItem(key)
        self.mode_segment.blockSignals(False)
        self.kernel_metric.set_mode(key, self._engine_running)

    def get_checked_aliases(self) -> List[str]:
        return [a for a, row in self._cards.items() if row.is_checked()]

    @staticmethod
    def _format_bytes(value: float) -> str:
        units = ["B", "KB", "MB", "GB", "TB"]
        size = float(max(0.0, value))
        for unit in units:
            if size < 1024 or unit == units[-1]:
                return f"{size:.2f} {unit}"
            size /= 1024
        return "0.00 B"

    def retranslate_ui(self):
        self._engine_title.setText(tr("home_engine_title"))
        self._mode_label.setText(tr("mode_label"))
        self.mode_segment.setItemText("proxy", tr("mode_proxy"))
        self.mode_segment.setItemText("tun", tr("mode_tun"))
        self._engine_label.setText(tr("home_engine_control"))
        self._engine_hint.setText(tr("home_engine_control_hint"))
        self.engine_switch.setOnText("")
        self.engine_switch.setOffText("")
        self.engine_switch.setToolTip("")
        if self._engine_busy:
            self._engine_status.setText(tr("home_engine_transitioning"))
        else:
            self._engine_status.setText(tr("home_engine_switch_on" if self.engine_switch.isChecked() else "home_engine_switch_off"))
        self._speed_caption.setText(tr("home_total_speed"))
        self._speed_unit.setText(tr("home_speed_unit"))
        self._up_conn.setText(tr("home_up_conn", up=self._last_up_mbps, conn=self._last_connections))
        self._adapters_title.setText(tr("home_adapters_title"))
        self.select_all_btn.setText(tr("home_select_all"))
        self.deselect_all_btn.setText(tr("home_deselect_all"))
        self.refresh_btn.setToolTip(tr("home_refresh_tip"))
        self._empty_label.setText(tr("home_no_adapters"))
        self._weighted_label.setText(tr("home_weighted_toggle"))
        self._weighted_hint.setText(tr("home_weighted_hint"))
        self._weighted_switch.setOnText("")
        self._weighted_switch.setOffText("")
        self._weighted_switch.setToolTip("")
        weighted_state = tr("home_engine_switch_on" if self._weighted_switch.isChecked() else "home_engine_switch_off")
        self._weighted_status.setText(weighted_state)
        self.conn_metric.set_value(tr("home_metric_connections_value", value=self._last_connections))
        self._engine_ports.setText(tr(
            "home_engine_ports",
            socks=self._last_socks_port,
            http=self._last_http_port,
        ))
        for card in (self.conn_metric, self.traffic_metric, self.latency_metric, self.kernel_metric):
            card.retranslate_ui()
        for row in self._cards.values():
            row.retranslate_ui()

    def set_weighted_scheduler(self, enabled: bool):
        self._weighted_switch.blockSignals(True)
        self._weighted_switch.setChecked(enabled)
        self._weighted_switch.blockSignals(False)
        state = tr("home_engine_switch_on" if enabled else "home_engine_switch_off")
        self._weighted_status.setText(state)

    def is_weighted_scheduler(self) -> bool:
        return self._weighted_switch.isChecked()

    def get_schedule_weights(self) -> Dict[str, int]:
        return {alias: row.schedule_weight for alias, row in self._cards.items()}
