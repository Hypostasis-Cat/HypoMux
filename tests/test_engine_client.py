from __future__ import annotations

import json
import os
from pathlib import Path
import socket
import struct
import sys
import tempfile
import textwrap
import threading
import time
import unittest

from engine_client import (
    EngineClient,
    EngineProcessError,
    EngineProtocolError,
    EngineRemoteError,
    EngineStateError,
    EngineTimeoutError,
    development_engine_enabled,
    go_proxy_development_enabled,
    go_tun_development_enabled,
    resolve_development_engine_command,
)
from engine_client.process import start_engine_process


def _recv_exact(connection: socket.socket, size: int) -> bytes:
    chunks = bytearray()
    while len(chunks) < size:
        chunk = connection.recv(size - len(chunks))
        if not chunk:
            raise ConnectionError("socket closed before expected payload")
        chunks.extend(chunk)
    return bytes(chunks)


FAKE_ENGINE_SOURCE = r"""
import json
import os
import sys
import time

mode = sys.argv[1]
event_sequence = 0

def send(message):
    sys.stdout.write(json.dumps(message, separators=(",", ":")) + "\n")
    sys.stdout.flush()

for raw_line in sys.stdin:
    request = json.loads(raw_line)
    request_id = request.get("id", "")
    method = request.get("method", "")

    if method == "engine.hello":
        if mode == "stderr":
            for index in range(500):
                sys.stderr.write("diagnostic-%d\n" % index)
            sys.stderr.flush()
        send({
            "protocol": 1,
            "id": request_id,
            "result": {
                "protocol_version": 1,
                "engine": "fake-engine",
                "capabilities": [
                    "engine.hello",
                    "engine.status",
                    "health.check",
                    "host.shutdown",
                ],
            },
        })
    elif method == "health.check":
        if mode == "timeout":
            time.sleep(10)
        elif mode == "malformed":
            sys.stdout.write("{not-json}\n")
            sys.stdout.flush()
            time.sleep(10)
        elif mode == "crash":
            os._exit(7)
        else:
            send({
                "protocol": 1,
                "id": request_id,
                "result": {"ok": True, "state": "stopped"},
            })
    elif method == "engine.status":
        send({
            "protocol": 1,
            "id": request_id,
            "result": {"engine": {"state": "stopped", "sequence": 0}},
        })
    elif method == "host.shutdown":
        if mode == "ignore-shutdown":
            continue
        send({
            "protocol": 1,
            "id": request_id,
            "result": {"accepted": True},
        })
        event_sequence += 1
        send({
            "protocol": 1,
            "sequence": event_sequence,
            "event": "host.exiting",
            "data": {"reason": "requested"},
        })
        break
    else:
        send({
            "protocol": 1,
            "id": request_id,
            "error": {
                "code": "method_not_found",
                "message": "unknown method",
                "details": {"method": method},
            },
        })
"""


