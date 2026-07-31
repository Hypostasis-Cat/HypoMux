from __future__ import annotations

import json
import os
from pathlib import Path
import socket
import struct
import subprocess
import sys
import time
import re
from typing import Any

from engine_client.client import EngineClient


ENGINE = os.environ.get(
    "HYPOMUX_TEST_ENGINE",
    r"C:\Program Files\HypoMux\bin\hypomux-engine.exe",
)
ADAPTER = {
    "name": "Ethernet",
    "source_ip": "192.168.1.146",
    "if_index": 11,
    "source_ipv6": "2408:8221:2b10:7e0:f6c6:acd8:217a:9f49",
    "ipv6_if_index": 11,
    "dns_servers": ["119.29.29.29", "8.8.8.8"],
}
WIFI_ADAPTER = {
    "name": "Wi-Fi",
    "source_ip": "192.168.1.46",
    "if_index": 14,
    "source_ipv6": "2408:8221:2b10:7e0:e6d8:c72c:c6a4:a089",
    "ipv6_if_index": 14,
    "dns_servers": ["192.168.1.1"],
}
TARGET_HOST = "www.msftconnecttest.com"
TARGET_PORT = 80
TARGET_PATH = "/connecttest.txt"
EXPECTED_BODY = b"Microsoft Connect Test"
OUTPUT_ROOT = Path(
    os.environ.get(
        "HYPOMUX_QUALIFICATION_DIR",
        "build/qualification/phase14-dev-9a591c7",
    )
)


def recv_exact(stream: socket.socket, length: int) -> bytes:
    chunks = []
    remaining = length
    while remaining:
        chunk = stream.recv(remaining)
        if not chunk:
            raise RuntimeError("proxy closed before the response completed")
        chunks.append(chunk)
        remaining -= len(chunk)
    return b"".join(chunks)


def recv_all(stream: socket.socket) -> bytes:
    chunks = []
    while True:
        chunk = stream.recv(65536)
        if not chunk:
            return b"".join(chunks)
        chunks.append(chunk)


def http_request(
    stream: socket.socket,
    *,
    host: str = TARGET_HOST,
    path: str = TARGET_PATH,
    expected_body: bytes | None = EXPECTED_BODY,
) -> bytes:
    request = (
        f"GET {path} HTTP/1.1\r\n"
        f"Host: {host}\r\n"
        "Connection: close\r\n\r\n"
    ).encode("ascii")
    stream.sendall(request)
    response = recv_all(stream)
    status_line = response.split(b"\r\n", 1)[0]
    if not status_line.startswith(b"HTTP/"):
        raise RuntimeError(f"unexpected HTTP response: {response[:120]!r}")
    if expected_body is not None and expected_body not in response:
        raise RuntimeError("connectivity response body did not match")
    return response


def socks_probe(endpoint: str) -> int:
    host, port = endpoint.rsplit(":", 1)
    with socket.create_connection((host, int(port)), timeout=8.0) as stream:
        stream.settimeout(12.0)
        stream.sendall(b"\x05\x01\x00")
        if recv_exact(stream, 2) != b"\x05\x00":
            raise RuntimeError("SOCKS5 authentication negotiation failed")
        encoded = TARGET_HOST.encode("idna")
        stream.sendall(
            b"\x05\x01\x00\x03"
            + bytes([len(encoded)])
            + encoded
            + struct.pack("!H", TARGET_PORT)
        )
        header = recv_exact(stream, 4)
        if header[1] != 0:
            raise RuntimeError(f"SOCKS5 connect failed with code {header[1]}")
        address_type = header[3]
        if address_type == 1:
            recv_exact(stream, 4)
        elif address_type == 3:
            recv_exact(stream, recv_exact(stream, 1)[0])
        elif address_type == 4:
            recv_exact(stream, 16)
        else:
            raise RuntimeError("SOCKS5 returned an invalid address type")
        recv_exact(stream, 2)
        return len(http_request(stream))


