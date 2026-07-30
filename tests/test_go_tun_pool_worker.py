from __future__ import annotations

import time

from PySide6.QtCore import QCoreApplication

from ui.go_tun_pool_worker import GoTunPoolWorker, can_use_go_tun_pool
from utils.tun_dns_planner import TunDnsPlanner


class _FakeBridge:
    def __init__(
        self,
        *,
        features=(
            "tcp_connect",
            "udp_associate",
            "ipv6_egress",
            "adaptive_health",
        ),
    ):
        self.features = set(features)
        self.requests: list[tuple[str, object]] = []
        self.fail_start = False

    def is_running(self):
        return True

    def supports(self, method: str):
        return method in {"engine.start", "engine.stop", "engine.telemetry"}

    def supports_mode_feature(self, mode: str, feature: str):
        return mode == "tun_tcp_pool" and feature in self.features

    def request(self, method: str, params=None, *, timeout=None):
        self.requests.append((method, params))
        if method == "engine.start":
            if self.fail_start:
                raise RuntimeError("synthetic start failure")
            return {
                "endpoints": {
                    "channels": {
                        "nic_ethernet": "127.0.0.1:2001",
                        "nic_wifi": "127.0.0.1:2002",
                        "aggregation": "127.0.0.1:2003",
                    }
                }
            }
        if method == "engine.telemetry":
            return {
                "adapters": [
                    {
                        "name": "Ethernet",
                        "if_index": 11,
                        "connections": 2,
                        "bytes_up": 1024,
                        "bytes_down": 2048,
                        "health_state": "probing",
                        "consecutive_failures": 1,
                        "domain_quarantines": 0,
                    }
                ]
            }
        if method == "engine.stop":
            return {"accepted": True}
        raise AssertionError(method)


class _FakePlanner:
    DNS_MODE_DOH_STRICT = "doh_strict"
    DNS_MODE_LEGACY_COMPAT = "legacy_compat"
    DNS_HEALTH_INTERVAL = 60.0
    DNS_HEALTH_FAILURE_LIMIT = 3

    def __init__(self, selected_nics, *, allow_degraded_start=False):
        self.selected_nics = [dict(nic) for nic in selected_nics]
        self.allow_degraded_start = allow_degraded_start
        self.mode = self.DNS_MODE_DOH_STRICT
        self.cancelled = False
        self.probe_error: Exception | None = None
        self.logs = None

    def set_log_callback(self, callback):
        self.logs = callback

    def set_dns_servers(self, servers):
        self.servers = list(servers)

    def set_doh_provider(self, provider):
        self.provider = provider

    def set_dns_mode_override(self, mode):
        if mode:
            self.mode = mode

    def cancel(self):
        self.cancelled = True

    def prepare(self):
        return True, ["DNS route verified"]

    def selected_nics_snapshot(self):
        return [dict(nic) for nic in self.selected_nics]

    def dns_mode(self):
        return self.mode

    def singbox_dns_plan(self):
        return {
            "mode": "doh",
            "server": "1.1.1.1",
            "bind_interface": "Ethernet",
        }

    def probe_selected_doh(self):
        if self.probe_error is not None:
            raise self.probe_error


def _process_events_until(predicate, timeout: float = 3.0) -> bool:
    app = QCoreApplication.instance() or QCoreApplication([])
    deadline = time.monotonic() + timeout
    while not predicate() and time.monotonic() < deadline:
        app.processEvents()
        time.sleep(0.01)
    app.processEvents()
    return predicate()


def _worker(bridge: _FakeBridge) -> GoTunPoolWorker:
    return GoTunPoolWorker(
        bridge=bridge,
        selected_nics=[
            {
                "name": "Ethernet",
                "ip": "192.0.2.10",
                "ipv6": "2001:db8:1::10",
                "if_index": 11,
                "ipv6_if_index": 21,
                "iftype": 6,
                "dns_servers": ["192.0.2.53"],
            },
            {
                "name": "Wi-Fi",
                "ip": "198.51.100.10",
                "ipv6": "2001:db8:2::10",
                "if_index": 12,
                "ipv6_if_index": 22,
                "iftype": 71,
                "dns_servers": ["198.51.100.53"],
            },
        ],
        use_weighted=True,
        bandwidth_limits={"Ethernet": 3, "Wi-Fi": 2},
        planner_factory=_FakePlanner,
    )