class EngineClientTests(unittest.TestCase):
    def _command(self, mode: str) -> list[str]:
        return [sys.executable, "-u", "-c", textwrap.dedent(FAKE_ENGINE_SOURCE), mode]

    def test_handshake_requests_events_and_graceful_shutdown(self):
        events: list[dict] = []
        disconnects: list[object] = []
        client = EngineClient(
            self._command("normal"),
            on_event=events.append,
            on_disconnect=disconnects.append,
        )
        try:
            hello = client.start()
            self.assertEqual(hello["engine"], "fake-engine")
            self.assertTrue(client.is_running())
            self.assertEqual(client.request("health.check")["state"], "stopped")
            self.assertEqual(
                client.request("engine.status")["engine"]["state"],
                "stopped",
            )
            with self.assertRaises(EngineRemoteError) as raised:
                client.request("missing.method")
            self.assertEqual(raised.exception.code, "method_not_found")
            self.assertEqual(client.stop(), "exited")
            self.assertFalse(client.is_running())
            self.assertEqual(events[-1]["event"], "host.exiting")
            self.assertEqual(disconnects, [None])
        finally:
            client.stop(graceful=False)

    def test_request_timeout_does_not_block_for_late_response(self):
        client = EngineClient(
            self._command("timeout"),
            request_timeout=0.1,
            shutdown_timeout=0.1,
        )
        try:
            client.start()
            started = time.monotonic()
            with self.assertRaises(EngineTimeoutError):
                client.request("health.check")
            self.assertLess(time.monotonic() - started, 1.0)
        finally:
            client.stop(graceful=False)

    def test_invalid_request_parameters_do_not_poison_the_connection(self):
        client = EngineClient(self._command("normal"))
        try:
            client.start()
            with self.assertRaises(EngineProtocolError):
                client.request("health.check", {"invalid": object()})
            self.assertTrue(client.request("health.check")["ok"])
        finally:
            client.stop()

    def test_malformed_output_aborts_the_protocol(self):
        disconnects: list[object] = []
        client = EngineClient(
            self._command("malformed"),
            request_timeout=1.0,
            shutdown_timeout=0.1,
            on_disconnect=disconnects.append,
        )
        try:
            client.start()
            with self.assertRaises(EngineProtocolError):
                client.request("health.check")
            self.assertIsInstance(disconnects[0], EngineProtocolError)
            with self.assertRaises(EngineStateError):
                client.request("engine.status")
        finally:
            client.stop(graceful=False)

    def test_process_crash_releases_pending_request(self):
        client = EngineClient(
            self._command("crash"),
            request_timeout=2.0,
            shutdown_timeout=0.1,
        )
        try:
            client.start()
            with self.assertRaises(EngineProcessError) as raised:
                client.request("health.check")
            self.assertEqual(raised.exception.returncode, 7)
        finally:
            client.stop(graceful=False)

    def test_stderr_is_drained_without_deadlocking_handshake(self):
        stderr_lines: list[str] = []
        client = EngineClient(
            self._command("stderr"),
            on_stderr=stderr_lines.append,
        )
        try:
            client.start()
            deadline = time.monotonic() + 1.0
            while len(stderr_lines) < 500 and time.monotonic() < deadline:
                time.sleep(0.01)
            self.assertGreaterEqual(len(stderr_lines), 500)
        finally:
            client.stop()

    def test_unresponsive_shutdown_is_forced_and_bounded(self):
        client = EngineClient(
            self._command("ignore-shutdown"),
            request_timeout=0.1,
            shutdown_timeout=0.1,
        )
        client.start()
        started = time.monotonic()
        action = client.stop()
        self.assertIn(action, {"terminated", "killed"})
        self.assertLess(time.monotonic() - started, 2.0)
        self.assertFalse(client.is_running())