def socks_ipv6_probe(endpoint: str, target_ipv6: str) -> int:
    host, port = endpoint.rsplit(":", 1)
    with socket.create_connection((host, int(port)), timeout=8.0) as stream:
        stream.settimeout(12.0)
        stream.sendall(b"\x05\x01\x00")
        if recv_exact(stream, 2) != b"\x05\x00":
            raise RuntimeError("SOCKS5 IPv6 authentication negotiation failed")
        stream.sendall(
            b"\x05\x01\x00\x04"
            + socket.inet_pton(socket.AF_INET6, target_ipv6)
            + struct.pack("!H", 80)
        )
        header = recv_exact(stream, 4)
        if header[1] != 0:
            raise RuntimeError(
                f"SOCKS5 IPv6 connect failed with code {header[1]}"
            )
        address_type = header[3]
        if address_type == 1:
            recv_exact(stream, 4)
        elif address_type == 3:
            recv_exact(stream, recv_exact(stream, 1)[0])
        elif address_type == 4:
            recv_exact(stream, 16)
        else:
            raise RuntimeError("SOCKS5 returned an invalid IPv6 bind type")
        recv_exact(stream, 2)
        return len(
            http_request(
                stream,
                host="www.cloudflare.com",
                path="/",
                expected_body=None,
            )
        )


def socks_ipv4_probe(endpoint: str, target_ipv4: str) -> int:
    host, port = endpoint.rsplit(":", 1)
    with socket.create_connection((host, int(port)), timeout=8.0) as stream:
        stream.settimeout(12.0)
        stream.sendall(b"\x05\x01\x00")
        if recv_exact(stream, 2) != b"\x05\x00":
            raise RuntimeError("SOCKS5 IPv4 authentication negotiation failed")
        stream.sendall(
            b"\x05\x01\x00\x01"
            + socket.inet_pton(socket.AF_INET, target_ipv4)
            + struct.pack("!H", TARGET_PORT)
        )
        header = recv_exact(stream, 4)
        if header[1] != 0:
            raise RuntimeError(
                f"SOCKS5 IPv4 connect failed with code {header[1]}"
            )
        address_type = header[3]
        if address_type == 1:
            recv_exact(stream, 4)
        elif address_type == 3:
            recv_exact(stream, recv_exact(stream, 1)[0])
        elif address_type == 4:
            recv_exact(stream, 16)
        else:
            raise RuntimeError("SOCKS5 returned an invalid IPv4 bind type")
        recv_exact(stream, 2)
        return len(http_request(stream))


def read_headers(stream: socket.socket) -> bytes:
    data = bytearray()
    while b"\r\n\r\n" not in data:
        chunk = stream.recv(4096)
        if not chunk:
            raise RuntimeError("HTTP proxy closed before CONNECT completed")
        data.extend(chunk)
        if len(data) > 65536:
            raise RuntimeError("HTTP CONNECT response headers are too large")
    return bytes(data)


def http_connect_probe(endpoint: str) -> int:
    host, port = endpoint.rsplit(":", 1)
    with socket.create_connection((host, int(port)), timeout=8.0) as stream:
        stream.settimeout(12.0)
        stream.sendall(
            (
                f"CONNECT {TARGET_HOST}:{TARGET_PORT} HTTP/1.1\r\n"
                f"Host: {TARGET_HOST}:{TARGET_PORT}\r\n\r\n"
            ).encode("ascii")
        )
        response = read_headers(stream)
        if b" 200 " not in response.split(b"\r\n", 1)[0]:
            raise RuntimeError(
                f"HTTP CONNECT failed: {response[:120]!r}"
            )
        return len(http_request(stream))


