"""
HypoMux 单网卡被墙域名页 (BlockedDomainsPage) - 第五个导航选项卡

展示每张网卡上被确认无法访问的域名清单，提供启用/关闭自动规避、
删除单条记录、清空全部等管理功能。
"""

from PySide6.QtCore import Qt, Signal, QTimer
from PySide6.QtWidgets import QWidget, QVBoxLayout, QHBoxLayout, QHeaderView
from qfluentwidgets import (
    TableWidget, TitleLabel, BodyLabel, PushButton, TransparentPushButton,
    SwitchSettingCard, FluentIcon,
)

from ui.i18n import tr
from utils.blocked_domain_tracker import get_tracker


class BlockedDomainsPage(QWidget):
    """单网卡被墙域名管理页。"""

    settings_changed = Signal()

    COL_NIC = 0
    COL_DOMAIN = 1
    COL_ACTION = 2
    ROW_HEIGHT = 40

    def __init__(self, parent=None):
        super().__init__(parent)
        self.setObjectName("blockedDomainsPage")
        self._controls_enabled = True
        self._init_ui()

        # 加速运行期间定时自动刷新，实时展示后台确认的被墙域名
        self._auto_refresh_timer = QTimer(self)
        self._auto_refresh_timer.setInterval(3000)
        self._auto_refresh_timer.timeout.connect(self._on_auto_refresh)

        # 首次显示时加载数据
        self._load_data()

    def _init_ui(self):
        root = QVBoxLayout(self)
        root.setContentsMargins(24, 24, 24, 24)
        root.setSpacing(16)

        self._title = TitleLabel(tr("blocked_title"), self)
        self._hint = BodyLabel(tr("blocked_hint"), self)
        self._hint.setWordWrap(True)
        root.addWidget(self._title)
        root.addWidget(self._hint)

        # 开关卡片
        tracker = get_tracker()
        self._enable_card = SwitchSettingCard(
            FluentIcon.COMPLETED,
            tr("blocked_enable"),
            tr("blocked_enable_hint"),
            parent=self,
        )
        self._enable_card.setChecked(tracker.enabled)
        self._enable_card.checkedChanged.connect(self._on_enable_changed)
        root.addWidget(self._enable_card)

        self._expiry_card = SwitchSettingCard(
            FluentIcon.HISTORY,
            tr("blocked_expiry_toggle"),
            tr("blocked_expiry_hint"),
            parent=self,
        )
        self._expiry_card.setChecked(tracker.use_expiry)
        self._expiry_card.checkedChanged.connect(self._on_expiry_changed)
        root.addWidget(self._expiry_card)

        # 工具栏
        self._toolbar = QHBoxLayout()
        self._toolbar.setSpacing(12)
        self._clear_all_btn = PushButton(FluentIcon.DELETE, tr("blocked_clear_all"), self)
        self._clear_all_btn.clicked.connect(self._on_clear_all)
        self._refresh_btn = PushButton(FluentIcon.SYNC, tr("blocked_refresh"), self)
        self._refresh_btn.clicked.connect(self._load_data)
        self._toolbar.addWidget(self._clear_all_btn)
        self._toolbar.addWidget(self._refresh_btn)
        self._toolbar.addStretch()
        root.addLayout(self._toolbar)

        # 空数据提示
        self._empty_hint = BodyLabel(tr("blocked_no_data"), self)
        self._empty_hint.setAlignment(Qt.AlignCenter)
        self._empty_hint.setStyleSheet("color: #888888; padding: 60px;")

        # 表格
        self.tableWidget = TableWidget(self)
        self.table = self.tableWidget
        self.tableWidget.setBorderVisible(True)
        self.tableWidget.setBorderRadius(8)
        self.tableWidget.setWordWrap(False)
        self.tableWidget.setColumnCount(3)
        self.tableWidget.setRowCount(0)
        self.tableWidget.verticalHeader().hide()
        self.tableWidget.verticalHeader().setDefaultSectionSize(self.ROW_HEIGHT)
        self.tableWidget.setSelectionBehavior(TableWidget.SelectRows)

        header = self.tableWidget.horizontalHeader()
        header.setSectionResizeMode(self.COL_NIC, QHeaderView.Fixed)
        header.setSectionResizeMode(self.COL_DOMAIN, QHeaderView.Stretch)
        header.setSectionResizeMode(self.COL_ACTION, QHeaderView.Fixed)
        self.tableWidget.setColumnWidth(self.COL_NIC, 180)
        self.tableWidget.setColumnWidth(self.COL_ACTION, 120)

        self._apply_headers()
        root.addWidget(self._empty_hint, 1)
        root.addWidget(self.tableWidget, 1)
        self.tableWidget.hide()

    def _apply_headers(self):
        self.tableWidget.setHorizontalHeaderLabels([
            tr("blocked_nic_label"),
            tr("routing_col_target"),
            "",
        ])

    # ----- 数据加载 -----
    def _load_data(self):
        tracker = get_tracker()
        data = tracker.all_blocked()
        self.tableWidget.setRowCount(0)

        if not data:
            self.tableWidget.hide()
            self._empty_hint.show()
            return

        self._empty_hint.hide()
        self.tableWidget.show()

        use_expiry = tracker.use_expiry
        for nic_name, domains in data.items():
            for domain in domains:
                if not use_expiry:
                    display_text = f"{domain} ({tr('blocked_permanent')})"
                else:
                    remaining = tracker.remaining_seconds(nic_name, domain)
                    if remaining >= 60:
                        display_text = f"{domain} ({tr('blocked_expire_min', min=remaining // 60)})"
                    else:
                        display_text = f"{domain} ({tr('blocked_expire_sec', sec=remaining)})"
                self._add_row(nic_name, domain, display_text)

    def _add_row(self, nic_name: str, domain: str, display_text: str):
        row = self.tableWidget.rowCount()
        self.tableWidget.insertRow(row)
        self.tableWidget.setRowHeight(row, self.ROW_HEIGHT)

        nic_label = BodyLabel(nic_name, self.tableWidget)
        nic_label.setAlignment(Qt.AlignVCenter | Qt.AlignLeft)
        nic_label.setContentsMargins(8, 0, 0, 0)
        self.tableWidget.setCellWidget(row, self.COL_NIC, nic_label)

        domain_label = BodyLabel(display_text, self.tableWidget)
        domain_label.setAlignment(Qt.AlignVCenter | Qt.AlignLeft)
        domain_label.setContentsMargins(4, 0, 0, 0)
        self.tableWidget.setCellWidget(row, self.COL_DOMAIN, domain_label)

        container = QWidget(self.tableWidget)
        container_layout = QHBoxLayout(container)
        container_layout.setContentsMargins(4, 4, 4, 4)
        container_layout.setSpacing(4)

        remove_btn = TransparentPushButton(FluentIcon.CLOSE, tr("blocked_delete_domain"), container)
        remove_btn.clicked.connect(lambda _checked=False, n=nic_name, d=domain: self._on_remove_domain(n, d))
        container_layout.addWidget(remove_btn)
        container_layout.addStretch()
        self.tableWidget.setCellWidget(row, self.COL_ACTION, container)

    # ----- 交互 -----
    def _on_enable_changed(self, checked: bool):
        tracker = get_tracker()
        tracker.enabled = checked
        tracker.save()
        self.settings_changed.emit()

    def _on_expiry_changed(self, checked: bool):
        tracker = get_tracker()
        tracker.use_expiry = checked
        tracker.save()
        self.settings_changed.emit()
        self._load_data()

    def _on_remove_domain(self, nic_name: str, domain: str):
        get_tracker().remove_domain(nic_name, domain)
        get_tracker().save()
        self._load_data()

    def _on_clear_all(self):
        get_tracker().clear_all()
        get_tracker().save()
        self._load_data()

    def _on_auto_refresh(self):
        # 仅在本页可见时刷新，被切走时跳过以省资源
        if self.isVisible():
            self._load_data()

    def showEvent(self, event):
        # 切换到本页时立即刷新，确保看到最新的被墙记录
        super().showEvent(event)
        self._load_data()

    # ----- 状态机 -----
    def set_controls_enabled(self, enabled: bool):
        """运行中锁死编辑入口，停止后恢复。"""
        self._controls_enabled = enabled
        self._enable_card.setEnabled(enabled)
        self._expiry_card.setEnabled(enabled)
        self._clear_all_btn.setEnabled(enabled)
        self.tableWidget.setEnabled(enabled)
        # enabled=False 表示加速引擎运行中：开启自动刷新；enabled=True：停止时关闭
        if not enabled:
            self._load_data()
            if not self._auto_refresh_timer.isActive():
                self._auto_refresh_timer.start()
        else:
            self._auto_refresh_timer.stop()
            self._load_data()

    def retranslate_ui(self):
        self._title.setText(tr("blocked_title"))
        self._hint.setText(tr("blocked_hint"))
        self._enable_card.titleLabel.setText(tr("blocked_enable"))
        self._enable_card.contentLabel.setText(tr("blocked_enable_hint"))
        self._expiry_card.titleLabel.setText(tr("blocked_expiry_toggle"))
        self._expiry_card.contentLabel.setText(tr("blocked_expiry_hint"))
        self._clear_all_btn.setText(tr("blocked_clear_all"))
        self._refresh_btn.setText(tr("blocked_refresh"))
        self._empty_hint.setText(tr("blocked_no_data"))
        self._apply_headers()

    def refresh_theme(self):
        """主题切换时重设表格样式。"""
        pass
