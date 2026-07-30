"""Qt lifecycle adapter for the Go-owned sing-box TUN sidecar."""

from __future__ import annotations

import threading
from pathlib import Path

from PySide6.QtCore import QObject, Signal

from engine_client import EngineClientError
from engine_client.capabilities import TUN_LIFECYCLE_METHODS
from ui.engine_bridge import EngineBridge
from utils.runtime_paths import get_singbox_path


def can_use_go_tun_lifecycle(bridge: EngineBridge | None) -> bool:
    if bridge is None or not bridge.is_running():
        return False
    feature_checker = getattr(bridge, "supports_mode_feature", None)
    if not callable(feature_checker):
        return False
    return (
        all(bridge.supports(method) for method in TUN_LIFECYCLE_METHODS)
        and feature_checker("tun_tcp_pool", "managed_tun_lifecycle")
    )


class GoTunManager(QObject):
    """Expose Qt lifecycle signals while Go owns the managed sidecar."""

    log_signal = Signal(str)
    started_ok = Signal(str)
    stopped = Signal(str)
    error_signal = Signal(str)
    finished = Signal()

    def __init__(
        self,
        bridge: EngineBridge,
        config_path: str,
        executable: str | None = None,
        parent: QObject | None = None,
    ):
        super().__init__(parent)
        self._bridge = bridge
        self._config_path = str(Path(config_path).resolve())
        resolved_executable = executable or get_singbox_path() or ""
        self._executable = (
            str(Path(resolved_executable).resolve())
            if resolved_executable
            else ""
        )
        self._stop_requested = threading.Event()
        self._thread: threading.Thread | None = None
        self._thread_lock = threading.Lock()
        self._running = False

    def start(self):
        with self._thread_lock:
            if self._thread is not None and self._thread.is_alive():
                return
            self._stop_requested.clear()
            self._running = True
            self._thread = threading.Thread(
                target=self._run,
                name="HypoMuxGoTunManager",
                daemon=True,
            )
            self._thread.start()

    def stop(self):
        self._stop_requested.set()

    def force_kill(self):
        """Bounded shutdown fallback used only during application exit."""
        self._stop_requested.set()
        if not self._bridge.is_running():
            return
        try:
            self._bridge.request("tun.deactivate", timeout=4.0)
        except Exception:
            # Closing the engine host closes its kill-on-close Job object.
            pass

    def isRunning(self) -> bool:
        with self._thread_lock:
            return self._running

    def wait(self, milliseconds: int = -1) -> bool:
        with self._thread_lock:
            thread = self._thread
        if thread is None or thread is threading.current_thread():
            return True
        timeout = None if milliseconds < 0 else milliseconds / 1000.0
        thread.join(timeout=timeout)
        return not thread.is_alive()

    def _run(self):
        activation_attempted = False
        activated = False
        try:
            if not can_use_go_tun_lifecycle(self._bridge):
                raise RuntimeError(
                    "Go engine does not advertise managed TUN lifecycle"
                )
            if not self._executable:
                raise RuntimeError("bin/sing-box.exe was not found")
            activation_attempted = True
            result = self._bridge.request(
                "tun.activate",
                {
                    "executable": self._executable,
                    "config_path": self._config_path,
                    "startup_timeout_ms": 1500,
                },
                timeout=40.0,
            )
            tun_status = (
                result.get("tun", {}) if isinstance(result, dict) else {}
            )
            if (
                not isinstance(tun_status, dict)
                or tun_status.get("state") != "running"
            ):
                raise RuntimeError(
                    "Go engine did not confirm stable TUN activation"
                )
            activated = True
            pid = int(tun_status.get("pid", 0) or 0)
            self.log_signal.emit(
                f"[GoEngine][TUN] managed sing-box is stable (PID={pid})"
            )
            self.started_ok.emit(f"pid={pid}")

            while not self._stop_requested.wait(1.0):
                status = self._bridge.request("tun.status", timeout=3.0)
                if not isinstance(status, dict):
                    continue
                state = str(status.get("state") or "")
                if state == "running":
                    continue
                if state == "failed":
                    detail = str(
                        status.get("last_error")
                        or "managed sing-box exited unexpectedly"
                    )
                    raise RuntimeError(detail)
                if state == "stopped":
                    raise RuntimeError("managed sing-box stopped unexpectedly")
        except EngineClientError as exc:
            if not self._stop_requested.is_set():
                self.error_signal.emit(f"Go TUN lifecycle error: {exc}")
        except Exception as exc:
            if not self._stop_requested.is_set():
                self.error_signal.emit(
                    f"Go TUN lifecycle failure: {type(exc).__name__}: {exc}"
                )
        finally:
            if activation_attempted and self._bridge.is_running():
                try:
                    self._bridge.request("tun.deactivate", timeout=20.0)
                except Exception as exc:
                    if activated:
                        self.log_signal.emit(
                            f"[GoEngine][TUN] sidecar stop warning: {exc}"
                        )
            with self._thread_lock:
                self._running = False
            self.stopped.emit(
                "Managed TUN kernel stopped"
                if activated
                else "Managed TUN kernel did not start"
            )
            self.finished.emit()
