"""Settings group responsible for appearance and wallpaper preferences."""

from __future__ import annotations

import shutil
from pathlib import Path

from PySide6.QtCore import Qt, QSettings, Signal
from PySide6.QtGui import QColor, QImageReader
from PySide6.QtWidgets import QFileDialog
from qfluentwidgets import (
    CaptionLabel,
    ComboBox,
    PushButton,
    SettingCardGroup,
    Slider,
    Theme,
    setTheme,
    setThemeColor,
)

from ui.components import (
    LocalizedColorPickerButton,
    LocalizedSwitchSettingCard,
    TranslucentSettingCard,
    register_content_card_control,
)
from ui.i18n import tr
from ui.pages import resolve_icon
from utils.config_manager import get_config_path


DEFAULT_THEME_COLOR = "#0078d4"
_THEME_MAP = {0: Theme.AUTO, 1: Theme.LIGHT, 2: Theme.DARK}
_THEME_INDEX = {"auto": 0, "light": 1, "dark": 2}
_IMAGE_FILTER = "Images (*.png *.jpg *.jpeg *.bmp *.webp)"


class PersonalizationSettingGroup(SettingCardGroup):
    """Keeps all appearance controls and their persistence in one place."""

    theme_color_changed = Signal()
    mica_effect_changed = Signal(bool)
    background_image_changed = Signal(str)
    content_card_opacity_changed = Signal(int)
    warning_message = Signal(str)

    def __init__(self, parent=None):
        super().__init__(tr("settings_personalization"), parent)
        self._settings = QSettings("Hypostasis-Cat", "HypoMux")
        self._theme_color_mode = "default"
        self._build_cards()

    def _build_cards(self):
        self.theme_card = TranslucentSettingCard(
            resolve_icon("PALETTE", "CONSTRACT", "BRUSH"),
            tr("settings_theme"), tr("settings_theme_hint"), self,
        )
        self.theme_combo = ComboBox(self.theme_card)
        self.theme_combo.addItem(tr("settings_theme_auto"), userData="auto")
        self.theme_combo.addItem(tr("settings_theme_light"), userData="light")
        self.theme_combo.addItem(tr("settings_theme_dark"), userData="dark")
        self.theme_combo.setCurrentIndex(
            _THEME_INDEX.get(self._settings.value("theme", "auto"), 0)
        )
        register_content_card_control(self.theme_combo)
        self.theme_combo.currentIndexChanged.connect(self._on_theme_changed)
        self.theme_card.hBoxLayout.addWidget(self.theme_combo, 0, Qt.AlignRight)
        self.theme_card.hBoxLayout.addSpacing(16)
        self.addSettingCard(self.theme_card)

        saved_color = QColor(
            self._settings.value("theme_color", DEFAULT_THEME_COLOR, type=str)
        )
        if not saved_color.isValid():
            saved_color = QColor(DEFAULT_THEME_COLOR)
        saved_color_mode = self._settings.value("theme_color_mode", "", type=str)
        if saved_color_mode not in {"default", "custom"}:
            saved_color_mode = (
                "custom" if saved_color.name().lower() != DEFAULT_THEME_COLOR else "default"
            )
        self._theme_color_mode = saved_color_mode

        self.theme_color_card = TranslucentSettingCard(
            resolve_icon("PALETTE", "BRUSH", "CONSTRACT"),
            tr("settings_theme_color"), tr("settings_theme_color_hint"), self,
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
        register_content_card_control(self.theme_color_mode_combo)
        self.theme_color_picker = LocalizedColorPickerButton(
            saved_color, tr("settings_theme_color"), self.theme_color_card
        )
        self.theme_color_picker.hide()
        self.theme_color_picker.colorChanged.connect(self._on_theme_color_changed)
        self.theme_color_choose_button = PushButton(
            tr("settings_theme_color_choose"), self.theme_color_card
        )
        register_content_card_control(self.theme_color_choose_button)
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
        self.addSettingCard(self.theme_color_card)

        self.mica_card = LocalizedSwitchSettingCard(
            resolve_icon("BRUSH", "PALETTE", "CONSTRACT"),
            tr("settings_mica_effect"), tr("settings_mica_effect_hint"), parent=self,
        )
        self.mica_card.setChecked(self._settings.value("mica_enabled", True, type=bool))
        self.mica_card.checkedChanged.connect(self._on_mica_effect_changed)
        self.addSettingCard(self.mica_card)

        self.background_card = TranslucentSettingCard(
            resolve_icon("PHOTO", "IMAGE", "ALBUM"),
            tr("settings_background_image"), tr("settings_background_image_hint"), self,
        )
        self.background_choose_button = PushButton(
            tr("settings_background_image_choose"), self.background_card
        )
        self.background_clear_button = PushButton(
            tr("settings_background_image_clear"), self.background_card
        )
        register_content_card_control(self.background_choose_button)
        register_content_card_control(self.background_clear_button)
        self.background_choose_button.clicked.connect(self._choose_background_image)
        self.background_clear_button.clicked.connect(self._clear_background_image)
        self.background_card.hBoxLayout.addWidget(
            self.background_clear_button, 0, Qt.AlignRight
        )
        self.background_card.hBoxLayout.addSpacing(8)
        self.background_card.hBoxLayout.addWidget(
            self.background_choose_button, 0, Qt.AlignRight
        )
        self.background_card.hBoxLayout.addSpacing(16)
        self.addSettingCard(self.background_card)

        saved_opacity = self._settings.value(
            "content_card_opacity", 88, type=int
        )
        self._content_card_opacity = max(0, min(saved_opacity, 100))
        self.content_card_opacity_card = TranslucentSettingCard(
            resolve_icon("TRANSPARENT", "BRUSH", "PALETTE"),
            tr("settings_content_card_opacity"),
            tr("settings_content_card_opacity_hint"), self,
        )
        self.content_card_opacity_value = CaptionLabel(
            self._format_content_card_opacity(), self.content_card_opacity_card
        )
        self.content_card_opacity_slider = Slider(self.content_card_opacity_card)
        self.content_card_opacity_slider.setOrientation(Qt.Horizontal)
        self.content_card_opacity_slider.setRange(0, 100)
        self.content_card_opacity_slider.setValue(self._content_card_opacity)
        self.content_card_opacity_slider.setFixedWidth(150)
        self.content_card_opacity_slider.valueChanged.connect(
            self._on_content_card_opacity_changed
        )
        self.content_card_opacity_card.hBoxLayout.addWidget(
            self.content_card_opacity_value, 0, Qt.AlignRight
        )
        self.content_card_opacity_card.hBoxLayout.addSpacing(12)
        self.content_card_opacity_card.hBoxLayout.addWidget(
            self.content_card_opacity_slider, 0, Qt.AlignRight
        )
        self.content_card_opacity_card.hBoxLayout.addSpacing(16)
        self.addSettingCard(self.content_card_opacity_card)
        self._refresh_background_controls()

    def _format_content_card_opacity(self) -> str:
        return f"{self._content_card_opacity}%"

    def _on_content_card_opacity_changed(self, opacity: int):
        self._content_card_opacity = max(0, min(int(opacity), 100))
        self.content_card_opacity_value.setText(self._format_content_card_opacity())
        self._settings.setValue("content_card_opacity", self._content_card_opacity)
        self._settings.sync()
        self.content_card_opacity_changed.emit(self._content_card_opacity)

    def _on_theme_changed(self, index: int):
        theme_code = self.theme_combo.itemData(index)
        self._settings.setValue("theme", theme_code)
        self._settings.sync()
        setTheme(_THEME_MAP.get(index, Theme.AUTO))

    def _on_theme_color_changed(self, color):
        color = QColor(color)
        if not color.isValid():
            return
        self._settings.setValue("theme_color", color.name())
        self._settings.sync()
        if self._theme_color_mode == "custom":
            setThemeColor(color)
            self.theme_color_changed.emit()

    def _on_theme_color_mode_changed(self, index: int):
        color_mode = self.theme_color_mode_combo.itemData(index)
        if color_mode not in {"default", "custom"}:
            return
        self._theme_color_mode = color_mode
        self.theme_color_choose_button.setEnabled(color_mode == "custom")
        self._settings.setValue("theme_color_mode", color_mode)
        self._settings.sync()
        setThemeColor(
            self.theme_color_picker.color if color_mode == "custom" else DEFAULT_THEME_COLOR
        )
        self.theme_color_changed.emit()

    def _on_mica_effect_changed(self, enabled: bool):
        self._settings.setValue("mica_enabled", bool(enabled))
        self._settings.sync()
        self.mica_effect_changed.emit(bool(enabled))

    def _choose_background_image(self):
        source, _ = QFileDialog.getOpenFileName(
            self, tr("settings_background_image_dialog"), "", _IMAGE_FILTER
        )
        if not source:
            return
        reader = QImageReader(source)
        if not reader.canRead():
            self.warning_message.emit(tr("settings_background_image_invalid"))
            return

        try:
            source_path = Path(source)
            suffix = source_path.suffix.lower()
            target_dir = get_config_path().parent / "background"
            target_dir.mkdir(parents=True, exist_ok=True)
            target = target_dir / f"wallpaper{suffix}"
            shutil.copy2(source_path, target)
            self._remove_cached_backgrounds(keep=target)
        except OSError:
            self.warning_message.emit(tr("settings_background_image_save_failed"))
            return

        self._settings.setValue("background_image", str(target))
        self._settings.sync()
        self._refresh_background_controls()
        self.background_image_changed.emit(str(target))

    def _clear_background_image(self):
        self._settings.remove("background_image")
        self._settings.sync()
        self._remove_cached_backgrounds()
        self._refresh_background_controls()
        self.background_image_changed.emit("")

    def _refresh_background_controls(self):
        path = self._settings.value("background_image", "", type=str)
        has_background = bool(path and Path(path).is_file())
        if path and not has_background:
            # 旧缓存被手动删除或清理工具移除时，不保留失效设置。
            self._settings.remove("background_image")
            self._settings.sync()
        self.background_clear_button.setEnabled(has_background)
        self.content_card_opacity_card.setVisible(has_background)
        self.mica_card.setEnabled(not has_background)
        self.mica_card.contentLabel.setText(
            tr("settings_mica_effect_background_hint")
            if has_background
            else tr("settings_mica_effect_hint")
        )
        self.adjustSize()

    @staticmethod
    def _remove_cached_backgrounds(keep: Path | None = None):
        """Delete only wallpapers created by this setting, never arbitrary user files."""
        try:
            cache_dir = get_config_path().parent / "background"
            if not cache_dir.is_dir():
                return
            keep_path = keep.resolve() if keep is not None else None
            for candidate in cache_dir.glob("wallpaper.*"):
                if not candidate.is_file():
                    continue
                if keep_path is not None and candidate.resolve() == keep_path:
                    continue
                candidate.unlink()
        except OSError:
            # Background cleanup should never block a successful visual change.
            pass

    def retranslate_ui(self):
        self.titleLabel.setText(tr("settings_personalization"))
        self.theme_card.titleLabel.setText(tr("settings_theme"))
        self.theme_card.contentLabel.setText(tr("settings_theme_hint"))
        self.theme_combo.setItemText(0, tr("settings_theme_auto"))
        self.theme_combo.setItemText(1, tr("settings_theme_light"))
        self.theme_combo.setItemText(2, tr("settings_theme_dark"))
        self.theme_color_card.titleLabel.setText(tr("settings_theme_color"))
        self.theme_color_card.contentLabel.setText(tr("settings_theme_color_hint"))
        self.theme_color_mode_combo.setItemText(0, tr("settings_theme_color_default"))
        self.theme_color_mode_combo.setItemText(1, tr("settings_theme_color_custom"))
        self.theme_color_picker.title = tr("settings_theme_color")
        self.theme_color_choose_button.setText(tr("settings_theme_color_choose"))
        self.mica_card.titleLabel.setText(tr("settings_mica_effect"))
        self.mica_card.contentLabel.setText(tr("settings_mica_effect_hint"))
        self.mica_card.refresh_switch_text()
        self.background_card.titleLabel.setText(tr("settings_background_image"))
        self.background_card.contentLabel.setText(tr("settings_background_image_hint"))
        self.background_choose_button.setText(tr("settings_background_image_choose"))
        self.background_clear_button.setText(tr("settings_background_image_clear"))
        self.content_card_opacity_card.titleLabel.setText(
            tr("settings_content_card_opacity")
        )
        self.content_card_opacity_card.contentLabel.setText(
            tr("settings_content_card_opacity_hint")
        )
        self._refresh_background_controls()
