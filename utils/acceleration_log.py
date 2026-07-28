"""单文件、最近三次会话保留的加速诊断日志。"""

from __future__ import annotations

from datetime import datetime
import json
from pathlib import Path
import re
from threading import RLock
import time
from typing import Any, Iterable, Mapping, Optional
from uuid import uuid4


SESSION_MARKER = "=== HypoMux Acceleration Session |"
MAX_SESSIONS = 3
MAX_LOG_BYTES = 5 * 1024 * 1024
RATE_LIMIT_WINDOW_SECONDS = 10.0
TRUNCATION_MARKER = "[日志维护] 较早内容已按日志大小上限裁剪。"


class AccelerationLogStore:
    """只记录加速故障排查所需事件，并在单一文件中保留最近会话。"""

    def __init__(
        self,
        path: Optional[Path] = None,
        max_sessions: int = MAX_SESSIONS,
        max_log_bytes: int = MAX_LOG_BYTES,
        rate_limit_window_seconds: float = RATE_LIMIT_WINDOW_SECONDS,
    ):
        self.path = path or (Path.home() / ".hypomux" / "logs" / "app.log")
        self.max_sessions = max(1, int(max_sessions))
        self.max_log_bytes = max(4096, int(max_log_bytes))
        self.rate_limit_window_seconds = max(0.0, float(rate_limit_window_seconds))
        self._active = False
        self._lock = RLock()
        self._rate_limit_states: dict[str, dict[str, Any]] = {}
        # 迁移旧版 RotatingFileHandler 遗留的明确轮转文件，后续只使用 app.log。
        self._remove_legacy_rotations()
        self._enforce_size_limit()

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
            self._rate_limit_states.clear()
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
            if not force and not self._allow_rate_limited_message(text):
                return
            self._append(f"{self._timestamp()} | {text}")

    def finish(self, reason: str = "stopped"):
        """结束当前会话；重复调用安全。"""
        with self._lock:
            if not self._active:
                return
            self._flush_rate_limit_summaries()
            self._append(
                f"=== HypoMux Acceleration Session End | ended={self._timestamp()} | reason={reason} ==="
            )
            self._active = False
            self._enforce_size_limit()

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
            with temp_path.open("w", encoding="utf-8", newline="\n") as stream:
                stream.write((content + "\n") if content else "")
            temp_path.replace(self.path)
        except OSError:
            # 日志不可写不能影响加速本身；后续 append 也会静默降级。
            pass

    def _append(self, line: str):
        try:
            self.path.parent.mkdir(parents=True, exist_ok=True)
            with self.path.open("a", encoding="utf-8", newline="\n") as stream:
                stream.write(line.rstrip() + "\n")
            self._enforce_size_limit()
        except OSError:
            pass

    def _allow_rate_limited_message(self, text: str) -> bool:
        """Keep one sample per noisy category and aggregate repeats."""
        limited = self._rate_limit_key(text)
        if limited is None or self.rate_limit_window_seconds <= 0:
            return True
        key, label = limited
        now = time.monotonic()
        state = self._rate_limit_states.get(key)
        if state is None:
            self._rate_limit_states[key] = {
                "started": now,
                "suppressed": 0,
                "label": label,
            }
            return True
        if now - float(state["started"]) < self.rate_limit_window_seconds:
            state["suppressed"] = int(state["suppressed"]) + 1
            return False
        self._append_rate_limit_summary(state)
        self._rate_limit_states[key] = {
            "started": now,
            "suppressed": 0,
            "label": label,
        }
        return True

    def _flush_rate_limit_summaries(self):
        for state in self._rate_limit_states.values():
            self._append_rate_limit_summary(state)
        self._rate_limit_states.clear()

    def _append_rate_limit_summary(self, state: Mapping[str, Any]):
        suppressed = int(state.get("suppressed", 0))
        if suppressed <= 0:
            return
        label = str(state.get("label") or "同类日志")
        window = max(1, int(round(self.rate_limit_window_seconds)))
        self._append(
            f"{self._timestamp()} | [日志聚合] {label}在 {window} 秒窗口内"
            f"重复 {suppressed} 次，已省略。"
        )

    @staticmethod
    def _rate_limit_key(text: str) -> Optional[tuple[str, str]]:
        lowered = text.casefold()
        if "[socket-bind] ready" in lowered:
            purpose = re.search(r"\bpurpose=([^\s]+)", text)
            adapter = re.search(r"\badapter=([^\s]+)", text)
            purpose_name = purpose.group(1) if purpose else "unknown"
            adapter_name = adapter.group(1) if adapter else "unknown"
            family = "IPv6" if "ipv6_unicast_if" in lowered else "IPv4"
            return (
                f"socket-bind:{purpose_name}:{adapter_name}:{family}",
                f"socket-bind ready（{purpose_name}/{adapter_name}/{family}）",
            )
        if "[连通失败]" in text and "[出站池" in text:
            pool = re.search(r"\[出站池(?:-([^\]]+))?\]\[连通失败\]", text)
            error = re.search(r"\|\s*([A-Za-z_][\w.]*)", text)
            pool_name = pool.group(1) if pool and pool.group(1) else "default"
            error_name = error.group(1) if error else "unknown"
            return (
                f"pool-connect-failure:{pool_name}:{error_name}",
                f"出站池连通失败（{pool_name}/{error_name}）",
            )
        if lowered.startswith("[sing-box:stderr]") and "connection: open connection" in lowered:
            return (
                "sing-box:connection-open-error",
                "sing-box 出站连接错误",
            )
        return None

    def _enforce_size_limit(self):
        """Keep recent structured sessions while enforcing a hard byte limit."""
        try:
            if not self.path.exists() or self.path.stat().st_size <= self.max_log_bytes:
                return
            sessions = self._read_sessions()
            if not sessions:
                self._trim_unstructured_file()
                return
            kept: list[str] = []
            remaining = self.max_log_bytes
            for session in reversed(sessions[-self.max_sessions:]):
                separator_size = 2 if kept else 1
                encoded_size = len(session.encode("utf-8")) + separator_size
                if encoded_size <= remaining:
                    kept.insert(0, session)
                    remaining -= encoded_size
                    continue
                if not kept:
                    kept.insert(0, self._trim_session(session, remaining))
                break
            self._rewrite_sessions(kept)
        except OSError:
            pass

    def _trim_session(self, session: str, byte_budget: int) -> str:
        """Preserve session identity/context and the newest diagnostic tail."""
        lines = session.splitlines()
        if not lines:
            return ""
        head = [lines[0]]
        for line in lines[1:3]:
            if line.startswith(("selected_adapters=", "session_context=")):
                head.append(line)
        marker = f"{self._timestamp()} | {TRUNCATION_MARKER}"
        prefix = "\n".join(head + [marker]) + "\n"
        prefix_bytes = prefix.encode("utf-8")
        tail_budget = max(0, byte_budget - len(prefix_bytes) - 1)
        tail_bytes = session.encode("utf-8")[-tail_budget:] if tail_budget else b""
        tail = tail_bytes.decode("utf-8", errors="ignore")
        if "\n" in tail and tail_bytes:
            tail = tail.split("\n", 1)[1]
        result = prefix + tail.lstrip("\n")
        while len(result.encode("utf-8")) + 1 > byte_budget and "\n" in tail:
            tail = tail.split("\n", 1)[1]
            result = prefix + tail
        return result.rstrip()

    def _trim_unstructured_file(self):
        """Migrate an oversized legacy log by retaining only its newest tail."""
        data = self.path.read_bytes()
        budget = max(0, self.max_log_bytes - len(TRUNCATION_MARKER.encode("utf-8")) - 2)
        tail = data[-budget:].decode("utf-8", errors="ignore") if budget else ""
        if "\n" in tail:
            tail = tail.split("\n", 1)[1]
        with self.path.open("w", encoding="utf-8", newline="\n") as stream:
            stream.write(TRUNCATION_MARKER + "\n")
            stream.write(tail)

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