def proxy_probe() -> dict[str, Any]:
    client = EngineClient(ENGINE)
    started = False
    try:
        client.start()
        result = client.request(
            "engine.start",
            {
                "mode": "proxy",
                "socks_port": 0,
                "http_port": 0,
                "dns": {"policy": "auto"},
                "adapters": [ADAPTER],
            },
            timeout=8.0,
        )
        started = True
        socks_bytes = socks_probe(result["endpoints"]["socks"])
        http_bytes = http_connect_probe(result["endpoints"]["http"])
        resolved_ipv6 = client.request(
            "dns.resolve",
            {
                "domain": "www.cloudflare.com",
                "adapter": ADAPTER["name"],
                "record_type": "AAAA",
            },
            timeout=15.0,
        )
        ipv6_bytes = socks_ipv6_probe(
            result["endpoints"]["socks"],
            resolved_ipv6["address"],
        )
        telemetry = client.request("engine.telemetry", timeout=5.0)
        adapter = next(
            item
            for item in telemetry["adapters"]
            if item["name"] == ADAPTER["name"]
        )
        if int(adapter.get("bytes_up", 0)) <= 0:
            raise RuntimeError("adapter upload telemetry did not advance")
        if int(adapter.get("bytes_down", 0)) <= 0:
            raise RuntimeError("adapter download telemetry did not advance")
        return {
            "passed": True,
            "target": f"{TARGET_HOST}:{TARGET_PORT}",
            "socks_response_bytes": socks_bytes,
            "http_connect_response_bytes": http_bytes,
            "ipv6_response_bytes": ipv6_bytes,
            "ipv6_dns_transport": resolved_ipv6["transport"],
            "adapter_telemetry": adapter,
        }
    finally:
        if started and client.is_running():
            client.request("engine.stop", timeout=8.0)
        client.stop(graceful=True)


def dns_probe() -> dict[str, Any]:
    results = []
    failures = []
    for policy in ("off", "alidns", "dnspod", "google"):
        client = EngineClient(ENGINE)
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
                        "policy": policy,
                        "legacy_servers": ["119.29.29.29", "8.8.8.8"],
                        "query_timeout_ms": 10_000,
                    },
                    "adapters": [ADAPTER],
                },
                timeout=8.0,
            )
            started = True
            for record_type in ("A", "AAAA"):
                try:
                    resolved = client.request(
                        "dns.resolve",
                        {
                            "domain": "www.cloudflare.com",
                            "adapter": ADAPTER["name"],
                            "record_type": record_type,
                        },
                        timeout=15.0,
                    )
                    if record_type == "A":
                        socket.inet_pton(socket.AF_INET, resolved["address"])
                    else:
                        socket.inet_pton(
                            socket.AF_INET6,
                            resolved["address"],
                        )
                    results.append(
                        {
                            "policy": policy,
                            "record_type": record_type,
                            "transport": resolved["transport"],
                            "server": resolved["server"],
                            "address_family": record_type,
                        }
                    )
                except Exception as exc:
                    failures.append(
                        {
                            "policy": policy,
                            "record_type": record_type,
                            "error": f"{type(exc).__name__}: {exc}",
                        }
                    )
        finally:
            if started and client.is_running():
                client.request("engine.stop", timeout=8.0)
            client.stop(graceful=True)
    return {
        "passed": not failures,
        "queries": results,
        "failures": failures,
    }


def distribution_probe(
    *,
    weighted: bool,
    ethernet_weight: int,
    wifi_weight: int,
    requests: int,
) -> dict[str, Any]:
    ethernet = dict(ADAPTER, weight=ethernet_weight)
    wifi = dict(WIFI_ADAPTER, weight=wifi_weight)
    client = EngineClient(ENGINE)
    started = False
    try:
        client.start()
        result = client.request(
            "engine.start",
            {
                "mode": "proxy",
                "socks_port": 0,
                "http_port": 0,
                "weighted": weighted,
                "dns": {"policy": "off"},
                "adapters": [ethernet, wifi],
            },
            timeout=8.0,
        )
        started = True
        for _index in range(requests):
            socks_probe(result["endpoints"]["socks"])
        telemetry = client.request("engine.telemetry", timeout=5.0)
        adapters = {
            item["name"]: item
            for item in telemetry["adapters"]
        }
        successes = {
            name: int(item.get("health_successes", 0))
            for name, item in adapters.items()
        }
        return {
            "weighted": weighted,
            "weights": {
                "Ethernet": ethernet_weight,
                "Wi-Fi": wifi_weight,
            },
            "requests": requests,
            "health_successes": successes,
            "adapter_telemetry": adapters,
        }
    finally:
        if started and client.is_running():
            client.request("engine.stop", timeout=8.0)
        client.stop(graceful=True)


