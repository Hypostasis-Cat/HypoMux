"""Thin Qt signal adapter for the toolkit-independent engine client."""

from __future__ import annotations

import threading
from typing import Any

from PySide6.QtCore import QObject, Signal

from engine_client import EngineClient, EngineClientError
from engine_client.process import EngineCommand


class EngineBridge(QObject):
    """Translate engine callbacks into queued Qt signals.

    Network lifecycle requests are exposed only to non-UI worker threads.
    Production UI code continues to receive queued Qt signals.
    """

    connected = Signal(object)
    disconnected = Signal(str)
    event_received = Signal(object)
    state_changed = Signal(object)
    engine_error = Signal(str)
    log_record = Signal(str)
    telemetry_snapshot = Signal(object)
    dns_fallback_required = Signal(object)

    def __init__(self, command: EngineCommand, parent: QObject | None = None):
        super().__init__(parent)
        self._start_lock = threading.Lock()
        self._start_thread: threading.Thread | None = None
        self._stop_requested = threading.Event()
        self._client = EngineClient(
            command,
            on_event=self._on_event,
            on_stderr=self._on_stderr,
            on_disconnect=self._on_disconnect,
        )

    def start(self):
        with self._start_lock:
            if self._start_thread is not None and self._start_thread.is_alive():
                return
            if self._client.is_running():
                return
            self._stop_requested.clear()
            self._start_thread = threading.Thread(
                target=self._start_client,
                name="HypoMuxEngineBridgeStart",
                daemon=True,
            )
            self._start_thread.start()

    def stop(self):
        self._stop_requested.set()
        self._client.stop()
        with self._start_lock:
            thread = self._start_thread
        if thread is not None and thread is not threading.current_thread():
            thread.join(timeout=2.5)

    def is_running(self) -> bool:
        return self._client.is_running()

    def supports(self, method: str) -> bool:
        hello = self._client.hello or {}
        capabilities = hello.get("capabilities")
        return isinstance(capabilities, list) and method in capabilities

    def request(self, method: str, params: Any = None, *, timeout: float | None = None):
        """Send an engine request from a non-UI worker thread."""
        return self._client.request(method, params, timeout=timeout)

    def _start_client(self):
        try:
            hello = self._client.start()
            if self._stop_requested.is_set():
                self._client.stop()
                return
            self.connected.emit(hello)
        except EngineClientError as exc:
            if not self._stop_requested.is_set():
                self.engine_error.emit(str(exc))
        except Exception as exc:
            if not self._stop_requested.is_set():
                self.engine_error.emit(
                    f"unexpected engine bridge failure: {type(exc).__name__}: {exc}"
                )

    def _on_event(self, message: dict[str, Any]):
        self.event_received.emit(message)
        name = message.get("event")
        data = message.get("data")
        if not isinstance(data, dict):
            data = {}

        if name == "engine.state_changed":
            self.state_changed.emit(data)
        elif name == "engine.error":
            self.engine_error.emit(str(data.get("message", "engine reported an error")))
        elif name == "log.record":
            self.log_record.emit(str(data.get("message", "")))
        elif name == "telemetry.snapshot":
            self.telemetry_snapshot.emit(data)
        elif name == "dns.fallback_required":
            self.dns_fallback_required.emit(data)

    def _on_stderr(self, message: str):
        self.log_record.emit(f"[GoEngine][stderr] {message}")

    def _on_disconnect(self, error: EngineClientError | None):
        self.disconnected.emit("" if error is None else str(error))
