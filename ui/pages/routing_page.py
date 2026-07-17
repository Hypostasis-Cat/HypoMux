"""
HypoMux 路由规则页 (RoutingPage) - 进程级分流规则编辑器

用户在表格中维护「进程名 -> 出口通道」规则，MainWindow 读取后动态
序列化为 sing-box route.rules。页面只负责视图、进程选择和规则数据回吐，
不直接触碰代理内核线程，避免破坏既有单端口、多端口、聚合引擎信号链。
"""

from __future__ import annotations

import csv
import subprocess
from io import StringIO
from typing import Any, Iterable, List, Optional

from PySide6.QtCore import Qt, QEvent, Signal, QThread, Slot
from PySide6.QtWidgets import QWidget, QVBoxLayout, QHBoxLayout, QHeaderView
from qfluentwidgets import (
    TableWidget, TitleLabel, BodyLabel, PushButton, TransparentPushButton,
    LineEdit, ComboBox, FluentIcon, MessageBoxBase, SearchLineEdit, ListWidget,
    SubtitleLabel, CaptionLabel, ElevatedCardWidget, PrimaryPushButton,
)

from ui.i18n import tr


_CREATE_NO_WINDOW = getattr(subprocess, "CREATE_NO_WINDOW", 0x08000000)
ROUTING_BACKUP_FORMAT = "hypomux-routing-rules"
ROUTING_BACKUP_VERSION = 1


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
            if version != ROUTING_BACKUP_VERSION:
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
        names = item.get("process_name", [])
        if isinstance(names, str):
            names = [names]
        if not isinstance(names, list) or not names:
            raise ValueError(f"missing process name at index {index}")

        outbound = str(item.get("outbound", "aggregation") or "").strip()
        if (
            outbound not in ("aggregation", "direct")
            and (not outbound.startswith("nic_") or len(outbound) == 4)
        ):
            raise ValueError(f"invalid outbound at index {index}")

        has_valid_name = False
        for raw_name in names:
            name = str(raw_name or "").strip()
            if not name or len(name) > 260 or any(ch in name for ch in ("/", "\\", ":", "\0")):
                raise ValueError(f"invalid process name at index {index}")
            has_valid_name = True
            normalized = name.casefold()
            if normalized in seen:
                continue
            seen.add(normalized)
            rules.append({"process_name": [name], "outbound": outbound})
        if not has_valid_name:
            raise ValueError(f"missing process name at index {index}")
    return rules


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
    """进程级分流规则管理页。"""

    rules_changed = Signal()
    duplicate_detected = Signal(str)
    export_requested = Signal()
    import_requested = Signal()

    COL_PROCESS = 0
    COL_OUTBOUND = 1
    ROW_HEIGHT = 38

    def __init__(self, parent=None):
        super().__init__(parent)
        self.setObjectName("routingPage")
        self._available_aliases: List[str] = []
        self._controls_enabled = True
        self._shutting_down = False
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
        self._toolbar_card = ElevatedCardWidget(self)
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
        root.addLayout(table_bar)

        self.tableWidget = TableWidget(self)
        self.table = self.tableWidget
        self.tableWidget.setBorderVisible(True)
        self.tableWidget.setBorderRadius(8)
        self.tableWidget.setWordWrap(False)
        self.tableWidget.setColumnCount(2)
        self.tableWidget.setRowCount(0)
        self.tableWidget.setMinimumHeight(260)
        self.tableWidget.setMaximumHeight(460)
        self.tableWidget.verticalHeader().hide()
        self.tableWidget.verticalHeader().setDefaultSectionSize(self.ROW_HEIGHT)
        self.tableWidget.setSelectionBehavior(TableWidget.SelectRows)
        # Fluent TableWidget 默认启用整行悬停追踪。规则单元格内嵌输入框时，
        # 这种高亮很像“鼠标经过即选中”，会干扰用户判断当前编辑行。
        self.tableWidget.setMouseTracking(False)
        self.tableWidget.viewport().setMouseTracking(False)
        self._apply_headers()

        header = self.tableWidget.horizontalHeader()
        header.setSectionResizeMode(self.COL_PROCESS, QHeaderView.Stretch)
        header.setSectionResizeMode(self.COL_OUTBOUND, QHeaderView.Stretch)

        self._duplicate_hint = BodyLabel("", self)
        self._duplicate_hint.setWordWrap(True)
        self._duplicate_hint.setStyleSheet("color: #c42b1c;")
        self._duplicate_hint.hide()
        root.addWidget(self._duplicate_hint)
        root.addWidget(self.tableWidget)
        root.addStretch(1)
        self._update_rule_count()

    def _apply_headers(self):
        self.tableWidget.setHorizontalHeaderLabels([
            tr("routing_col_process"),
            tr("routing_col_nic"),
        ])

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

    def _make_outbound_combo(self, current: str = "aggregation") -> ComboBox:
        combo = ComboBox(self.tableWidget)
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
        for row in range(self.tableWidget.rowCount()):
            combo = self.tableWidget.cellWidget(row, self.COL_OUTBOUND)
            if combo is not None:
                current = combo.currentData() or "aggregation"
                self._fill_outbound_combo(combo, current)
                combo.setEnabled(self._controls_enabled)

    # ---------- 行构建 ----------
    def _insert_row(self, process_name: str = "", outbound: str = "aggregation"):
        row = self.tableWidget.rowCount()
        self.tableWidget.insertRow(row)
        self.tableWidget.setRowHeight(row, self.ROW_HEIGHT)

        edit = LineEdit(self.tableWidget)
        edit.setPlaceholderText(tr("routing_placeholder_process"))
        edit.setText(process_name)
        edit.textChanged.connect(self._on_process_text_changed)
        edit.installEventFilter(self)
        edit.setEnabled(self._controls_enabled)
        self.tableWidget.setCellWidget(row, self.COL_PROCESS, edit)

        combo = self._make_outbound_combo(outbound)
        self.tableWidget.setCellWidget(row, self.COL_OUTBOUND, combo)
        self._update_duplicate_state()
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
            for row in range(self.tableWidget.rowCount()):
                if watched in (
                    self.tableWidget.cellWidget(row, self.COL_PROCESS),
                    self.tableWidget.cellWidget(row, self.COL_OUTBOUND),
                ):
                    self.tableWidget.selectRow(row)
                    break
        return super().eventFilter(watched, event)

    @staticmethod
    def _normalize_process_name(name: str) -> str:
        """Windows 进程名不区分大小写；首尾空格也不应形成不同规则。"""
        return str(name or "").strip().casefold()

    def _process_edits(self):
        for row in range(self.tableWidget.rowCount()):
            edit = self.tableWidget.cellWidget(row, self.COL_PROCESS)
            if edit is not None:
                yield row, edit

    def _duplicate_process_names(self) -> set:
        counts = {}
        for _row, edit in self._process_edits():
            normalized = self._normalize_process_name(edit.text())
            if normalized:
                counts[normalized] = counts.get(normalized, 0) + 1
        return {name for name, count in counts.items() if count > 1}

    def _update_duplicate_state(self) -> set:
        duplicates = self._duplicate_process_names()
        display_names = []
        seen = set()
        for _row, edit in self._process_edits():
            normalized = self._normalize_process_name(edit.text())
            edit.setError(bool(normalized and normalized in duplicates))
            if normalized in duplicates and normalized not in seen:
                seen.add(normalized)
                display_names.append(edit.text().strip())

        if display_names:
            self._duplicate_hint.setText(tr(
                "routing_duplicate_hint", names=", ".join(display_names)
            ))
            self._duplicate_hint.show()
        else:
            self._duplicate_hint.clear()
            self._duplicate_hint.hide()
        return duplicates

    def _on_process_text_changed(self, _text: str):
        if self._shutting_down:
            return
        self._update_duplicate_state()
        self.rules_changed.emit()

    def _find_process_row(self, process_name: str) -> int:
        target = self._normalize_process_name(process_name)
        if not target:
            return -1
        for row, edit in self._process_edits():
            if self._normalize_process_name(edit.text()) == target:
                return row
        return -1

    def _focus_process_row(self, row: int):
        if row < 0:
            return
        self.tableWidget.selectRow(row)
        edit = self.tableWidget.cellWidget(row, self.COL_PROCESS)
        if edit is not None:
            edit.setFocus()
            edit.setCursorPosition(len(edit.text()))

    # ---------- 交互 ----------
    def _on_add_rule(self):
        self._insert_row("", "aggregation")
        self.rules_changed.emit()

    def _on_remove_selected(self):
        rows = sorted({idx.row() for idx in self.tableWidget.selectedIndexes()}, reverse=True)
        if not rows and self.tableWidget.rowCount() > 0:
            rows = [self.tableWidget.rowCount() - 1]
        for row in rows:
            self.tableWidget.removeRow(row)
        if rows:
            self._update_duplicate_state()
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
                    self._focus_process_row(existing_row)
                    self.duplicate_detected.emit(tr(
                        "routing_duplicate_process", name=process
                    ))
                    return
                self._insert_row(process, "aggregation")
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
        self.tableWidget.setEnabled(enabled)
        for row in range(self.tableWidget.rowCount()):
            for col in (self.COL_PROCESS, self.COL_OUTBOUND):
                widget = self.tableWidget.cellWidget(row, col)
                if widget is not None:
                    widget.setEnabled(enabled)

    def prepare_for_shutdown(self):
        """停止编辑回调和进程扫描，避免 Qt 销毁阶段访问已释放的表格控件。"""
        if self._shutting_down:
            return
        self._shutting_down = True

        for row in range(self.tableWidget.rowCount()):
            for col in (self.COL_PROCESS, self.COL_OUTBOUND):
                widget = self.tableWidget.cellWidget(row, col)
                if widget is None:
                    continue
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
        """读取表格，返回 [{"process_name": [name], "outbound": tag}, ...]。"""
        rules = []
        seen = set()
        for row in range(self.tableWidget.rowCount()):
            edit = self.tableWidget.cellWidget(row, self.COL_PROCESS)
            combo = self.tableWidget.cellWidget(row, self.COL_OUTBOUND)
            if edit is None or combo is None:
                continue
            name = edit.text().strip()
            if not name:
                continue
            normalized = self._normalize_process_name(name)
            if normalized in seen:
                # 防御性去重：存在未修正的重复项时，以第一条规则为准，
                # 避免向 sing-box 生成含义冲突的重复规则。
                continue
            seen.add(normalized)
            tag = combo.currentData() or "aggregation"
            rules.append({"process_name": [name], "outbound": tag})
        return rules

    def load_rules(self, rules: list):
        """从持久化配置恢复规则到表格。"""
        self.tableWidget.setRowCount(0)
        for rule in (rules or []):
            if not isinstance(rule, dict):
                continue
            procs = rule.get("process_name", [])
            name = procs[0] if isinstance(procs, list) and procs else (
                procs if isinstance(procs, str) else ""
            )
            outbound = rule.get("outbound", "aggregation")
            if name:
                self._insert_row(str(name), str(outbound))
        self._update_duplicate_state()
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
        self._update_rule_count()
        self._update_duplicate_state()
        self._apply_headers()
        for row in range(self.tableWidget.rowCount()):
            edit = self.tableWidget.cellWidget(row, self.COL_PROCESS)
            combo = self.tableWidget.cellWidget(row, self.COL_OUTBOUND)
            if edit is not None:
                edit.setPlaceholderText(tr("routing_placeholder_process"))
            if combo is not None:
                current = combo.currentData() or "aggregation"
                self._fill_outbound_combo(combo, current)
                combo.setEnabled(self._controls_enabled)