def scheduling_probe() -> dict[str, Any]:
    round_robin = distribution_probe(
        weighted=False,
        ethernet_weight=1,
        wifi_weight=1,
        requests=6,
    )
    weighted = distribution_probe(
        weighted=True,
        ethernet_weight=3,
        wifi_weight=1,
        requests=12,
    )
    rr = round_robin["health_successes"]
    wr = weighted["health_successes"]
    if rr != {"Ethernet": 3, "Wi-Fi": 3}:
        raise RuntimeError(f"round-robin distribution was {rr!r}")
    if wr != {"Ethernet": 9, "Wi-Fi": 3}:
        raise RuntimeError(f"weighted 3:1 distribution was {wr!r}")
    return {
        "passed": True,
        "round_robin": round_robin,
        "weighted_3_to_1": weighted,
    }


def run_netsh(arguments: list[str]) -> str:
    completed = subprocess.run(
        ["netsh", *arguments],
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
        timeout=20.0,
    )
    if completed.returncode != 0:
        detail = (completed.stderr or completed.stdout).strip()
        raise RuntimeError(f"netsh failed: {detail}")
    return completed.stdout


def run_powershell(script: str) -> str:
    completed = subprocess.run(
        [
            "powershell",
            "-NoProfile",
            "-NonInteractive",
            "-Command",
            "$ErrorActionPreference = 'Stop'; " + script,
        ],
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
        timeout=20.0,
    )
    if completed.returncode != 0:
        detail = (completed.stderr or completed.stdout).strip()
        raise RuntimeError(f"PowerShell adapter command failed: {detail}")
    return completed.stdout


def current_wifi_profile() -> str:
    configured = os.environ.get("HYPOMUX_TEST_WIFI_PROFILE", "").strip()
    if configured:
        return configured
    output = run_netsh(["wlan", "show", "interfaces"])
    match = re.search(r"^\s*Profile\s*:\s*(.+?)\s*$", output, re.MULTILINE)
    if match:
        return match.group(1)
    profiles = run_netsh(["wlan", "show", "profiles"])
    matches = re.findall(
        r"^\s*All User Profile\s*:\s*(.+?)\s*$",
        profiles,
        re.MULTILINE,
    )
    if len(matches) == 1:
        return matches[0]
    raise RuntimeError("could not determine the active WLAN profile")


def source_address_bindable(address: str) -> bool:
    stream = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    try:
        stream.bind((address, 0))
        return True
    except OSError:
        return False
    finally:
        stream.close()


def wait_for_source_address(address: str, *, present: bool) -> None:
    for _attempt in range(30):
        if source_address_bindable(address) is present:
            return
        time.sleep(0.5)
    state = "available" if present else "removed"
    raise RuntimeError(f"Wi-Fi source address was not {state} in time")


def set_wifi_enabled(enabled: bool, profile: str | None = None) -> None:
    command = "Enable-NetAdapter" if enabled else "Disable-NetAdapter"
    run_powershell(
        f"{command} -Name 'WLAN' -Confirm:$false | Out-Null"
    )
    if enabled and profile:
        last_error = None
        for _attempt in range(30):
            try:
                run_netsh(
                    [
                        "wlan",
                        "connect",
                        f"name={profile}",
                        "interface=WLAN",
                    ]
                )
                return
            except RuntimeError as exc:
                last_error = exc
                time.sleep(0.5)
        raise RuntimeError(
            f"WLAN did not become ready for reconnect: {last_error}"
        )


