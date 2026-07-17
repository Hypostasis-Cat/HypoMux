"""
HypoMux 网络体检页 (ToolsPage)

第三阶段 Fluent 换装 + 第四阶段深度打磨：
- 任务1：移除主标题下方灰色引导文字，保持洗练。
- 任务2：标题与体检按钮下方并入网卡选择 FlowLayout 卡片，
  其勾选状态与首页 HomePage 双向实时同步（经 MainWindow 信号中转）。

无缝接入第二阶段的 Rust 异步诊断内核 diagnostic.exe：
- PrimaryPushButton 触发体检，IndeterminateProgressRing 指示运行态
- 每张网卡一张 ElevatedCardWidget 结果卡：三色 InfoBadge + 丢包/延迟/抖动

纯视图层：用户意图经 Qt 信号上抛，数据经公开方法回填。
"""

from typing import Dict, List

from PySide6.QtCore import Qt, Signal
from PySide6.QtWidgets import (
    QWidget, QVBoxLayout, QHBoxLayout, QFrame, QSizePolicy,
)
from qfluentwidgets import (
    PrimaryPushButton, PushButton, TitleLabel, SubtitleLabel, BodyLabel,
    CaptionLabel, StrongBodyLabel, ElevatedCardWidget, InfoBadge, InfoLevel,
    IndeterminateProgressRing, IconWidget, FlowLayout,
    SingleDirectionScrollArea, TransparentToolButton, CheckBox, FluentIcon,
)

from ui.i18n import tr


_HEALTH_LEVEL = {
    "available": InfoLevel.SUCCESS,
    "unstable": InfoLevel.WARNING,
    "unavailable": InfoLevel.ERROR,
}


class SelectableNicCard(ElevatedCardWidget):
    """体检页用的网卡选择卡片（轻量版，仅勾选 + 名称 + IP）。

    勾选变化通过 toggled 信号上抛，由 MainWindow 做跨屏双向同步。
    """

    toggled = Signal(str, bool)   # (alias, checked)

    def __init__(self, adapter: Dict, parent=None):
        super().__init__(parent)
        self._alias = adapter.get("alias", "")
        self._ip = adapter.get("ip", "") or adapter.get("ipv4", "")
        self._is_ppp = bool(adapter.get("is_ppp", False))
        self.setFixedSize(300, 96)

        layout = QVBoxLayout(self)
        layout.setContentsMargins(20, 16, 20, 16)
        layout.setSpacing(8)

        top = QHBoxLayout()
        top.setSpacing(12)
        self.checkbox = CheckBox(self)
        self.checkbox.toggled.connect(self._on_toggled)

        nic_icon = FluentIcon.CONNECT if self._is_ppp else FluentIcon.WIFI
        self._icon = IconWidget(nic_icon, self)
        self._icon.setFixedSize(18, 18)

        self._name_label = StrongBodyLabel(self._alias, self)
        self._name_label.setSizePolicy(QSizePolicy.Expanding, QSizePolicy.Preferred)

        top.addWidget(self.checkbox)
        top.addWidget(self._icon)
        top.addWidget(self._name_label, 1)
        layout.addLayout(top)

        ip_text = self._ip
        if self._is_ppp:
            ip_text = f"{self._ip}  ·  {tr('home_card_ppp')}"
        self._ip_label = CaptionLabel(ip_text, self)
        layout.addWidget(self._ip_label)

    def _on_toggled(self, checked: bool):
        self.toggled.emit(self._alias, checked)

    def set_checked(self, checked: bool):
        self.checkbox.blockSignals(True)
        self.checkbox.setChecked(checked)
        self.checkbox.blockSignals(False)

    def is_checked(self) -> bool:
        return self.checkbox.isChecked()

    @property
    def alias(self) -> str:
        return self._alias


