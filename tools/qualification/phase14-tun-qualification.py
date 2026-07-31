from __future__ import annotations

import argparse
import base64
import ctypes
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import socket
import subprocess
import sys
import time
from typing import Any

REPO_ROOT = Path(__file__).resolve().parents[1]
if str(REPO_ROOT) not in sys.path:
    sys.path.insert(0, str(REPO_ROOT))

from engine_client.client import EngineClient
from utils.singbox_config import build_config
from utils.wfp_dns_exemption import probe_wfp_engine


ENGINE = Path(r"C:\Program Files\HypoMux\bin\hypomux-engine.exe")
SING_BOX = Path(r"C:\Program Files\HypoMux\bin\sing-box.exe")
OUTPUT_ROOT = Path("build/qualification/phase14-dev-9a591c7")
EXPECTED_ENGINE_SHA256 = (
    "3b72ce18b3110d49f1d260a967a07f252ff282e16a12a2f2d2c68b453b5b06a1"
)
FOREIGN_TUN_PATTERN = (
    "meta|clash|mihomo|wireguard|tailscale|vpn|tap|wintun"
)
ADAPTERS = [
    {
        "name": "Ethernet",
        "source_ip": "192.168.1.146",
        "source_ipv6": "2408:8221:2b10:7e0:f6c6:acd8:217a:9f49",
        "if_index": 11,
        "ipv6_if_index": 11,
        "weight": 1,
        "dns_servers": ["119.29.29.29", "8.8.8.8"],
    },
    {
        "name": "Wi-Fi",
        "source_ip": "192.168.1.46",
        "source_ipv6": "2408:8221:2b10:7e0:e6d8:c72c:c6a4:a089",
        "if_index": 14,
        "ipv6_if_index": 14,
        "weight": 1,
        "dns_servers": ["192.168.1.1"],
    },
]


def utc_now() -> str:
    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(block)
    return digest.hexdigest()


def is_elevated() -> bool:
    try:
        return bool(ctypes.windll.shell32.IsUserAnAdmin())
    except Exception:
        return False


def powershell(script: str, *, timeout: float = 20.0) -> str:
    # Windows PowerShell inherits the host console code page when its output is
    # redirected.  That corrupts localized interface aliases before Python can
    # decode the JSON (for example, "以太网" became "锟斤拷太锟斤拷").  Force a
    # BOM-less UTF-8 stream so the subprocess contract matches encoding below.
    wrapped = (
        "$OutputEncoding = [Console]::OutputEncoding = "
        "[System.Text.UTF8Encoding]::new($false);"
        + script
    )
    encoded = base64.b64encode(wrapped.encode("utf-16-le")).decode("ascii")
    completed = subprocess.run(
        [
            "powershell.exe",
            "-NoProfile",
            "-NonInteractive",
            "-EncodedCommand",
            encoded,
        ],
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
        timeout=timeout,
        creationflags=getattr(subprocess, "CREATE_NO_WINDOW", 0),
    )
    if completed.returncode != 0:
        detail = (completed.stderr or completed.stdout).strip()
        raise RuntimeError(f"PowerShell failed: {detail}")
    return completed.stdout.strip()


def powershell_json(script: str, *, timeout: float = 20.0) -> list[dict[str, Any]]:
    output = powershell(
        "$ErrorActionPreference='Stop';"
        f"$value=@({script});"
        "$value | ConvertTo-Json -Compress -Depth 6",
        timeout=timeout,
    )
    if not output:
        return []
    value = json.loads(output)
    if value is None:
        return []
    if isinstance(value, list):
        return [item for item in value if isinstance(item, dict)]
    return [value] if isinstance(value, dict) else []


def default_routes() -> list[dict[str, Any]]:
    return powershell_json(
        "Get-NetRoute -AddressFamily IPv4 "
        "-DestinationPrefix '0.0.0.0/0' -ErrorAction SilentlyContinue | "
        "Select-Object InterfaceAlias,ifIndex,NextHop,RouteMetric,InterfaceMetric"
    )