def churn_probe() -> dict[str, Any]:
    client = EngineClient(ENGINE)
    started = False
    wifi_disabled = False
    wifi_profile = current_wifi_profile()
    try:
        if not source_address_bindable(WIFI_ADAPTER["source_ip"]):
            set_wifi_enabled(True, wifi_profile)
            wait_for_source_address(
                WIFI_ADAPTER["source_ip"],
                present=True,
            )
        client.start()
        result = client.request(
            "engine.start",
            {
                "mode": "proxy",
                "socks_port": 0,
                "http_port": 0,
                "weighted": False,
                "dns": {"policy": "off"},
                "adapters": [ADAPTER, WIFI_ADAPTER],
            },
            timeout=8.0,
        )
        started = True
        endpoint = result["endpoints"]["socks"]
        socks_probe(endpoint)
        socks_probe(endpoint)
        baseline = client.request("engine.telemetry", timeout=5.0)
        resolved = client.request(
            "dns.resolve",
            {
                "domain": TARGET_HOST,
                "adapter": ADAPTER["name"],
                "record_type": "A",
            },
            timeout=15.0,
        )
        target_ipv4 = resolved["address"]

        set_wifi_enabled(False)
        wifi_disabled = True
        wait_for_source_address(WIFI_ADAPTER["source_ip"], present=False)
        disabled_diagnostic = client.request(
            "diagnostic.run",
            {
                "src_ip": WIFI_ADAPTER["source_ip"],
                "target_ip": "223.5.5.5",
                "count": 1,
                "timeout_ms": 500,
            },
            timeout=5.0,
        )
        socks_ipv4_probe(endpoint, target_ipv4)
        socks_ipv4_probe(endpoint, target_ipv4)
        failed_over = client.request("engine.telemetry", timeout=5.0)
        failed_adapters = {
            item["name"]: item
            for item in failed_over["adapters"]
        }
        wifi_failed = failed_adapters["Wi-Fi"]
        if int(wifi_failed.get("health_failures", 0)) < 1:
            raise RuntimeError(
                "disabled Wi-Fi did not record a failure: "
                + json.dumps(wifi_failed, ensure_ascii=False)
                + "; diagnostic="
                + json.dumps(disabled_diagnostic, ensure_ascii=False)
            )
        if wifi_failed.get("health_state") != "cooldown":
            raise RuntimeError(
                "disabled Wi-Fi did not enter health cooldown"
            )

        set_wifi_enabled(True, wifi_profile)
        wifi_disabled = False
        wait_for_source_address(WIFI_ADAPTER["source_ip"], present=True)
        recovered = None
        for _attempt in range(12):
            time.sleep(1.0)
            try:
                socks_ipv4_probe(endpoint, target_ipv4)
            except Exception:
                continue
            snapshot = client.request("engine.telemetry", timeout=5.0)
            current = {
                item["name"]: item
                for item in snapshot["adapters"]
            }
            wifi = current["Wi-Fi"]
            if (
                int(wifi.get("health_successes", 0)) >= 2
                and wifi.get("health_state") == "healthy"
            ):
                recovered = snapshot
                break
        if recovered is None:
            raise RuntimeError("re-enabled Wi-Fi did not recover")
        return {
            "passed": True,
            "baseline": baseline["adapters"],
            "failed_over": failed_over["adapters"],
            "recovered": recovered["adapters"],
        }
    finally:
        if wifi_disabled or not source_address_bindable(
            WIFI_ADAPTER["source_ip"]
        ):
            set_wifi_enabled(True, wifi_profile)
            wait_for_source_address(
                WIFI_ADAPTER["source_ip"],
                present=True,
            )
        if started and client.is_running():
            client.request("engine.stop", timeout=8.0)
        client.stop(graceful=True)


def main() -> int:
    if len(sys.argv) > 1 and sys.argv[1] in {"scheduling", "churn"}:
        command = sys.argv[1]
        output = OUTPUT_ROOT / f"{command}-probe.json"
        report: dict[str, Any] = {}
        try:
            report[command] = (
                scheduling_probe()
                if command == "scheduling"
                else churn_probe()
            )
            report["passed"] = True
        except Exception as exc:
            report["passed"] = False
            report["error"] = f"{type(exc).__name__}: {exc}"
        output.write_text(
            json.dumps(report, ensure_ascii=False, indent=2) + "\n",
            encoding="utf-8",
        )
        print(json.dumps(report, ensure_ascii=False, indent=2))
        return 0 if report["passed"] else 1

    output = OUTPUT_ROOT / "network-probe.json"
    report: dict[str, Any] = {}
    try:
        report["proxy"] = proxy_probe()
        report["dns"] = dns_probe()
        report["passed"] = bool(
            report["proxy"]["passed"] and report["dns"]["passed"]
        )
    except Exception as exc:
        report["passed"] = False
        report["error"] = f"{type(exc).__name__}: {exc}"
    output.write_text(
        json.dumps(report, ensure_ascii=False, indent=2) + "\n",
        encoding="utf-8",
    )
    print(json.dumps(report, ensure_ascii=False, indent=2))
    return 0 if report["passed"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
