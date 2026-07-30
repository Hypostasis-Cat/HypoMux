"""Qt-compatible development worker backed by the persistent Go engine."""

from __future__ import annotations

import threading
import time
from typing import Any

from PySide6.QtCore import QObject, Signal

from engine_client import EngineClientError
from ui.engine_bridge import EngineBridge


REQUIRED_PROXY_FEATURES = (
    "socks5_connect",
    "http_connect",
    "source_bound_dns",
    "ipv6_egress",
)


def can_use_go_proxy(bridge: EngineBridge | None) -> bool:
    if bridge is None or not bridge.is_running():
        return False
    feature_checker = getattr(bridge, "supports_mode_feature", None)
    if not callable(feature_checker):
        return False
    if not all(
        bridge.supports(method)
        for method in ("engine.start", "engine.stop", "engine.telemetry")
    ):
        return False
    return all(
        feature_checker("proxy", feature)
        for feature in REQUIRED_PROXY_FEATURES
    )


class GoProxyWorker(QObject):
    """Mirror the ProxyWorker signal/lifecycle surface for staged migration."""

    log_signal = Signal(str)
    traffic_signal = Signal(dict)
    started_ok = Signal(str)
    stopped = Signal(str)
    error_signal = Signal(str)
    finished = Signal()

    def __init__(
        self,
        bridge: EngineBridge,
        selected_nics: list[dict[str, Any]],
        listen_host: str,
        listen_port: int,
        http_port: int,
        use_weighted: bool,
        bandwidth_limits: dict[str, int] | None = None,
        dns_server: str = "223.5.5.5",
        doh_provider: str = "auto",
        parent: QObject | None = None,
    ):
        super().__init__(parent)
        weights = bandwidth_limits or {}
        self._bridge = bridge
        self._config = {
            "mode": "proxy",
            "listen_host": listen_host,
            "socks_port": int(listen_port),
            "http_port": int(http_port),
            "weighted": bool(use_weighted),
            "adapters": [
                {
                    "name": str(nic.get("name") or nic.get("alias") or ""),
                    "source_ip": str(nic.get("ip") or nic.get("ipv4") or ""),
                    "source_ipv6": str(nic.get("ipv6") or ""),
                    "if_index": int(
                        nic.get("if_index", nic.get("index", 0)) or 0
                    ),
                    "ipv6_if_index": int(
                        nic.get(
                            "ipv6_if_index",
                            nic.get("if_index", nic.get("index", 0)),
                        )
                        or 0
                    ),
                    "weight": max(
                        int(
                            weights.get(
                                str(nic.get("name") or nic.get("alias") or ""),
                                1,
                            )
                            or 1
                        ),
                        1,
                    ),
                    "dns_servers": [
                        str(server).strip()
                        for server in (
                            nic.get("dns_servers")
                            if isinstance(nic.get("dns_servers"), list)
                            else []
                        )
                        if str(server).strip()
                    ],
                }
                for nic in selected_nics
            ],
            "dns": {
                "policy": str(doh_provider or "auto").strip().lower(),
                "legacy_servers": [str(dns_server or "223.5.5.5").strip()],
                "cache_ttl_ms": 180_000,
                "query_timeout_ms": 4_000,
            },
        }
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
                name="HypoMuxGoProxyWorker",
                daemon=True,
            )
            self._thread.start()

    def stop(self):
        self._stop_requested.set()

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
        started = False
        try:
            if not can_use_go_proxy(self._bridge):
                raise RuntimeError(
                    "Go engine does not advertise the complete dual-stack proxy"
                )
            result = self._bridge.request("engine.start", self._config, timeout=10.0)
            endpoints = result.get("endpoints", {}) if isinstance(result, dict) else {}
            socks = str(endpoints.get("socks", ""))
            http = str(endpoints.get("http", ""))
            started = True
            self.log_signal.emit(
                f"[GoEngine][TCP] Go proxy started | SOCKS {socks} | HTTP {http}"
            )
            self.started_ok.emit(f"socks={socks};http={http}")
            self._poll_telemetry()
        except EngineClientError as exc:
            self.error_signal.emit(f"Go proxy engine error: {exc}")
        except Exception as exc:
            self.error_signal.emit(
                f"Go proxy engine failure: {type(exc).__name__}: {exc}"
            )
        finally:
            if started and self._bridge.is_running():
                try:
                    self._bridge.request("engine.stop", timeout=6.0)
                except EngineClientError as exc:
                    self.log_signal.emit(f"[GoEngine][TCP] stop warning: {exc}")
            with self._thread_lock:
                self._running = False
            self.stopped.emit("Go proxy stopped")
            self.finished.emit()

    def _poll_telemetry(self):
        previous: dict[str, tuple[int, int]] = {}
        previous_at = time.monotonic()
        while not self._stop_requested.wait(1.0):
            snapshot = self._bridge.request("engine.telemetry", timeout=3.0)
            if not isinstance(snapshot, dict):
                continue
            sampled_at = time.monotonic()
            elapsed = max(sampled_at - previous_at, 0.001)
            payload: dict[str, dict[str, Any]] = {}
            total_down = total_up = 0.0
            total_connections = 0
            current: dict[str, tuple[int, int]] = {}
            adapters = snapshot.get("adapters", [])
            if not isinstance(adapters, list):
                adapters = []
            for adapter in adapters:
                if not isinstance(adapter, dict):
                    continue
                name = str(adapter.get("name", ""))
                bytes_up = int(adapter.get("bytes_up", 0) or 0)
                bytes_down = int(adapter.get("bytes_down", 0) or 0)
                old_up, old_down = previous.get(name, (bytes_up, bytes_down))
                up = max(bytes_up - old_up, 0) / 1024 / 1024 / elapsed
                down = max(bytes_down - old_down, 0) / 1024 / 1024 / elapsed
                connections = int(adapter.get("connections", 0) or 0)
                payload[name] = {
                    "index": int(adapter.get("if_index", 0) or 0),
                    "down_mbps": round(down, 2),
                    "up_mbps": round(up, 2),
                    "connections": connections,
                }
                current[name] = (bytes_up, bytes_down)
                total_down += down
                total_up += up
                total_connections += connections
            payload["_total"] = {
                "down_mbps": round(total_down, 2),
                "up_mbps": round(total_up, 2),
                "connections": total_connections,
            }
            self.traffic_signal.emit(payload)
            previous = current
            previous_at = sampled_at