def foreign_default_routes() -> list[dict[str, Any]]:
    result = []
    for route in default_routes():
        alias = str(route.get("InterfaceAlias") or "")
        lowered = alias.lower()
        if alias == "HypoMux-Tun":
            continue
        if any(
            token in lowered
            for token in (
                "meta",
                "clash",
                "mihomo",
                "wireguard",
                "tailscale",
                "vpn",
                "tap",
                "wintun",
            )
        ):
            result.append(route)
    return result


def hypomux_residue() -> dict[str, list[dict[str, Any]]]:
    routes = powershell_json(
        "Get-NetRoute -ErrorAction SilentlyContinue | "
        "Where-Object { $_.InterfaceAlias -eq 'HypoMux-Tun' -and "
        "($_.DestinationPrefix -eq '0.0.0.0/0' -or "
        "$_.DestinationPrefix -eq '::/0') } | "
        "Select-Object InterfaceAlias,ifIndex,DestinationPrefix,NextHop,RouteMetric"
    )
    devices = powershell_json(
        "Get-PnpDevice -Class Net -ErrorAction SilentlyContinue | "
        "Where-Object { $_.FriendlyName -eq 'HypoMux-Tun' -and "
        "$_.InstanceId -like '*WINTUN*' } | "
        "Select-Object FriendlyName,InstanceId,Status"
    )
    return {"routes": routes, "devices": devices}


def address_bindable(address: str) -> bool:
    family = socket.AF_INET6 if ":" in address else socket.AF_INET
    stream = socket.socket(family, socket.SOCK_STREAM)
    try:
        stream.bind((address, 0))
        return True
    except OSError:
        return False
    finally:
        stream.close()


def interface_alias_for_ip(address: str) -> str:
    """Resolve the localized Windows interface alias for one source address."""
    rows = powershell_json(
        "Get-NetIPAddress -AddressFamily IPv4 -ErrorAction SilentlyContinue | "
        f"Where-Object {{ $_.IPAddress -eq '{address}' }} | "
        "ForEach-Object { "
        "$adapter = Get-NetAdapter -InterfaceIndex $_.ifIndex "
        "-ErrorAction SilentlyContinue; "
        "[pscustomobject]@{alias=$adapter.Name; ifIndex=$_.ifIndex} "
        "}"
    )
    if not rows:
        raise RuntimeError(f"could not resolve interface alias for {address}")
    alias = str(rows[0].get("alias") or "").strip()
    if not alias:
        raise RuntimeError(f"interface alias is empty for {address}")
    if "\ufffd" in alias or "锟" in alias:
        raise RuntimeError(
            f"interface alias was corrupted while decoding PowerShell output: "
            f"{alias!r}"
        )
    return alias


def engine_identity() -> dict[str, Any]:
    if not ENGINE.is_file():
        return {"exists": False, "path": str(ENGINE)}
    digest = sha256_file(ENGINE)
    client = EngineClient(str(ENGINE), request_timeout=5.0)
    try:
        hello = client.start()
    finally:
        client.stop(graceful=True)
    return {
        "exists": True,
        "path": str(ENGINE),
        "sha256": digest,
        "sha256_matches": digest == EXPECTED_ENGINE_SHA256,
        "hello": hello,
    }


def preflight() -> dict[str, Any]:
    wfp_ready, wfp_detail = probe_wfp_engine()
    foreign = foreign_default_routes()
    residue = hypomux_residue()
    adapter_state = [
        {
            "name": adapter["name"],
            "source_ip": adapter["source_ip"],
            "bindable": address_bindable(str(adapter["source_ip"])),
        }
        for adapter in ADAPTERS
    ]
    identity = engine_identity()
    checks = {
        "elevated": is_elevated(),
        "engine_exact_candidate": bool(
            identity.get("sha256_matches")
            and identity.get("hello", {}).get("commit")
            == "9a591c72a77777914d10ca4dee4350d12ccf8b29"
        ),
        "sing_box_present": SING_BOX.is_file(),
        "wfp_available": wfp_ready,
        "no_foreign_tun_default_route": not foreign,
        "no_hypomux_residue": not residue["routes"] and not residue["devices"],
        "physical_sources_bindable": all(
            item["bindable"] for item in adapter_state
        ),
    }
    return {
        "kind": "hypomux.phase14.tun_preflight",
        "captured_at": utc_now(),
        "ready_to_run": all(checks.values()),
        "checks": checks,
        "engine": identity,
        "sing_box": {
            "path": str(SING_BOX),
            "exists": SING_BOX.is_file(),
        },
        "wfp": {"ready": wfp_ready, "detail": wfp_detail},
        "foreign_default_routes": foreign,
        "hypomux_residue": residue,
        "adapters": adapter_state,
    }


