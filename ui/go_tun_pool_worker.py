"""Qt-compatible TUN pool worker backed by the persistent Go engine."""

from __future__ import annotations

import threading
import time
from typing import Any, Callable

from PySide6.QtCore import QObject, Signal

from engine_client import EngineClientError
from proxy_worker import (
    PORT_AGGREGATION,
    PORT_ETHERNET,
    PORT_WIFI,
    classify_nics,
)
from ui.engine_bridge import EngineBridge
from utils.tun_dns_planner import TunDnsPlanner


REQUIRED_TUN_FEATURES = ("tcp_connect", "udp_associate")


def can_use_go_tun_pool(bridge: EngineBridge | None) -> bool:
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
        feature_checker("tun_tcp_pool", feature)
        for feature in REQUIRED_TUN_FEATURES
    )


class GoTunPoolWorker(QObject):
    """Mirror the MultiPortProxyWorker surface used by MainWindow."""

    log_signal = Signal(str)
    traffic_signal = Signal(dict)
    connectivity_signal = Signal(str)
    dns_compatibility_required = Signal(str)
    started_ok = Signal(str)
    stopped = Signal(str)
    error_signal = Signal(str)
    finished = Signal()

    def __init__(
        self,
        bridge: EngineBridge,
        selected_nics: list[dict[str, Any]],
        listen_host: str = "127.0.0.1",
        ethernet_port: int = PORT_ETHERNET,
        wifi_port: int = PORT_WIFI,
        aggregation_port: int = PORT_AGGREGATION,
        use_weighted: bool = False,
        bandwidth_limits: dict[str, int] | None = None,
        allow_degraded_start: bool = False,
        parent: QObject | None = None,
        planner_factory: Callable[..., TunDnsPlanner] = TunDnsPlanner,
    ):
        super().__init__(parent)
        self._bridge = bridge
        self._selected_nics = [dict(nic) for nic in selected_nics]
        self._listen_host = str(listen_host)
        self._requested_ports = {
            "nic_ethernet": int(ethernet_port),
            "nic_wifi": int(wifi_port),
            "aggregation": int(aggregation_port),
        }
        self._ports = dict(self._requested_ports)
        self._use_weighted = bool(use_weighted)
        self._weights = dict(bandwidth_limits or {})
        self._planner = planner_factory(
            self._selected_nics,
            allow_degraded_start=allow_degraded_start,
        )
        self._planner.set_log_callback(self.log_signal.emit)
        self._stop_requested = threading.Event()
        self._thread: threading.Thread | None = None
        self._thread_lock = threading.Lock()
        self._state_lock = threading.Lock()
        self._running = False
        self._last_connectivity_emit = 0.0
        self._dns_health_failures = 0
        self._dns_signal_emitted = False

    def set_dns_servers(self, servers: list[str]):
        self._planner.set_dns_servers(servers)

    def set_doh_provider(self, provider: str):
        self._planner.set_doh_provider(provider)

    def set_dns_mode_override(self, mode: str):
        self._planner.set_dns_mode_override(mode)

    def listen_ports(self) -> dict[str, int]:
        with self._state_lock:
            return dict(self._ports)

    def selected_nics_snapshot(self) -> list[dict]:
        return self._planner.selected_nics_snapshot()

    def dns_mode(self) -> str:
        return self._planner.dns_mode()

    def singbox_dns_plan(self) -> dict[str, object]:
        return self._planner.singbox_dns_plan()

    def start(self):
        with self._thread_lock:
            if self._thread is not None and self._thread.is_alive():
                return
            self._stop_requested.clear()
            self._running = True
            self._thread = threading.Thread(
                target=self._run,
                name="HypoMuxGoTunPoolWorker",
                daemon=True,
            )
            self._thread.start()

    def stop(self):
        self._stop_requested.set()
        self._planner.cancel()

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
        start_attempted = False
        started = False
        try:
            if not can_use_go_tun_pool(self._bridge):
                raise RuntimeError(
                    "Go engine does not advertise the complete TUN TCP/UDP pool"
                )
            ready, details = self._planner.prepare()
            for detail in details:
                self.log_signal.emit(f"[GoEngine][TUN][DNS preflight] {detail}")
            if self._stop_requested.is_set():
                return
            if not ready:
                raise RuntimeError(
                    "selected adapters have no verified DNS path for sing-box"
                )

            config = self._build_engine_config()
            start_attempted = True
            result = self._bridge.request("engine.start", config, timeout=10.0)
            endpoints = result.get("endpoints", {}) if isinstance(result, dict) else {}
            channels = endpoints.get("channels", {}) if isinstance(endpoints, dict) else {}
            ports = self._parse_channel_ports(channels)
            with self._state_lock:
                self._ports = ports
            started = True
            info = (
                f"ethernet={ports['nic_ethernet']};"
                f"wifi={ports['nic_wifi']};"
                f"aggregation={ports['aggregation']}"
            )
            self.log_signal.emit(
                "[GoEngine][TUN] Go TCP/UDP outbound pool started | " + info
            )
            self.started_ok.emit(info)
            self._poll_runtime()
        except EngineClientError as exc:
            if not self._stop_requested.is_set():
                self.error_signal.emit(f"Go TUN pool engine error: {exc}")
        except Exception as exc:
            if not self._stop_requested.is_set():
                self.error_signal.emit(
                    f"Go TUN pool failure: {type(exc).__name__}: {exc}"
                )
        finally:
            if start_attempted and self._bridge.is_running():
                try:
                    self._bridge.request("engine.stop", timeout=6.0)
                except EngineClientError as exc:
                    self.log_signal.emit(
                        f"[GoEngine][TUN] engine stop warning: {exc}"
                    )
            with self._thread_lock:
                self._running = False
            self.stopped.emit(
                "Go TUN outbound pool stopped"
                if started
                else "Go TUN outbound pool did not start"
            )
            self.finished.emit()

    def _build_engine_config(self) -> dict[str, object]:
        selected = self.selected_nics_snapshot()
        wired, wifi = classify_nics(selected)
        wired = wired or selected
        wifi = wifi or selected

        def adapter_name(nic: dict) -> str:
            return str(nic.get("name") or nic.get("alias") or "")

        def channel_names(nics: list[dict]) -> list[str]:
            return [adapter_name(nic) for nic in nics]

        adapters = []
        for nic in selected:
            name = adapter_name(nic)
            dns_servers = nic.get("dns_servers")
            adapters.append(
                {
                    "name": name,
                    "source_ip": str(nic.get("ip") or nic.get("ipv4") or ""),
                    "if_index": int(
                        nic.get("if_index", nic.get("index", 0)) or 0
                    ),
                    "weight": max(int(self._weights.get(name, 1) or 1), 1),
                    "dns_servers": [
                        str(server).strip()
                        for server in (
                            dns_servers if isinstance(dns_servers, list) else []
                        )
                        if str(server).strip()
                    ],
                }
            )
        return {
            "mode": "tun_tcp_pool",
            "listen_host": self._listen_host,
            "weighted": self._use_weighted,
            "connect_timeout_ms": 6000,
            "adapters": adapters,
            "channels": [
                {
                    "name": "nic_ethernet",
                    "port": self._requested_ports["nic_ethernet"],
                    "adapter_names": channel_names(wired),
                },
                {
                    "name": "nic_wifi",
                    "port": self._requested_ports["nic_wifi"],
                    "adapter_names": channel_names(wifi),
                },
                {
                    "name": "aggregation",
                    "port": self._requested_ports["aggregation"],
                    "adapter_names": channel_names(selected),
                },
            ],
        }

    @staticmethod
    def _parse_channel_ports(channels: object) -> dict[str, int]:
        if not isinstance(channels, dict):
            raise RuntimeError("Go engine returned no TUN channel endpoints")
        result: dict[str, int] = {}
        for name in ("nic_ethernet", "nic_wifi", "aggregation"):
            endpoint = str(channels.get(name) or "")
            try:
                host, port_text = endpoint.rsplit(":", 1)
                port = int(port_text)
            except (ValueError, TypeError):
                raise RuntimeError(
                    f"Go engine returned an invalid {name} endpoint"
                ) from None
            if host not in {"127.0.0.1", "localhost"} or not 0 < port <= 65535:
                raise RuntimeError(
                    f"Go engine returned a non-loopback {name} endpoint"
                )
            result[name] = port
        return result

    def _poll_runtime(self):
        previous: dict[str, tuple[int, int]] = {}
        previous_at = time.monotonic()
        next_dns_probe = (
            time.monotonic() + float(self._planner.DNS_HEALTH_INTERVAL)
        )
        while not self._stop_requested.wait(1.0):
            snapshot = self._bridge.request(
                "engine.telemetry",
                {"include_connections": True},
                timeout=3.0,
            )
            if isinstance(snapshot, dict):
                previous, previous_at = self._emit_telemetry(
                    snapshot,
                    previous,
                    previous_at,
                )
            if (
                self.dns_mode() == self._planner.DNS_MODE_DOH_STRICT
                and time.monotonic() >= next_dns_probe
            ):
                self._probe_doh_health()
                next_dns_probe = (
                    time.monotonic()
                    + float(self._planner.DNS_HEALTH_INTERVAL)
                )

    def _emit_telemetry(
        self,
        snapshot: dict,
        previous: dict[str, tuple[int, int]],
        previous_at: float,
    ) -> tuple[dict[str, tuple[int, int]], float]:
        sampled_at = time.monotonic()
        elapsed = max(sampled_at - previous_at, 0.001)
        payload: dict[str, dict[str, Any]] = {}
        current: dict[str, tuple[int, int]] = {}
        total_down = total_up = 0.0
        total_connections = 0
        downstream_delta = 0
        adapters = snapshot.get("adapters", [])
        if not isinstance(adapters, list):
            adapters = []
        for adapter in adapters:
            if not isinstance(adapter, dict):
                continue
            name = str(adapter.get("name") or "")
            bytes_up = int(adapter.get("bytes_up", 0) or 0)
            bytes_down = int(adapter.get("bytes_down", 0) or 0)
            old_up, old_down = previous.get(name, (0, 0))
            up_bytes = max(bytes_up - old_up, 0)
            down_bytes = max(bytes_down - old_down, 0)
            up = up_bytes / 1024 / 1024 / elapsed
            down = down_bytes / 1024 / 1024 / elapsed
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
            downstream_delta += down_bytes
        payload["_total"] = {
            "down_mbps": round(total_down, 2),
            "up_mbps": round(total_up, 2),
            "connections": total_connections,
        }
        self.traffic_signal.emit(payload)
        if downstream_delta > 0:
            now = time.monotonic()
            if now - self._last_connectivity_emit >= 0.5:
                self._last_connectivity_emit = now
                self.connectivity_signal.emit(
                    f"Go TUN pool received {downstream_delta} upstream bytes"
                )
        return current, sampled_at

    def _probe_doh_health(self):
        try:
            self._planner.probe_selected_doh()
            if self._dns_health_failures:
                self.log_signal.emit(
                    "[GoEngine][TUN][DNS health] selected DoH egress recovered"
                )
            self._dns_health_failures = 0
        except Exception as exc:
            self._dns_health_failures += 1
            self.log_signal.emit(
                "[GoEngine][TUN][DNS health] probe failed "
                f"{self._dns_health_failures}/"
                f"{self._planner.DNS_HEALTH_FAILURE_LIMIT}: "
                f"{type(exc).__name__}"
            )
            if (
                self._dns_health_failures
                >= self._planner.DNS_HEALTH_FAILURE_LIMIT
                and not self._dns_signal_emitted
            ):
                self._dns_signal_emitted = True
                self.dns_compatibility_required.emit(
                    "sing-box selected DoH egress failed three consecutive probes"
                )