def test_go_tun_pool_worker_matches_multi_port_lifecycle_contract():
    bridge = _FakeBridge()
    worker = _worker(bridge)
    started: list[str] = []
    stopped: list[str] = []
    telemetry: list[dict] = []
    connectivity: list[str] = []
    errors: list[str] = []
    worker.started_ok.connect(started.append)
    worker.stopped.connect(stopped.append)
    worker.traffic_signal.connect(telemetry.append)
    worker.connectivity_signal.connect(connectivity.append)
    worker.error_signal.connect(errors.append)

    worker.start()
    assert _process_events_until(lambda: bool(started))
    assert started == ["ethernet=2001;wifi=2002;aggregation=2003"]
    assert worker.listen_ports() == {
        "nic_ethernet": 2001,
        "nic_wifi": 2002,
        "aggregation": 2003,
    }
    assert _process_events_until(lambda: bool(telemetry), timeout=2.0)
    assert telemetry[-1]["Ethernet"]["connections"] == 2
    assert telemetry[-1]["Ethernet"]["health_state"] == "probing"
    assert telemetry[-1]["Ethernet"]["consecutive_failures"] == 1
    assert connectivity

    worker.stop()
    assert _process_events_until(lambda: bool(stopped))
    assert worker.wait(1000)
    assert not worker.isRunning()
    assert not errors

    methods = [method for method, _ in bridge.requests]
    assert methods.count("engine.start") == 1
    assert methods.count("engine.stop") == 1
    start_params = bridge.requests[0][1]
    assert start_params["mode"] == "tun_tcp_pool"
    assert start_params["weighted"] is True
    assert start_params["adapters"][0]["weight"] == 3
    assert start_params["adapters"][1]["weight"] == 2
    assert start_params["adapters"][0]["source_ipv6"] == "2001:db8:1::10"
    assert start_params["adapters"][0]["ipv6_if_index"] == 21
    channels = {
        channel["name"]: channel["adapter_names"]
        for channel in start_params["channels"]
    }
    assert channels == {
        "nic_ethernet": ["Ethernet"],
        "nic_wifi": ["Wi-Fi"],
        "aggregation": ["Ethernet", "Wi-Fi"],
    }


def test_go_tun_pool_requires_adaptive_health_feature():
    assert can_use_go_tun_pool(_FakeBridge())
    assert not can_use_go_tun_pool(
        _FakeBridge(
            features=("tcp_connect", "udp_associate", "ipv6_egress")
        )
    )


def test_dns_planner_produces_singbox_plan_without_starting_python_pool():
    planner = TunDnsPlanner(
        [
            {
                "name": "Ethernet",
                "ip": "192.0.2.10",
                "index": 11,
                "dns_servers": ["192.0.2.53"],
            }
        ],
        allow_degraded_start=True,
    )
    planner.set_doh_provider("off")

    ready, details = planner.prepare()

    assert ready
    assert details
    assert planner.dns_mode() == planner.DNS_MODE_LEGACY_COMPAT
    assert planner.singbox_dns_plan() == {
        "bind_interface": "Ethernet",
        "bind_ip": "192.0.2.10",
        "mode": "legacy",
        "server": "192.0.2.53",
        "server_port": 53,
    }
    assert not planner._worker.isRunning()


def test_partial_go_tun_start_is_rolled_back():
    bridge = _FakeBridge()
    bridge.fail_start = True
    worker = _worker(bridge)
    errors: list[str] = []
    stopped: list[str] = []
    worker.error_signal.connect(errors.append)
    worker.stopped.connect(stopped.append)

    worker.start()
    assert _process_events_until(lambda: bool(stopped))
    assert errors and "synthetic start failure" in errors[0]
    assert [method for method, _ in bridge.requests] == [
        "engine.start",
        "engine.stop",
    ]


def test_persistent_doh_failure_requests_one_compatibility_restart():
    worker = _worker(_FakeBridge())
    planner = worker._planner
    planner.probe_error = TimeoutError("synthetic DoH timeout")
    fallbacks: list[str] = []
    worker.dns_compatibility_required.connect(fallbacks.append)

    for _ in range(5):
        worker._probe_doh_health()

    assert len(fallbacks) == 1
    assert "three consecutive" in fallbacks[0]
