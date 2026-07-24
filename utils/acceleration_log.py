"""单文件、最近三次会话保留的加速诊断日志。"""

from __future__ import annotations

from datetime import datetime
import json
from pathlib import Path
from threading import RLock
from typing import Any, Iterable, Mapping, Optional
from uuid import uuid4


SESSION_MARKER = "=== HypoMux Acceleration Session |"
MAX_SESSIONS = 3


class AccelerationLogStore:
    """只记录加速故障排查所需事件，并在单一文件中保留最近会话。"""

    def __init__(self, path: Optional[Path] = None, max_sessions: int = MAX_SESSIONS):
        self.path = path or (Path.home() / ".hypomux" / "logs" / "app.log")
        self.max_sessions = max(1, int(max_sessions))
        self._active = False
        self._lock = RLock()
        # 迁移旧版 RotatingFileHandler 遗留的明确轮转文件，后续只使用 app.log。
        self._remove_legacy_rotations()

    @property
    def active(self) -> bool:
        return self._active

    def start(
        self,
        mode: str,
        adapters: Iterable[str] = (),
        context: Optional[Mapping[str, Any]] = None,
    ):
        """开启新的加速会话，并裁剪历史到最近 ``max_sessions`` 次。

        ``context`` 只应包含适合用户提交给开发者排障的非敏感快照，例如
        程序版本、运行模式、端口、选中网卡的接口索引/网关/DNS 与规则统计。
        它会以单行 JSON 写入，既便于人工阅读，也方便之后按字段检索。
        """
        with self._lock:
            if self._active:
                return
            history = self._read_sessions()
            # 新会话加入后最多保留 max_sessions 段，因此历史只保留 max-1 段。
            history = history[-(self.max_sessions - 1):] if self.max_sessions > 1 else []
            self._rewrite_sessions(history)
            names = ", ".join(str(name).strip() for name in adapters if str(name).strip())
            session_id = uuid4().hex[:12]
            self._append(
                f"{SESSION_MARKER} id={session_id} | started={self._timestamp()} | mode={mode} ===\n"
                f"selected_adapters={names or 'none'}"
            )
            if context:
                self._append(
                    "session_context="
                    + json.dumps(context, ensure_ascii=False, sort_keys=True, default=str)
                )
            self._active = True
            self._remove_legacy_rotations()

    def record(self, message: object, *, force: bool = False):
        """仅在会话中写入关键生命周期、配置及故障信息。"""
        text = str(message or "").strip()
        if not text or not self._active:
            return
        if not force and not self._is_key_event(text):
            return
        with self._lock:
            self._append(f"{self._timestamp()} | {text}")

    def finish(self, reason: str = "stopped"):
        """结束当前会话；重复调用安全。"""
        with self._lock:
            if not self._active:
                return
            self._append(
                f"=== HypoMux Acceleration Session End | ended={self._timestamp()} | reason={reason} ==="
            )
            self._active = False

    def record_event(self, category: str, event: str, **fields: Any):
        """写入一个稳定、易检索的生命周期或诊断事件。"""
        payload = {
            "category": str(category).strip() or "general",
            "event": str(event).strip() or "unknown",
            **{key: value for key, value in fields.items() if value is not None},
        }
        self.record(
            "event=" + json.dumps(payload, ensure_ascii=False, sort_keys=True, default=str),
            force=True,
        )

    def _read_sessions(self) -> list[str]:
        try:
            content = self.path.read_text(encoding="utf-8")
        except OSError:
            return []
        starts = []
        offset = 0
        while True:
            found = content.find(SESSION_MARKER, offset)
            if found < 0:
                break
            starts.append(found)
            offset = found + len(SESSION_MARKER)
        if not starts:
            # 旧版 app.log 是无结构的全量日志，不与新的诊断会话混存。
            return []
        return [
            content[start:end].strip()
            for start, end in zip(starts, starts[1:] + [len(content)])
            if content[start:end].strip()
        ]

    def _rewrite_sessions(self, sessions: list[str]):
        try:
            self.path.parent.mkdir(parents=True, exist_ok=True)
            content = "\n\n".join(sessions).strip()
            temp_path = self.path.with_suffix(self.path.suffix + ".tmp")
            temp_path.write_text((content + "\n") if content else "", encoding="utf-8")
            temp_path.replace(self.path)
        except OSError:
            # 日志不可写不能影响加速本身；后续 append 也会静默降级。
            pass

    def _append(self, line: str):
        try:
            self.path.parent.mkdir(parents=True, exist_ok=True)
            with self.path.open("a", encoding="utf-8") as stream:
                stream.write(line.rstrip() + "\n")
        except OSError:
            pass

    def _remove_legacy_rotations(self):
        for suffix in (".1", ".2", ".3"):
            try:
                self.path.with_name(self.path.name + suffix).unlink(missing_ok=True)
            except OSError:
                pass

    @staticmethod
    def _timestamp() -> str:
        return datetime.now().astimezone().isoformat(timespec="seconds")

    @staticmethod
    def _is_key_event(message: str) -> bool:
        """滤掉逐连接/逐流量噪声，留下复现与定位问题所需的事件。"""
        text = message.casefold()
        failure_words = (
            "失败", "错误", "异常", "超时", "无法", "回滚", "崩溃",
            "error", "fail", "exception", "timeout", "fatal", "panic",
        )
        if any(word in text for word in failure_words):
            return True
        # sing-box 正常运行日志可能非常频繁；非告警内容不写入诊断文件。
        if text.startswith("[sing-box:"):
            return False
        # TCP/UDP 每条连接都会产生日志，即使正常也没有诊断价值。
        if "[tcp]" in text or "[udp]" in text or "[调度分配]" in text:
            return False
        key_words = (
            "[tun]", "[出站池]", "[hypomux]", "启动", "停止", "验证",
            "预检", "配置", "启用", "关闭", "dns", "bind", "listen",
            "route", "wintun", "sing-box",
        )
        return any(word in text for word in key_words)