def channel_ports(result: dict[str, Any]) -> dict[str, int]:
    endpoints = result.get("endpoints", {}).get("channels", {})
    ports = {}
    for name in ("nic_ethernet", "nic_wifi", "aggregation"):
        endpoint = str(endpoints.get(name) or "")
        host, port_text = endpoint.rsplit(":", 1)
        if host != "127.0.0.1":
            raise RuntimeError(f"{name} did not bind to loopback: {endpoint}")
        ports[name] = int(port_text)
    return ports


def telemetry_totals(snapshot: dict[str, Any]) -> dict[str, int]:
    adapters = snapshot.get("adapters", [])
    return {
        "bytes_up": sum(int(item.get("bytes_up", 0)) for item in adapters),
        "bytes_down": sum(int(item.get("bytes_down", 0)) for item in adapters),
    }


def delta(after: dict[str, int], before: dict[str, int]) -> dict[str, int]:
    return {
        key: max(int(after.get(key, 0)) - int(before.get(key, 0)), 0)
        for key in after
    }


def curl_download(url: str, *, expected_minimum: int) -> dict[str, Any]:
    completed = subprocess.run(
        [
            "curl.exe",
            "--noproxy",
            "*",
            "--silent",
            "--show-error",
            "--fail",
            "--location",
            "--connect-timeout",
            "10",
            "--max-time",
            "60",
            "--output",
            "NUL",
            "--write-out",
            "%{http_code} %{size_download} %{time_total}",
            url,
        ],
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
        timeout=70.0,
        env={
            **os.environ,
            "HTTP_PROXY": "",
            "HTTPS_PROXY": "",
            "ALL_PROXY": "",
            "NO_PROXY": "*",
        },
        creationflags=getattr(subprocess, "CREATE_NO_WINDOW", 0),
    )
    if completed.returncode != 0:
        raise RuntimeError(
            f"curl failed ({completed.returncode}): "
            f"{(completed.stderr or completed.stdout).strip()}"
        )
    parts = completed.stdout.strip().split()
    if len(parts) != 3:
        raise RuntimeError(f"curl returned unexpected metrics: {completed.stdout!r}")
    downloaded = int(float(parts[1]))
    if downloaded < expected_minimum:
        raise RuntimeError(
            f"downloaded {downloaded} bytes, expected at least {expected_minimum}"
        )
    return {
        "url": url,
        "http_code": int(parts[0]),
        "bytes": downloaded,
        "seconds": float(parts[2]),
    }


def ntp_udp_probe(target: str = "203.107.6.88") -> dict[str, Any]:
    script = rf"""
$ErrorActionPreference = 'Stop'
$client = [System.Net.Sockets.UdpClient]::new()
try {{
  $client.Client.ReceiveTimeout = 10000
  $client.Connect('{target}', 123)
  [byte[]]$packet = New-Object byte[] 48
  $packet[0] = 0x1B
  [void]$client.Send($packet, $packet.Length)
  $remote = [System.Net.IPEndPoint]::new([System.Net.IPAddress]::Any, 0)
  $response = $client.Receive([ref]$remote)
  [pscustomobject]@{{
    target = '{target}:123'
    response_bytes = $response.Length
    remote = $remote.ToString()
    leap_version_mode = $response[0]
  }} | ConvertTo-Json -Compress
}} finally {{
  $client.Dispose()
}}
"""
    output = powershell(script, timeout=15.0)
    result = json.loads(output)
    if int(result.get("response_bytes", 0)) < 48:
        raise RuntimeError(f"NTP response was too short: {result!r}")
    return result