class DiagResultCard(ElevatedCardWidget):
    """单张网卡体检结果卡片。"""

    def __init__(self, result: Dict, parent=None):
        super().__init__(parent)
        name = result.get("name", result.get("src_ip", ""))
        ip = result.get("ip", result.get("src_ip", ""))
        status = result.get("status", "unavailable")
        loss = result.get("loss_rate", 100)
        latency = result.get("avg_latency_ms", 0)
        jitter = result.get("jitter_ms", 0)

        self.setFixedHeight(184)
        layout = QHBoxLayout(self)
        layout.setContentsMargins(20, 14, 20, 14)
        layout.setSpacing(16)

        icon = IconWidget(FluentIcon.WIFI, self)
        icon.setFixedSize(22, 22)
        layout.addWidget(icon)

        # 左：网卡名 + IP
        left = QVBoxLayout()
        left.setSpacing(4)
        left.addWidget(StrongBodyLabel(name, self))
        left.addWidget(CaptionLabel(ip, self))
        layout.addLayout(left)

        layout.addStretch()

        # 中：指标
        metrics = QVBoxLayout()
        metrics.setSpacing(4)
        metrics.addWidget(BodyLabel(
            f"{tr('diag_metric_loss')} {loss}%  ·  "
            f"{tr('diag_metric_latency')} {latency}ms  ·  "
            f"{tr('diag_metric_jitter')} {jitter}ms", self
        ))
        metrics.addWidget(CaptionLabel(tr(f"diag_desc_{status}"), self))
        checks = result.get("checks") or []
        if checks:
            check_lines = []
            for check in checks:
                key = str(check.get("key", ""))
                label = tr(f"diag_check_{key}")
                level = str(check.get("level", "warn"))
                marker = {"pass": "✓", "warn": "⚠", "fail": "✕"}.get(level, "•")
                detail = str(check.get("detail", "")).strip()
                if key == "metric" and detail:
                    metric_mode = "auto" if check.get("mode") == "auto" else "fixed"
                    detail = f"{tr(f'diag_check_metric_{metric_mode}')} {detail}"
                elif not detail:
                    detail = tr("diag_check_missing")
                check_lines.append(f"{marker} {label}: {detail}")
            checks_label = CaptionLabel("\n".join(check_lines), self)
            checks_label.setWordWrap(True)
            metrics.addWidget(checks_label)
        layout.addLayout(metrics)

        # 右：三色徽标
        level = _HEALTH_LEVEL.get(status, InfoLevel.INFOAMTION)
        badge = InfoBadge.make(tr(f"diag_status_{status}"), self, level=level)
        layout.addWidget(badge)


