"""
HypoMux 路由规则页 (RoutingPage) - 进程/域名/IP 分流规则编辑器

用户在表格中维护「匹配条件 -> 出口通道」规则，MainWindow 读取后动态
序列化为 sing-box route.rules。页面只负责视图、进程选择和规则数据回吐，
不直接触碰代理内核线程，避免破坏既有单端口、多端口、聚合引擎信号链。
"""

from __future__ import annotations

import csv
import subprocess
from io import StringIO
from typing import Any, Iterable, List, Optional

from PySide6.QtCore import Qt, QEvent, Signal, QThread, Slot
from PySide6.QtWidgets import (
    QWidget, QVBoxLayout, QHBoxLayout, QHeaderView, QStackedWidget,
)
from qfluentwidgets import (
    TableWidget, TitleLabel, BodyLabel, PushButton, TransparentPushButton,
    LineEdit, ComboBox, FluentIcon, MessageBoxBase, SearchLineEdit, ListWidget,
    SubtitleLabel, CaptionLabel, PrimaryPushButton, SegmentedWidget,
)

from ui.components import SurfaceCardWidget, register_content_card_control
from ui.i18n import tr
from ui.popup_material import apply_mica_popup
from utils.routing_rules import (
    MATCH_DOMAIN,
    MATCH_IP,
    MATCH_PROCESS,
    VALID_MATCH_TYPES,
    expand_routing_rule,
    normalize_match_value,
    normalize_routing_rules,
    routing_rule_identity,
    routing_rule_sort_key,
    routing_rule_value,
)
from qfluentwidgets.common.color import FluentSystemColor


_CREATE_NO_WINDOW = getattr(subprocess, "CREATE_NO_WINDOW", 0x08000000)
ROUTING_BACKUP_FORMAT = "hypomux-routing-rules"
ROUTING_BACKUP_VERSION = 2


def parse_routing_rules_backup(payload: Any) -> list:
    """校验并规整导入的分流规则备份，失败时不修改现有规则表。"""
    if isinstance(payload, list):
        raw_rules = payload
    elif isinstance(payload, dict):
        format_name = payload.get("format")
        if format_name not in (None, ROUTING_BACKUP_FORMAT):
            raise ValueError("unsupported backup format")
        if format_name == ROUTING_BACKUP_FORMAT:
            try:
                version = int(payload.get("version", 0))
            except (TypeError, ValueError):
                version = 0
            if version not in (1, ROUTING_BACKUP_VERSION):
                raise ValueError("unsupported backup version")
        raw_rules = payload.get("rules", payload.get("routing_rules"))
    else:
        raw_rules = None

    if not isinstance(raw_rules, list):
        raise ValueError("rules must be a list")

    rules = []
    seen = set()
    for index, item in enumerate(raw_rules, start=1):
        if not isinstance(item, dict):
            raise ValueError(f"invalid rule at index {index}")
        expanded = expand_routing_rule(item)
        if not expanded:
            raise ValueError(f"invalid routing rule at index {index}")
        for rule in expanded:
            identity = routing_rule_identity(
                rule["match_type"], routing_rule_value(rule)
            )
            if identity in seen:
                continue
            seen.add(identity)
            rules.append(rule)
    return sorted(rules, key=routing_rule_sort_key)


def _decode_process_output(raw: bytes) -> str:
    """兼容 Windows 本地代码页与 UTF-8 的子进程输出解码。"""
    for encoding in ("utf-8", "mbcs", "gbk"):
        try:
            return raw.decode(encoding)
        except Exception:
            continue
    return raw.decode("utf-8", errors="replace")


def _parse_tasklist_csv(text: str) -> List[str]:
    """从 tasklist CSV 输出中提取去重后的 .exe 进程名。"""
    names = set()
    reader = csv.reader(StringIO(text))
    for row in reader:
        if not row:
            continue
        name = str(row[0]).strip().strip('"')
        if not name or not name.lower().endswith(".exe"):
            continue
        if any(ch in name for ch in ("/", "\\", ":", "\0")):
            continue
        names.add(name)
    return sorted(names, key=str.lower)