class DevelopmentSelectionTests(unittest.TestCase):
    def test_development_flag_is_explicit_and_not_persisted(self):
        self.assertFalse(development_engine_enabled({}))
        self.assertTrue(development_engine_enabled({"HYPOMUX_GO_ENGINE_DEV": "true"}))
        self.assertTrue(development_engine_enabled({"HYPOMUX_GO_PROXY_DEV": "1"}))
        self.assertTrue(development_engine_enabled({"HYPOMUX_GO_TUN_DEV": "on"}))
        self.assertFalse(development_engine_enabled({"HYPOMUX_GO_ENGINE_DEV": "0"}))
        self.assertFalse(go_proxy_development_enabled({}))
        self.assertTrue(go_proxy_development_enabled({"HYPOMUX_GO_PROXY_DEV": "yes"}))
        self.assertFalse(go_tun_development_enabled({}))
        self.assertTrue(go_tun_development_enabled({"HYPOMUX_GO_TUN_DEV": "yes"}))

    def test_configured_engine_path_must_exist(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            engine = root / "hypomux-engine.exe"
            engine.write_bytes(b"test")
            command = resolve_development_engine_command(
                root,
                {"HYPOMUX_ENGINE_PATH": str(engine)},
            )
            self.assertEqual(command, [str(engine.resolve())])
            self.assertIsNone(
                resolve_development_engine_command(
                    root,
                    {"HYPOMUX_ENGINE_PATH": str(root / "missing.exe")},
                )
            )


@unittest.skipUnless(
    os.environ.get("HYPOMUX_ENGINE_TEST_EXE"),
    "set HYPOMUX_ENGINE_TEST_EXE to run the real Go host integration test",
)
class RealGoEngineIntegrationTests(unittest.TestCase):
    def test_real_child_process_handshake_status_and_shutdown(self):
        executable = os.environ["HYPOMUX_ENGINE_TEST_EXE"]
        events: list[dict] = []
        client = EngineClient(executable, on_event=events.append)
        try:
            hello = client.start()
            self.assertEqual(hello["protocol_version"], 1)
            self.assertIn("engine.status", hello["capabilities"])
            self.assertIn("diagnostic.run", hello["capabilities"])
            self.assertIn("engine.start", hello["capabilities"])
            self.assertTrue(client.request("health.check")["ok"])
            diagnostic = client.request(
                "diagnostic.run",
                {"src_ip": "invalid", "target_ip": "223.5.5.5"},
            )
            self.assertEqual(diagnostic["status"], "unavailable")
            self.assertEqual(diagnostic["note"], "invalid --src-ip")
            self.assertEqual(
                client.request("engine.status")["engine"]["state"],
                "stopped",
            )
            self.assertEqual(client.stop(), "exited")
            self.assertFalse(client.is_running())
            self.assertEqual(events[-1]["event"], "host.exiting")
        finally:
            client.stop(graceful=False)

    def test_real_host_exits_when_parent_pipe_closes(self):
        executable = os.environ["HYPOMUX_ENGINE_TEST_EXE"]
        process = start_engine_process(executable)
        self.assertIsNotNone(process.stdin)
        process.stdin.close()
        self.assertEqual(process.wait(timeout=2.0), 0)

    def test_real_go_proxy_relays_socks_and_reports_telemetry(self):
        executable = os.environ["HYPOMUX_ENGINE_TEST_EXE"]
        echo_listener = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        echo_listener.bind(("127.0.0.1", 0))
        echo_listener.listen()
        echo_listener.settimeout(3.0)
        echo_address = echo_listener.getsockname()
        echo_error: list[Exception] = []

        def echo_once():
            try:
                connection, _ = echo_listener.accept()
                with connection:
                    while True:
                        data = connection.recv(65536)
                        if not data:
                            return
                        connection.sendall(data)
            except Exception as exc:
                echo_error.append(exc)

        echo_thread = threading.Thread(target=echo_once, daemon=True)
        echo_thread.start()
        events: list[dict] = []
        client = EngineClient(executable, on_event=events.append)
        proxy_socket = None
        try:
            client.start()
            started = client.request(
                "engine.start",
                {
                    "mode": "proxy",
                    "socks_port": 0,
                    "http_port": 0,
                    "adapters": [
                        {"name": "loopback", "source_ip": "127.0.0.1"}
                    ],
                },
                timeout=5.0,
            )
            socks_host, socks_port = started["endpoints"]["socks"].rsplit(":", 1)
            proxy_socket = socket.create_connection(
                (socks_host, int(socks_port)), timeout=2.0
            )
            proxy_socket.settimeout(2.0)
            proxy_socket.sendall(b"\x05\x01\x00")
            self.assertEqual(_recv_exact(proxy_socket, 2), b"\x05\x00")
            request = (
                b"\x05\x01\x00\x01"
                + socket.inet_aton(echo_address[0])
                + struct.pack("!H", echo_address[1])
            )
            proxy_socket.sendall(request)
            self.assertEqual(_recv_exact(proxy_socket, 10)[1], 0)
            proxy_socket.sendall(b"go-proxy-integration")
            self.assertEqual(
                _recv_exact(proxy_socket, len(b"go-proxy-integration")),
                b"go-proxy-integration",
            )
            telemetry = client.request("engine.telemetry")
            self.assertGreaterEqual(telemetry["total"]["connections"], 1)
            self.assertGreaterEqual(
                telemetry["total"]["bytes_up"], len(b"go-proxy-integration")
            )
            proxy_socket.close()
            proxy_socket = None
            stopped = client.request("engine.stop", timeout=5.0)
            self.assertTrue(stopped["accepted"])
            self.assertTrue(
                any(event.get("event") == "engine.state_changed" for event in events)
            )
        finally:
            if proxy_socket is not None:
                proxy_socket.close()
            client.stop(graceful=True)
            echo_listener.close()
            echo_thread.join(timeout=2.0)
        self.assertFalse(echo_error)

    @unittest.skipUnless(
        os.environ.get("HYPOMUX_DNS_TEST_SOURCE_IP")
        and os.environ.get("HYPOMUX_DNS_TEST_IF_INDEX")
        and os.environ.get("HYPOMUX_DNS_TEST_SERVER"),
        "set physical adapter DNS test environment variables",
    )
    def test_real_go_engine_resolves_dns_on_physical_adapter(self):
        executable = os.environ["HYPOMUX_ENGINE_TEST_EXE"]
        source_ip = os.environ["HYPOMUX_DNS_TEST_SOURCE_IP"]
        interface_index = int(os.environ["HYPOMUX_DNS_TEST_IF_INDEX"])
        dns_server = os.environ["HYPOMUX_DNS_TEST_SERVER"]
        client = EngineClient(executable)
        started = False
        try:
            client.start()
            client.request(
                "engine.start",
                {
                    "mode": "proxy",
                    "socks_port": 0,
                    "http_port": 0,
                    "dns": {
                        "policy": "off",
                        "legacy_servers": [dns_server],
                        "cache_ttl_ms": 180_000,
                        "query_timeout_ms": 4_000,
                    },
                    "adapters": [
                        {
                            "name": "physical-dns-test",
                            "source_ip": source_ip,
                            "if_index": interface_index,
                            "dns_servers": [dns_server],
                        }
                    ],
                },
                timeout=5.0,
            )
            started = True
            first = client.request(
                "dns.resolve",
                {
                    "domain": "www.msftconnecttest.com",
                    "adapter": "physical-dns-test",
                    "record_type": "A",
                },
                timeout=8.0,
            )
            self.assertEqual(first["adapter"], "physical-dns-test")
            self.assertIn(first["transport"], {"udp", "tcp"})
            self.assertFalse(first["cached"])
            socket.inet_aton(first["address"])

            second = client.request(
                "dns.resolve",
                {
                    "domain": "www.msftconnecttest.com",
                    "adapter": "physical-dns-test",
                    "record_type": "A",
                },
                timeout=8.0,
            )
            self.assertTrue(second["cached"])
            status = client.request("dns.status")
            self.assertGreaterEqual(status["cache_hits"], 1)
            self.assertGreaterEqual(status["legacy_successes"], 1)
        finally:
            if started and client.is_running():
                try:
                    client.request("engine.stop", timeout=6.0)
                except EngineClientError:
                    pass
            client.stop(graceful=True)

    @unittest.skipUnless(
        os.environ.get("HYPOMUX_DOH_TEST") == "1"
        and os.environ.get("HYPOMUX_DNS_TEST_SOURCE_IP")
        and os.environ.get("HYPOMUX_DNS_TEST_IF_INDEX"),
        "set physical adapter DoH test environment variables",
    )
    def test_real_go_engine_resolves_doh_on_physical_adapter(self):
        executable = os.environ["HYPOMUX_ENGINE_TEST_EXE"]
        source_ip = os.environ["HYPOMUX_DNS_TEST_SOURCE_IP"]
        interface_index = int(os.environ["HYPOMUX_DNS_TEST_IF_INDEX"])
        client = EngineClient(executable)
        started = False
        try:
            client.start()
            client.request(
                "engine.start",
                {
                    "mode": "proxy",
                    "socks_port": 0,
                    "http_port": 0,
                    "dns": {
                        "policy": "alidns",
                        "cache_ttl_ms": 180_000,
                        "query_timeout_ms": 8_000,
                    },
                    "adapters": [
                        {
                            "name": "physical-doh-test",
                            "source_ip": source_ip,
                            "if_index": interface_index,
                        }
                    ],
                },
                timeout=5.0,
            )
            started = True
            result = client.request(
                "dns.resolve",
                {
                    "domain": "www.msftconnecttest.com",
                    "adapter": "physical-doh-test",
                    "record_type": "A",
                },
                timeout=12.0,
            )
            self.assertEqual(result["transport"], "doh")
            self.assertIn("dns.alidns.com@", result["server"])
            socket.inet_aton(result["address"])
            status = client.request("dns.status")
            self.assertGreaterEqual(status["doh_successes"], 1)
            self.assertEqual(status["automatic_fallbacks"], 0)
        finally:
            if started and client.is_running():
                try:
                    client.request("engine.stop", timeout=6.0)
                except EngineClientError:
                    pass
            client.stop(graceful=True)