def wait_for_hypomux_route(*, present: bool) -> dict[str, Any]:
    deadline = time.monotonic() + 15.0
    last = hypomux_residue()
    while time.monotonic() < deadline:
        has_route = bool(last["routes"])
        if has_route is present:
            return last
        time.sleep(0.5)
        last = hypomux_residue()
    expected = "appear" if present else "disappear"
    raise RuntimeError(f"HypoMux TUN route did not {expected}: {last!r}")


def engine_start_config() -> dict[str, Any]:
    return {
        "mode": "tun_tcp_pool",
        "listen_host": "127.0.0.1",
        "weighted": False,
        "connect_timeout_ms": 6000,
        "adapters": ADAPTERS,
        "channels": [
            {
                "name": "nic_ethernet",
                "port": 0,
                "adapter_names": ["Ethernet"],
            },
            {
                "name": "nic_wifi",
                "port": 0,
                "adapter_names": ["Wi-Fi"],
            },
            {
                "name": "aggregation",
                "port": 0,
                "adapter_names": ["Ethernet", "Wi-Fi"],
            },
        ],
    }


def run_tun_once(
    *,
    strict_route: bool,
    output_name: str,
    full_data_plane: bool,
) -> dict[str, Any]:
    events: list[dict[str, Any]] = []
    stderr: list[str] = []
    client = EngineClient(
        str(ENGINE),
        request_timeout=8.0,
        shutdown_timeout=25.0,
        on_event=lambda item: events.append(dict(item)),
        on_stderr=stderr.append,
    )
    started = False
    activated = False
    result: dict[str, Any] = {
        "strict_route": strict_route,
        "started_at": utc_now(),
        "passed": False,
    }
    config_path = (OUTPUT_ROOT / output_name).resolve()
    try:
        ethernet_alias = interface_alias_for_ip("192.168.1.146")
        hello = client.start()
        pool = client.request(
            "engine.start",
            engine_start_config(),
            timeout=10.0,
        )
        started = True
        ports = channel_ports(pool)
        config = build_config(
            [],
            ethernet_port=ports["nic_ethernet"],
            wifi_port=ports["nic_wifi"],
            aggregation_port=ports["aggregation"],
            strict_route=strict_route,
            dns_plan={
                "mode": "legacy",
                "server": "223.5.5.5",
                "bind_interface": ethernet_alias,
                "bind_ip": "192.168.1.146",
            },
            app_process_path=[
                str(ENGINE),
                str(SING_BOX),
                sys.executable,
            ],
        )
        config_path.write_text(
            json.dumps(config, ensure_ascii=False, indent=2),
            encoding="utf-8",
        )
        activation = client.request(
            "tun.activate",
            {
                "executable": str(SING_BOX),
                "config_path": str(config_path),
                "startup_timeout_ms": 2000,
            },
            timeout=40.0,
        )
        activated = True
        active_residue = wait_for_hypomux_route(present=True)
        before = telemetry_totals(
            client.request(
                "engine.telemetry",
                {"include_connections": True},
                timeout=5.0,
            )
        )
        connectivity = curl_download(
            "http://www.msftconnecttest.com/connecttest.txt",
            expected_minimum=20,
        )
        if full_data_plane:
            sustained = curl_download(
                "https://speed.cloudflare.com/__down?bytes=1048576",
                expected_minimum=1024 * 1024,
            )
            after_tcp = telemetry_totals(
                client.request("engine.telemetry", timeout=5.0)
            )
            udp = ntp_udp_probe()
            after_udp = telemetry_totals(
                client.request("engine.telemetry", timeout=5.0)
            )
            tcp_delta = delta(after_tcp, before)
            udp_delta = delta(after_udp, after_tcp)
            if tcp_delta["bytes_down"] <= 0:
                raise RuntimeError("TCP download did not advance engine telemetry")
            if udp_delta["bytes_up"] <= 0 or udp_delta["bytes_down"] <= 0:
                raise RuntimeError(
                    f"UDP probe did not advance byte telemetry: {udp_delta}"
                )
            result.update(
                {
                    "sustained_download": sustained,
                    "udp_probe": udp,
                    "tcp_telemetry_delta": tcp_delta,
                    "udp_telemetry_delta": udp_delta,
                }
            )
        result.update(
            {
                "engine_hello": hello,
                "pool_ports": ports,
                "activation": activation,
                "active_hypomux_state": active_residue,
                "connectivity": connectivity,
                "passed": True,
            }
        )
    except Exception as exc:
        result["error"] = f"{type(exc).__name__}: {exc}"
        raise
    finally:
        cleanup_errors = []
        if activated and client.is_running():
            try:
                client.request("tun.deactivate", timeout=25.0)
            except Exception as exc:
                cleanup_errors.append(f"tun.deactivate: {type(exc).__name__}: {exc}")
        if started and client.is_running():
            try:
                client.request("engine.stop", timeout=25.0)
            except Exception as exc:
                cleanup_errors.append(f"engine.stop: {type(exc).__name__}: {exc}")
        if client.is_running():
            try:
                client.stop(graceful=True)
            except Exception as exc:
                cleanup_errors.append(f"host.shutdown: {type(exc).__name__}: {exc}")
        result["events"] = events
        result["engine_stderr"] = stderr
        result["cleanup_errors"] = cleanup_errors
        try:
            result["post_cleanup"] = wait_for_hypomux_route(present=False)
        except Exception as exc:
            result["cleanup_errors"].append(
                f"residue check: {type(exc).__name__}: {exc}"
            )
        if result["cleanup_errors"]:
            result["passed"] = False
        result["finished_at"] = utc_now()
        result_path = OUTPUT_ROOT / (
            "tun-strict-probe.json"
            if strict_route
            else "tun-compatibility-probe.json"
        )
        result_path.write_text(
            json.dumps(result, ensure_ascii=False, indent=2),
            encoding="utf-8",
        )
    return result