class ProcessListWorker(QThread):
    """后台读取当前运行中的 Windows 进程列表。"""

    result_ready = Signal(list)
    failed = Signal(str)

    def run(self):
        try:
            proc = subprocess.Popen(
                "tasklist /NH /FO CSV",
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                stdin=subprocess.DEVNULL,
                shell=True,
                creationflags=_CREATE_NO_WINDOW,
            )
            stdout, stderr = proc.communicate(timeout=8)
            if proc.returncode not in (0, None):
                message = _decode_process_output(stderr or stdout).strip()
                self.failed.emit(message or "tasklist failed")
                return
            self.result_ready.emit(_parse_tasklist_csv(_decode_process_output(stdout)))
        except Exception as e:
            self.failed.emit(str(e))


class ProcessSelectDialog(MessageBoxBase):
    """运行中进程搜索选择对话框。"""

    def __init__(self, processes: List[str], parent=None):
        super().__init__(parent)
        apply_mica_popup(self)
        self._all_processes = list(processes or [])
        self._selected_process = ""

        self.widget.setFixedWidth(520)
        self._title = SubtitleLabel(tr("routing_process_dialog_title"), self.widget)
        self.search_edit = SearchLineEdit(self.widget)
        self.search_edit.setPlaceholderText(tr("routing_process_search_placeholder"))
        self.process_list = ListWidget(self.widget)
        self.process_list.setMinimumHeight(360)
        self._empty_label = BodyLabel(tr("routing_process_empty"), self.widget)
        self._empty_label.setAlignment(Qt.AlignCenter)

        self.viewLayout.addWidget(self._title)
        self.viewLayout.addWidget(self.search_edit)
        self.viewLayout.addWidget(self.process_list)
        self.viewLayout.addWidget(self._empty_label)

        self.yesButton.setText(tr("routing_dialog_ok"))
        self.cancelButton.setText(tr("routing_dialog_cancel"))

        self.search_edit.textChanged.connect(self._filter_processes)
        self.process_list.itemDoubleClicked.connect(self._on_item_double_clicked)
        self._filter_processes("")

    def _filter_processes(self, keyword: str):
        keyword = (keyword or "").strip().lower()
        self.process_list.clear()
        matched = [
            name for name in self._all_processes
            if not keyword or keyword in name.lower()
        ]
        self.process_list.addItems(matched)
        has_items = bool(matched)
        self.process_list.setVisible(has_items)
        self._empty_label.setVisible(not has_items)
        if has_items:
            self.process_list.setCurrentRow(0)

    def _on_item_double_clicked(self, item):
        if item is not None:
            self._selected_process = item.text().strip()
            self.accept()

    def selected_process(self) -> str:
        item = self.process_list.currentItem()
        if item is not None:
            return item.text().strip()
        return self._selected_process

    def validate(self) -> bool:
        self._selected_process = self.selected_process()
        return bool(self._selected_process)


