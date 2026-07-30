from __future__ import annotations

import time

from PySide6.QtCore import QCoreApplication

from ui.go_proxy_worker import GoProxyWorker


class _FakeBridge:
    def __init__(self):
        self.requests: list[tuple[str, object]] = []

    def is_running(self):
        return True

    def supports(self, method: str):
        return method in {"engine.start", "engine.stop", "engine.telemetry"}

    def request(self, method: str, params=None, *, timeout=None):
        self.requests.append((method, params))
        if method == "engine.start":
            return {
                "endpoints": {
                    "socks": "127.0.0.1:10800",
                    "http": "127.0.0.1:10801",
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
                    }
                ]
            }
        if method == "engine.stop":
            return {"accepted": True}
        raise AssertionError(method)


def _process_events_until(predicate, timeout: float = 3.0) -> bool:
    app = QCoreApplication.instance() or QCoreApplication([])
    deadline = time.monotonic() + timeout
    while not predicate() and time.monotonic() < deadline:
        app.processEvents()
        time.sleep(0.01)
    app.processEvents()
    return predicate()


def test_go_proxy_worker_matches_proxy_worker_lifecycle_contract():
    bridge = _FakeBridge()
    worker = GoProxyWorker(
        bridge=bridge,
        selected_nics=[
            {
                "name": "Ethernet",
                "ip": "192.0.2.10",
                "if_index": 11,
                "dns_servers": ["192.0.2.53"],
            }
        ],
        listen_host="127.0.0.1",
        listen_port=10800,
        http_port=10801,
        use_weighted=True,
        bandwidth_limits={"Ethernet": 3},
    )
    started: list[str] = []
    stopped: list[str] = []
    telemetry: list[dict] = []
    errors: list[str] = []
    worker.started_ok.connect(started.append)
    worker.stopped.connect(stopped.append)
    worker.traffic_signal.connect(telemetry.append)
    worker.error_signal.connect(errors.append)

    worker.start()
    assert _process_events_until(lambda: bool(started))
    assert started == [
        "socks=127.0.0.1:10800;http=127.0.0.1:10801"
    ]
    assert _process_events_until(lambda: bool(telemetry), timeout=2.0)
    assert telemetry[-1]["Ethernet"]["connections"] == 2

    worker.stop()
    assert _process_events_until(lambda: bool(stopped))
    assert worker.wait(1000)
    assert not worker.isRunning()
    assert not errors
    assert [method for method, _ in bridge.requests].count("engine.start") == 1
    assert [method for method, _ in bridge.requests].count("engine.stop") == 1
    start_params = bridge.requests[0][1]
    assert start_params["adapters"][0]["weight"] == 3
    assert start_params["adapters"][0]["dns_servers"] == ["192.0.2.53"]
    assert start_params["dns"]["policy"] == "auto"
    assert start_params["dns"]["legacy_servers"] == ["223.5.5.5"]