def run_qualification() -> dict[str, Any]:
    initial = preflight()
    if not initial["ready_to_run"]:
        raise RuntimeError(
            "TUN qualification preflight is not ready: "
            + json.dumps(initial["checks"], ensure_ascii=False)
        )
    strict_result = run_tun_once(
        strict_route=True,
        output_name="singbox-tun-strict.json",
        full_data_plane=True,
    )
    compatibility_result = run_tun_once(
        strict_route=False,
        output_name="singbox-tun-compatibility.json",
        full_data_plane=False,
    )
    postflight_residue = hypomux_residue()
    final = {
        "kind": "hypomux.phase14.tun_wfp_qualification",
        "started_at": initial["captured_at"],
        "finished_at": utc_now(),
        "candidate_sha256": EXPECTED_ENGINE_SHA256,
        "strict_route": strict_result,
        "controlled_compatibility_restart": compatibility_result,
        "postflight_residue": postflight_residue,
        "passed": bool(
            strict_result["passed"]
            and compatibility_result["passed"]
            and not postflight_residue["routes"]
            and not postflight_residue["devices"]
        ),
    }
    (OUTPUT_ROOT / "tun-wfp-probe.json").write_text(
        json.dumps(final, ensure_ascii=False, indent=2),
        encoding="utf-8",
    )
    return final


def main() -> int:
    parser = argparse.ArgumentParser()
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument("--preflight", action="store_true")
    mode.add_argument("--run", action="store_true")
    options = parser.parse_args()

    OUTPUT_ROOT.mkdir(parents=True, exist_ok=True)
    try:
        report = preflight() if options.preflight else run_qualification()
    except Exception as exc:
        report = {
            "kind": "hypomux.phase14.tun_wfp_failure",
            "captured_at": utc_now(),
            "passed": False,
            "error": f"{type(exc).__name__}: {exc}",
        }
        if options.run:
            report["postflight_residue"] = hypomux_residue()
    output_path = OUTPUT_ROOT / (
        "tun-preflight.json" if options.preflight else "tun-wfp-probe.json"
    )
    output_path.write_text(
        json.dumps(report, ensure_ascii=False, indent=2),
        encoding="utf-8",
    )
    print(json.dumps(report, ensure_ascii=False, indent=2))
    return 0 if report.get("ready_to_run") or report.get("passed") else 1


if __name__ == "__main__":
    raise SystemExit(main())