class RoutingPage(QWidget):
    """进程、目标 IP/CIDR 与域名分流规则管理页。"""

    rules_changed = Signal()
    duplicate_detected = Signal(str)
    export_requested = Signal()
    import_requested = Signal()

    COL_VALUE = 0
    COL_OUTBOUND = 1
    ROW_HEIGHT = 38

    def __init__(self, parent=None):
        super().__init__(parent)
        self.setObjectName("routingPage")
        self._available_aliases: List[str] = []
        self._controls_enabled = True
        self._shutting_down = False
        self._sorting_rules = False
        self._current_match_type = MATCH_PROCESS
        self._tables = {}
        self._process_worker: Optional[ProcessListWorker] = None
        self._init_ui()

    def _init_ui(self):
        root = QVBoxLayout(self)
        root.setContentsMargins(24, 24, 24, 24)
        root.setSpacing(16)

        self._title = TitleLabel(tr("routing_title"), self)
        self._hint = BodyLabel(tr("routing_hint"), self)
        self._hint.setWordWrap(True)
        root.addWidget(self._title)
        root.addWidget(self._hint)

        # 将操作收纳进卡片，避免少量按钮散落在整行两端。
        self._toolbar_card = SurfaceCardWidget(self)
        self._toolbar = QHBoxLayout(self._toolbar_card)
        self._toolbar.setContentsMargins(14, 10, 14, 10)
        self._toolbar.setSpacing(10)
        self.add_btn = PrimaryPushButton(FluentIcon.ADD, tr("routing_add"), self._toolbar_card)
        self.add_btn.clicked.connect(self._on_add_rule)
        self.select_process_btn = PushButton(
            FluentIcon.APPLICATION, tr("routing_select_process"), self._toolbar_card
        )
        self.select_process_btn.clicked.connect(self._on_select_process)
        self.remove_btn = TransparentPushButton(
            FluentIcon.DELETE, tr("routing_remove"), self._toolbar_card
        )
        self.remove_btn.clicked.connect(self._on_remove_selected)
        self._backup_label = CaptionLabel(tr("routing_backup_group"), self._toolbar_card)
        self.export_btn = PushButton(tr("routing_export"), self._toolbar_card)
        self.export_btn.clicked.connect(self.export_requested.emit)
        self.import_btn = PushButton(tr("routing_import"), self._toolbar_card)
        self.import_btn.clicked.connect(self.import_requested.emit)

        self._toolbar.addWidget(self.add_btn)
        self._toolbar.addWidget(self.select_process_btn)
        self._toolbar.addWidget(self.remove_btn)
        self._toolbar.addStretch()
        self._toolbar.addWidget(self._backup_label)
        self._toolbar.addWidget(self.export_btn)
        self._toolbar.addWidget(self.import_btn)
        root.addWidget(self._toolbar_card)

        table_bar = QHBoxLayout()
        table_bar.setContentsMargins(2, 4, 2, 0)
        self._list_title = SubtitleLabel(tr("routing_list_title"), self)
        self._rule_count = CaptionLabel("", self)
        table_bar.addWidget(self._list_title)
        table_bar.addWidget(self._rule_count)
        table_bar.addStretch()
        self.rule_segment = SegmentedWidget(self)
        self.rule_segment.addItem(MATCH_PROCESS, tr("routing_tab_process"))
        self.rule_segment.addItem(MATCH_DOMAIN, tr("routing_tab_domain"))
        self.rule_segment.addItem(MATCH_IP, tr("routing_tab_ip"))
        self.rule_segment.setCurrentItem(MATCH_PROCESS)
        self.rule_segment.currentItemChanged.connect(self._on_rule_tab_changed)
        table_bar.addWidget(self.rule_segment)
        root.addLayout(table_bar)

        self._table_stack = QStackedWidget(self)
        for match_type in VALID_MATCH_TYPES:
            table = self._create_rule_table(match_type)
            self._tables[match_type] = table
            self._table_stack.addWidget(table)
        self.tableWidget = self._tables[MATCH_PROCESS]
        self.table = self.tableWidget

        self._duplicate_hint = BodyLabel("", self)
        self._duplicate_hint.setWordWrap(True)
        self._apply_theme_colors()
        self._duplicate_hint.hide()
        root.addWidget(self._duplicate_hint)
        # 规则表填满页面剩余空间；规则较多时由表格自身滚动，而非把下半页留白。
        root.addWidget(self._table_stack, 1)
        self._on_rule_tab_changed(MATCH_PROCESS)
        self._update_rule_count()

    def _create_rule_table(self, match_type: str) -> TableWidget:
        table = TableWidget(self)
        table.setBorderVisible(True)
        table.setBorderRadius(8)
        table.setWordWrap(False)
        table.setColumnCount(2)
        table.setRowCount(0)
        table.setMinimumHeight(260)
        table.verticalHeader().hide()
        table.verticalHeader().setDefaultSectionSize(self.ROW_HEIGHT)
        table.setSelectionBehavior(TableWidget.SelectRows)
        # 内嵌输入框时关闭整行悬停追踪，避免悬停看起来像选择。
        table.setMouseTracking(False)
        table.viewport().setMouseTracking(False)
        self._apply_headers(table, match_type)
        header = table.horizontalHeader()
        header.setSectionResizeMode(self.COL_VALUE, QHeaderView.Stretch)
        header.setSectionResizeMode(self.COL_OUTBOUND, QHeaderView.Stretch)
        return table

    def _apply_headers(self, table: TableWidget, match_type: str):
        value_key = {
            MATCH_PROCESS: "routing_col_process",
            MATCH_DOMAIN: "routing_col_domain",
            MATCH_IP: "routing_col_ip",
        }[match_type]
        table.setHorizontalHeaderLabels([
            tr(value_key),
            tr("routing_col_nic"),
        ])

    def _on_rule_tab_changed(self, match_type: str):
        if match_type not in self._tables:
            return
        self._current_match_type = match_type
        table = self._tables[match_type]
        self._table_stack.setCurrentWidget(table)
        self.tableWidget = table
        self.table = table
        self.select_process_btn.setVisible(match_type == MATCH_PROCESS)
        self._update_rule_count()

    def _apply_theme_colors(self):
        """使用 qfluentwidgets 的语义色，避免深色主题下错误提示变暗。"""
        light, dark = FluentSystemColor.CRITICAL_FOREGROUND.value
        self._duplicate_hint.setTextColor(light, dark)

    def refresh_theme(self):
        self._apply_theme_colors()

    # ---------- 网卡出口选项 ----------
    def set_available_adapters(self, adapters: Iterable):
        """注入当前扫描到的真实网卡别名，并刷新已有下拉框。"""
        aliases: List[str] = []
        seen = set()
        for item in adapters or []:
            if isinstance(item, dict):
                alias = str(item.get("alias") or item.get("name") or "").strip()
            else:
                alias = str(item).strip()
            if not alias or alias in seen:
                continue
            seen.add(alias)
            aliases.append(alias)
        self._available_aliases = aliases
        self._refresh_outbound_combos()

    def _make_outbound_combo(
        self, table: TableWidget, current: str = "aggregation"
    ) -> ComboBox:
        combo = ComboBox(table)
        self._fill_outbound_combo(combo, current)
        combo.currentIndexChanged.connect(lambda _i: self.rules_changed.emit())
        combo.installEventFilter(self)
        combo.setEnabled(self._controls_enabled)
        return combo

    def _fill_outbound_combo(self, combo: ComboBox, current: str = "aggregation"):
        combo.blockSignals(True)
        combo.clear()
        combo.addItem(tr("routing_outbound_aggregation"), userData="aggregation")
        for alias in self._available_aliases:
            combo.addItem(alias, userData=f"nic_{alias}")
        if current.startswith("nic_") and combo.findData(current) < 0:
            combo.addItem(current[4:], userData=current)
        combo.addItem(tr("routing_outbound_direct"), userData="direct")
        idx = combo.findData(current)
        combo.setCurrentIndex(idx if idx >= 0 else 0)
        combo.blockSignals(False)

    def _refresh_outbound_combos(self):
        for _match_type, _table, _row, _edit, combo in self._rule_rows():
            current = combo.currentData() or "aggregation"
            self._fill_outbound_combo(combo, current)
            combo.setEnabled(self._controls_enabled)

    @staticmethod
    def _placeholder_key(match_type: str) -> str:
        return {
            MATCH_DOMAIN: "routing_placeholder_domain",
            MATCH_IP: "routing_placeholder_ip",
        }.get(match_type, "routing_placeholder_process")

    # ---------- 行构建 ----------
    def _insert_row(
        self,
        match_type: str = MATCH_PROCESS,
        value: str = "",
        outbound: str = "aggregation",
    ):
        if match_type not in self._tables:
            match_type = MATCH_PROCESS
        table = self._tables[match_type]
        row = table.rowCount()
        table.insertRow(row)
        table.setRowHeight(row, self.ROW_HEIGHT)

        edit = LineEdit(table)
        register_content_card_control(edit)
        edit.setPlaceholderText(tr(self._placeholder_key(match_type)))
        edit.setText(value)
        edit.textChanged.connect(self._on_rule_value_changed)
        # 输入完成后再排序，避免用户每敲一个字符就跳动行位置。
        edit.editingFinished.connect(
            lambda kind=match_type: self._sort_rules(kind)
        )
        edit.installEventFilter(self)
        edit.setEnabled(self._controls_enabled)
        table.setCellWidget(row, self.COL_VALUE, edit)

        combo = self._make_outbound_combo(table, outbound)
        register_content_card_control(combo)
        table.setCellWidget(row, self.COL_OUTBOUND, combo)
        self._update_rule_state()
        self._update_rule_count()

    def _update_rule_count(self):
        self._rule_count.setText(
            tr("routing_rule_count", count=self.tableWidget.rowCount())
        )

    def eventFilter(self, watched, event):
        """只有实际点击单元格控件时才选择所在行，鼠标经过不改变选择。"""
        if self._shutting_down:
            return super().eventFilter(watched, event)
        if event.type() == QEvent.MouseButtonPress:
            for _kind, table, row, edit, outbound in self._rule_rows():
                if watched in (edit, outbound):
                    table.selectRow(row)
                    break
        return super().eventFilter(watched, event)

    def _rule_rows(self, match_type: Optional[str] = None):
        match_types = (
            (match_type,) if match_type in self._tables else VALID_MATCH_TYPES
        )
        for kind in match_types:
            table = self._tables[kind]
            for row in range(table.rowCount()):
                edit = table.cellWidget(row, self.COL_VALUE)
                outbound = table.cellWidget(row, self.COL_OUTBOUND)
                if edit is not None and outbound is not None:
                    yield kind, table, row, edit, outbound

    def _update_rule_state(self) -> set:
        counts = {}
        rows = []
        invalid_values = []
        for match_type, _table, row, edit, _outbound in self._rule_rows():
            raw_value = edit.text().strip()
            normalized = normalize_match_value(match_type, raw_value)
            identity = (
                routing_rule_identity(match_type, normalized)
                if normalized is not None else None
            )
            if identity is not None:
                counts[identity] = counts.get(identity, 0) + 1
            elif raw_value:
                invalid_values.append(raw_value)
            rows.append((match_type, row, edit, raw_value, identity))

        duplicates = {identity for identity, count in counts.items() if count > 1}
        duplicate_values = []
        seen = set()
        for _match_type, _row, edit, raw_value, identity in rows:
            invalid = bool(raw_value and identity is None)
            duplicated = identity in duplicates if identity is not None else False
            edit.setError(invalid or duplicated)
            if duplicated and identity not in seen:
                seen.add(identity)
                duplicate_values.append(raw_value)

        messages = []
        if duplicate_values:
            messages.append(tr(
                "routing_duplicate_hint", names=", ".join(duplicate_values)
            ))
        if invalid_values:
            messages.append(tr(
                "routing_invalid_hint", values=", ".join(invalid_values[:5])
            ))
        if messages:
            self._duplicate_hint.setText("\n".join(messages))
            self._duplicate_hint.show()
        else:
            self._duplicate_hint.clear()
            self._duplicate_hint.hide()
        return duplicates

    def _on_rule_value_changed(self, _text: str):
        if self._shutting_down:
            return
        self._update_rule_state()
        self.rules_changed.emit()

    def _sort_rules(self, match_type: Optional[str] = None):
        """排序当前类型的规则，同时保留当前编辑/选中的规则。"""
        match_type = (
            match_type if match_type in self._tables else self._current_match_type
        )
        table = self._tables[match_type]
        if self._shutting_down or self._sorting_rules or table.rowCount() < 2:
            return

        rows = []
        selected_identities = set()
        focused_identity = None
        selected_rows = {index.row() for index in table.selectedIndexes()}
        for _kind, _table, row, edit, combo in self._rule_rows(match_type):
            value = edit.text().strip()
            identity = routing_rule_identity(match_type, value)
            if row in selected_rows:
                selected_identities.add(identity)
            if edit.hasFocus():
                focused_identity = identity
            rows.append((row, value, combo.currentData() or "aggregation"))

        def row_sort_key(item):
            old_row, value, outbound = item
            expanded = expand_routing_rule({
                "match_type": match_type,
                "value": value,
                "outbound": outbound,
            })
            if not expanded:
                return (1, value.casefold(), old_row)
            return (0, *routing_rule_sort_key(expanded[0]), old_row)

        # 空白/非法的新规则始终排在末尾；有效规则按明确优先级排序。
        sorted_rows = sorted(
            rows,
            key=row_sort_key,
        )
        if rows == sorted_rows:
            return

        self._sorting_rules = True
        table.setUpdatesEnabled(False)
        try:
            table.setRowCount(0)
            for _old_row, value, outbound in sorted_rows:
                self._insert_row(match_type, value, outbound)
        finally:
            table.setUpdatesEnabled(True)
            self._sorting_rules = False

        for _kind, _table, row, edit, _outbound in self._rule_rows(match_type):
            identity = routing_rule_identity(match_type, edit.text())
            if identity == focused_identity and focused_identity is not None:
                self._focus_rule_row(match_type, row)
                break
            if identity in selected_identities:
                table.selectRow(row)
        self._update_rule_state()
        self._update_rule_count()

    def _find_process_row(self, process_name: str) -> int:
        target = routing_rule_identity(MATCH_PROCESS, process_name)
        if not target:
            return -1
        for _kind, _table, row, edit, _outbound in self._rule_rows(MATCH_PROCESS):
            if routing_rule_identity(MATCH_PROCESS, edit.text()) == target:
                return row
        return -1

    def _focus_rule_row(self, match_type: str, row: int):
        if row < 0:
            return
        self.rule_segment.setCurrentItem(match_type)
        self._on_rule_tab_changed(match_type)
        table = self._tables[match_type]
        table.selectRow(row)
        edit = table.cellWidget(row, self.COL_VALUE)
        if edit is not None:
            edit.setFocus()
            edit.setCursorPosition(len(edit.text()))

    # ---------- 交互 ----------
    def _on_add_rule(self):
        self._insert_row(self._current_match_type, "", "aggregation")
        self.rules_changed.emit()

    def _on_remove_selected(self):
        rows = sorted({idx.row() for idx in self.tableWidget.selectedIndexes()}, reverse=True)
        if not rows and self.tableWidget.rowCount() > 0:
            rows = [self.tableWidget.rowCount() - 1]
        for row in rows:
            self.tableWidget.removeRow(row)
        if rows:
            self._update_rule_state()
            self._update_rule_count()
            self.rules_changed.emit()

    def _on_select_process(self):
        if self._process_worker is not None and self._process_worker.isRunning():
            return
        self.select_process_btn.setEnabled(False)
        self.select_process_btn.setText(tr("routing_process_loading"))
        self._process_worker = ProcessListWorker(self)
        self._process_worker.result_ready.connect(self._on_processes_loaded)
        self._process_worker.failed.connect(self._on_processes_failed)
        self._process_worker.finished.connect(self._cleanup_process_worker)
        self._process_worker.start()

    @Slot(list)
    def _on_processes_loaded(self, processes: list):
        self._restore_process_button()
        dialog = ProcessSelectDialog(list(processes), self)
        if dialog.exec():
            process = dialog.selected_process()
            if process:
                existing_row = self._find_process_row(process)
                if existing_row >= 0:
                    self._focus_rule_row(MATCH_PROCESS, existing_row)
                    self.duplicate_detected.emit(tr(
                        "routing_duplicate_process", name=process
                    ))
                    return
                self._insert_row(MATCH_PROCESS, process, "aggregation")
                self._sort_rules(MATCH_PROCESS)
                self.rules_changed.emit()

    @Slot(str)
    def _on_processes_failed(self, _message: str):
        self._restore_process_button()
        dialog = ProcessSelectDialog([], self)
        dialog.exec()

    def _cleanup_process_worker(self):
        if self._shutting_down:
            return
        if self._process_worker is not None:
            self._process_worker.deleteLater()
            self._process_worker = None
        self._restore_process_button()

    def _restore_process_button(self):
        if self._shutting_down:
            return
        self.select_process_btn.setText(tr("routing_select_process"))
        self.select_process_btn.setEnabled(self._controls_enabled)

    # ---------- 状态机 ----------
    def set_controls_enabled(self, enabled: bool):
        """运行中锁死规则编辑入口，停止后恢复。"""
        self._controls_enabled = enabled
        self.add_btn.setEnabled(enabled)
        self.select_process_btn.setEnabled(enabled)
        self.remove_btn.setEnabled(enabled)
        self.export_btn.setEnabled(enabled)
        self.import_btn.setEnabled(enabled)
        for table in self._tables.values():
            table.setEnabled(enabled)
        for _kind, _table, _row, edit, outbound in self._rule_rows():
            edit.setEnabled(enabled)
            outbound.setEnabled(enabled)

    def prepare_for_shutdown(self):
        """停止编辑回调和进程扫描，避免 Qt 销毁阶段访问已释放的表格控件。"""
        if self._shutting_down:
            return
        self._shutting_down = True

        for _kind, _table, _row, edit, outbound in self._rule_rows():
            for widget in (edit, outbound):
                widget.blockSignals(True)
                widget.removeEventFilter(self)

        worker = self._process_worker
        if worker is not None:
            try:
                worker.result_ready.disconnect(self._on_processes_loaded)
            except Exception:
                pass
            try:
                worker.failed.disconnect(self._on_processes_failed)
            except Exception:
                pass
            try:
                worker.finished.disconnect(self._cleanup_process_worker)
            except Exception:
                pass
            if worker.isRunning():
                # tasklist 本身有 8 秒超时；等待它自然结束比强制销毁 QThread 安全。
                worker.wait(9000)

    # ---------- 数据 API ----------
    def get_rules(self) -> list:
        """读取表格并返回规范化的进程、域名和 IP/CIDR 规则。"""
        rules = []
        seen = set()
        for match_type, _table, _row, edit, combo in self._rule_rows():
            expanded = expand_routing_rule({
                "match_type": match_type,
                "value": edit.text().strip(),
                "outbound": combo.currentData() or "aggregation",
            })
            if not expanded:
                continue
            rule = expanded[0]
            identity = routing_rule_identity(match_type, routing_rule_value(rule))
            if identity in seen:
                continue
            seen.add(identity)
            rules.append(rule)
        return sorted(rules, key=routing_rule_sort_key)

    def load_rules(self, rules: list):
        """从持久化配置恢复规则到表格。"""
        for table in self._tables.values():
            table.setRowCount(0)
        for rule in normalize_routing_rules(rules or []):
            self._insert_row(
                rule["match_type"],
                routing_rule_value(rule),
                str(rule.get("outbound", "aggregation")),
            )
        self._update_rule_state()
        self._update_rule_count()

    def retranslate_ui(self):
        self._title.setText(tr("routing_title"))
        self._hint.setText(tr("routing_hint"))
        self.add_btn.setText(tr("routing_add"))
        self.select_process_btn.setText(tr("routing_select_process"))
        self.remove_btn.setText(tr("routing_remove"))
        self._backup_label.setText(tr("routing_backup_group"))
        self.export_btn.setText(tr("routing_export"))
        self.import_btn.setText(tr("routing_import"))
        self._list_title.setText(tr("routing_list_title"))
        self.rule_segment.setItemText(MATCH_PROCESS, tr("routing_tab_process"))
        self.rule_segment.setItemText(MATCH_DOMAIN, tr("routing_tab_domain"))
        self.rule_segment.setItemText(MATCH_IP, tr("routing_tab_ip"))
        self._update_rule_count()
        self._update_rule_state()
        for match_type, table in self._tables.items():
            self._apply_headers(table, match_type)
        for match_type, _table, _row, edit, combo in self._rule_rows():
            edit.setPlaceholderText(tr(self._placeholder_key(match_type)))
            current = combo.currentData() or "aggregation"
            self._fill_outbound_combo(combo, current)
            combo.setEnabled(self._controls_enabled)
