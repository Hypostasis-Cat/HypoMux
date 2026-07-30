from __future__ import annotations

import sys
import textwrap
import time

from PySide6.QtCore import QCoreApplication

from ui.engine_bridge import EngineBridge


BRIDGE_HOST_SOURCE = r"""
import json
import sys

def send(message):
    sys.stdout.write(json.dumps(message, separators=(",", ":")) + "\n")
    sys.stdout.flush()

for line in sys.stdin:
    request = json.loads(line)
    request_id = request["id"]
    if request["method"] == "engine.hello":
        send({
            "protocol": 1,
            "id": request_id,
            "result": {
                "protocol_version": 1,
                "engine": "bridge-test",
                "capabilities": [
                    "engine.hello",
                    "engine.status",
                    "health.check",
                    "host.shutdown",
                ],
                "modes": ["proxy", "tun_tcp_pool"],
                "mode_features": {
                    "proxy": ["tcp_connect"],
                    "tun_tcp_pool": ["tcp_connect", "udp_associate"],
                },
            },
        })
    elif request["method"] == "host.shutdown":
        send({
            "protocol": 1,
            "id": request_id,
            "result": {"accepted": True},
        })
        send({
            "protocol": 1,
            "sequence": 1,
            "event": "dns.fallback_required",
            "data": {
                "adapter": "Ethernet",
                "policy": "alidns",
                "reason": "test failure",
            },
        })
        send({
            "protocol": 1,
            "sequence": 2,
            "event": "host.exiting",
            "data": {"reason": "requested"},
        })
        break
"""


def _process_events_until(predicate, timeout: float = 2.0) -> bool:
    app = QCoreApplication.instance() or QCoreApplication([])
    deadline = time.monotonic() + timeout
    while not predicate() and time.monotonic() < deadline:
        app.processEvents()
        time.sleep(0.01)
    app.processEvents()
    return predicate()


def test_qt_bridge_connects_without_blocking_and_stops_the_host():
    connected: list[dict] = []
    errors: list[str] = []
    disconnected: list[str] = []
    dns_fallbacks: list[dict] = []
    bridge = EngineBridge(
        [
            sys.executable,
            "-u",
            "-c",
            textwrap.dedent(BRIDGE_HOST_SOURCE),
        ]
    )
    bridge.connected.connect(connected.append)
    bridge.engine_error.connect(errors.append)
    bridge.disconnected.connect(disconnected.append)
    bridge.dns_fallback_required.connect(dns_fallbacks.append)

    started = time.monotonic()
    bridge.start()
    assert time.monotonic() - started < 0.2
    assert _process_events_until(lambda: bool(connected))
    assert connected[0]["engine"] == "bridge-test"
    assert not errors
    assert bridge.is_running()
    assert bridge.supports_mode_feature("tun_tcp_pool", "udp_associate")
    assert not bridge.supports_mode_feature("proxy", "udp_associate")

    bridge.stop()
    assert _process_events_until(lambda: bool(disconnected))
    assert disconnected == [""]
    assert dns_fallbacks == [
        {
            "adapter": "Ethernet",
            "policy": "alidns",
            "reason": "test failure",
        }
    ]
    assert not bridge.is_running()
