from __future__ import annotations

import time

from PySide6.QtCore import QCoreApplication

from ui.go_tun_manager import GoTunManager, can_use_go_tun_lifecycle


class _FakeBridge:
    def __init__(self):
        self.requests: list[tuple[str, object]] = []
        self.features = {"managed_tun_lifecycle"}
        self.methods = {"tun.activate", "tun.status", "tun.deactivate"}
        self.status = {
            "state": "running",
            "pid": 4321,
            "config_path": "config.json",
        }
        self.fail_activate = False

    def is_running(self):
        return True

    def supports(self, method: str):
        return method in self.methods

    def supports_mode_feature(self, mode: str, feature: str):
        return mode == "tun_tcp_pool" and feature in self.features

    def request(self, method: str, params=None, *, timeout=None):
        self.requests.append((method, params))
        if method == "tun.activate":
            if self.fail_activate:
                raise RuntimeError("synthetic activation failure")
            return {"accepted": True, "tun": dict(self.status)}
        if method == "tun.status":
            return dict(self.status)
        if method == "tun.deactivate":
            self.status = {"state": "stopped"}
            return {
                "accepted": True,
                "tun": dict(self.status),
            }
        raise AssertionError(method)


def _process_events_until(predicate, timeout: float = 3.0) -> bool:
    app = QCoreApplication.instance() or QCoreApplication([])
    deadline = time.monotonic() + timeout
    while not predicate() and time.monotonic() < deadline:
        app.processEvents()
        time.sleep(0.01)
    app.processEvents()
    return predicate()


def _manager(bridge: _FakeBridge, tmp_path) -> GoTunManager:
    executable = tmp_path / "sing-box.exe"
    executable.write_bytes(b"test")
    config = tmp_path / "config.json"
    config.write_text("{}", encoding="utf-8")
    return GoTunManager(
        bridge,
        str(config),
        executable=str(executable),
    )


def test_go_tun_manager_activates_polls_and_deactivates(tmp_path):
    bridge = _FakeBridge()
    manager = _manager(bridge, tmp_path)
    started: list[str] = []
    stopped: list[str] = []
    errors: list[str] = []
    manager.started_ok.connect(started.append)
    manager.stopped.connect(stopped.append)
    manager.error_signal.connect(errors.append)

    manager.start()
    assert _process_events_until(lambda: bool(started))
    assert started == ["pid=4321"]
    assert manager.isRunning()

    manager.stop()
    assert _process_events_until(lambda: bool(stopped))
    assert manager.wait(1000)
    assert not manager.isRunning()
    assert not errors
    methods = [method for method, _ in bridge.requests]
    assert methods[0] == "tun.activate"
    assert methods[-1] == "tun.deactivate"
    activate_params = bridge.requests[0][1]
    assert activate_params["startup_timeout_ms"] == 1500
    assert activate_params["config_path"].endswith("config.json")


def test_go_tun_manager_surfaces_managed_sidecar_failure(tmp_path):
    bridge = _FakeBridge()
    manager = _manager(bridge, tmp_path)
    started: list[str] = []
    errors: list[str] = []
    manager.started_ok.connect(started.append)
    manager.error_signal.connect(errors.append)

    manager.start()
    assert _process_events_until(lambda: bool(started))
    bridge.status = {
        "state": "failed",
        "last_error": "FwpmEngineOpen0 synthetic failure",
    }
    assert _process_events_until(lambda: bool(errors), timeout=2.5)
    assert "FwpmEngineOpen0 synthetic failure" in errors[-1]
    assert manager.wait(2000)


def test_go_tun_manager_rolls_back_partial_activation(tmp_path):
    bridge = _FakeBridge()
    bridge.fail_activate = True
    manager = _manager(bridge, tmp_path)
    errors: list[str] = []
    stopped: list[str] = []
    manager.error_signal.connect(errors.append)
    manager.stopped.connect(stopped.append)

    manager.start()
    assert _process_events_until(lambda: bool(stopped))
    assert errors and "synthetic activation failure" in errors[-1]
    assert [method for method, _ in bridge.requests] == [
        "tun.activate",
        "tun.deactivate",
    ]


def test_go_tun_lifecycle_requires_methods_and_feature():
    bridge = _FakeBridge()
    assert can_use_go_tun_lifecycle(bridge)
    bridge.features.clear()
    assert not can_use_go_tun_lifecycle(bridge)
    bridge.features.add("managed_tun_lifecycle")
    bridge.methods.remove("tun.deactivate")
    assert not can_use_go_tun_lifecycle(bridge)