class ToolsPage(QWidget):
    """网络体检页。

    Signals:
        start_clicked()             用户点击「开始网卡体检」
        adapter_checked(str, bool)  单张网卡勾选变化（跨屏同步）
        refresh_clicked()           刷新网卡
    """

    start_clicked = Signal()
    adapter_checked = Signal(str, bool)
    refresh_clicked = Signal()

    def __init__(self, parent=None):
        super().__init__(parent)
        self.setObjectName("toolsPage")
        self._result_cards = []
        self._nic_cards: Dict[str, SelectableNicCard] = {}
        self._init_ui()

    def _init_ui(self):
        root = QVBoxLayout(self)
        # 大留白 + 干净大号加粗标题
        root.setContentsMargins(36, 28, 36, 28)
        root.setSpacing(8)

        # 任务1：仅保留干净大标题，移除下方灰色引导文字
        self._title = TitleLabel(tr("tools_title"), self)
        root.addWidget(self._title)
        root.addSpacing(20)

        # 控制栏：体检按钮 + 进度环
        bar = QHBoxLayout()
        bar.setSpacing(14)
        self.start_btn = PrimaryPushButton(FluentIcon.SPEED_HIGH, tr("tools_start"), self)
        self.start_btn.clicked.connect(self._on_start)
        self.progress_ring = IndeterminateProgressRing(self)
        self.progress_ring.setFixedSize(24, 24)
        self.progress_ring.setVisible(False)
        bar.addWidget(self.start_btn)
        bar.addWidget(self.progress_ring)
        bar.addStretch()
        root.addLayout(bar)
        root.addSpacing(20)

        # 任务2：网卡选择区（标题 + 全选/取消 + 刷新 + FlowLayout 卡片）
        sel_bar = QHBoxLayout()
        sel_bar.setSpacing(12)
        self._sel_title = SubtitleLabel(tr("home_adapters_title"), self)
        self.select_all_btn = PushButton(tr("home_select_all"), self)
        self.select_all_btn.clicked.connect(lambda: self._set_all(True))
        self.deselect_all_btn = PushButton(tr("home_deselect_all"), self)
        self.deselect_all_btn.clicked.connect(lambda: self._set_all(False))
        self.refresh_btn = TransparentToolButton(FluentIcon.SYNC, self)
        self.refresh_btn.setToolTip(tr("home_refresh_tip"))
        self.refresh_btn.clicked.connect(self.refresh_clicked.emit)
        sel_bar.addWidget(self._sel_title)
        sel_bar.addStretch()
        sel_bar.addWidget(self.select_all_btn)
        sel_bar.addWidget(self.deselect_all_btn)
        sel_bar.addWidget(self.refresh_btn)
        root.addLayout(sel_bar)
        root.addSpacing(4)

        self._nic_host = QWidget(self)
        self._nic_host.setStyleSheet("background: transparent;")
        self._nic_flow = FlowLayout(self._nic_host, needAni=False)
        self._nic_flow.setContentsMargins(0, 0, 0, 0)
        self._nic_flow.setHorizontalSpacing(16)
        self._nic_flow.setVerticalSpacing(16)
        self._nic_empty = BodyLabel(tr("home_no_adapters"), self)
        self._nic_empty.setAlignment(Qt.AlignCenter)
        root.addWidget(self._nic_empty)
        root.addWidget(self._nic_host)
        root.addSpacing(8)

        # 结果滚动区
        scroll = SingleDirectionScrollArea(self, orient=Qt.Vertical)
        scroll.setWidgetResizable(True)
        scroll.setFrameShape(QFrame.NoFrame)
        scroll.setStyleSheet("background: transparent;")

        self._result_host = QWidget()
        self._result_host.setStyleSheet("background: transparent;")
        self._result_layout = QVBoxLayout(self._result_host)
        self._result_layout.setContentsMargins(0, 8, 0, 8)
        self._result_layout.setSpacing(12)

        self._empty_label = BodyLabel(tr("tools_no_result"), self._result_host)
        self._empty_label.setAlignment(Qt.AlignCenter)
        self._result_layout.addWidget(self._empty_label)
        self._result_layout.addStretch()

        scroll.setWidget(self._result_host)
        root.addWidget(scroll, 1)

    def _on_start(self):
        self.start_clicked.emit()

    def _set_all(self, checked: bool):
        """全选/取消：逐卡发信号交由 MainWindow 做权威同步。"""
        for card in self._nic_cards.values():
            if card.is_checked() != checked:
                card.set_checked(checked)
                self.adapter_checked.emit(card.alias, checked)

    # ========== 任务2：网卡选择卡片 API ==========
    def rebuild_cards(self, adapters: List[Dict], checked_aliases: List[str]):
        """与首页同源重建网卡选择卡片，并恢复勾选。"""
        for card in self._nic_cards.values():
            self._nic_flow.removeWidget(card)
            card.deleteLater()
        self._nic_cards.clear()

        checked_set = set(checked_aliases or [])
        for adapter in adapters:
            card = SelectableNicCard(adapter, self._nic_host)
            if adapter.get("alias", "") in checked_set:
                card.set_checked(True)
            card.toggled.connect(self.adapter_checked.emit)
            self._nic_flow.addWidget(card)
            self._nic_cards[card.alias] = card

        has_cards = bool(self._nic_cards)
        self._nic_empty.setVisible(not has_cards)
        self._nic_host.setVisible(has_cards)

    def set_card_checked(self, alias: str, checked: bool):
        """跨屏同步：按别名设置勾选态（不回抛信号）。"""
        card = self._nic_cards.get(alias)
        if card is not None and card.is_checked() != checked:
            card.set_checked(checked)

    def set_all_checked(self, checked: bool):
        for card in self._nic_cards.values():
            card.set_checked(checked)

    def set_controls_enabled(self, enabled: bool):
        """加速运行期间禁止改动勾选。"""
        self.select_all_btn.setEnabled(enabled)
        self.deselect_all_btn.setEnabled(enabled)
        self.refresh_btn.setEnabled(enabled)
        for card in self._nic_cards.values():
            card.checkbox.setEnabled(enabled)

    # ========== 体检结果 API ==========
    def begin_running(self):
        """进入体检中状态：禁用按钮、显示进度环、清空旧结果。"""
        self.start_btn.setEnabled(False)
        self.progress_ring.setVisible(True)
        self.start_btn.setText(tr("tools_running"))
        self._clear_results()

    def add_result(self, result: Dict):
        """逐张网卡结果回填。"""
        self._empty_label.setVisible(False)
        card = DiagResultCard(result, self._result_host)
        self._result_layout.insertWidget(self._result_layout.count() - 1, card)
        self._result_cards.append(card)

    def end_running(self):
        """体检结束：恢复按钮、隐藏进度环。"""
        self.start_btn.setEnabled(True)
        self.progress_ring.setVisible(False)
        self.start_btn.setText(tr("tools_start"))
        if not self._result_cards:
            self._empty_label.setVisible(True)

    def _clear_results(self):
        for card in self._result_cards:
            self._result_layout.removeWidget(card)
            card.deleteLater()
        self._result_cards.clear()

    def retranslate_ui(self):
        self._title.setText(tr("tools_title"))
        if self.start_btn.isEnabled():
            self.start_btn.setText(tr("tools_start"))
        self._sel_title.setText(tr("home_adapters_title"))
        self.select_all_btn.setText(tr("home_select_all"))
        self.deselect_all_btn.setText(tr("home_deselect_all"))
        self.refresh_btn.setToolTip(tr("home_refresh_tip"))
        self._nic_empty.setText(tr("home_no_adapters"))
        self._empty_label.setText(tr("tools_no_result"))
