"""Read-only strict-Go qualification and Windows residue reporting."""

from __future__ import annotations

import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import platform
import subprocess
from typing import Any, Callable, Sequence

from .capabilities import (
    PROXY_MODE,
    TUN_MODE,
    missing_mode_requirements,
)
from .client import EngineClient
from .development import resolve_engine_command


REPORT_SCHEMA_VERSION = 1
_CREATE_NO_WINDOW = getattr(subprocess, "CREATE_NO_WINDOW", 0)

_WINDOWS_SNAPSHOT_SCRIPT = r"""
$routes = @(Get-NetRoute -ErrorAction SilentlyContinue |
  Where-Object {
    $_.InterfaceAlias -eq 'HypoMux-Tun' -and
    ($_.DestinationPrefix -eq '0.0.0.0/0' -or
     $_.DestinationPrefix -eq '::/0')
  } |
  Select-Object InterfaceAlias, DestinationPrefix, InterfaceIndex, RouteMetric)
$devices = @(Get-PnpDevice -Class Net -ErrorAction SilentlyContinue |
  Where-Object {
    $_.FriendlyName -eq 'HypoMux-Tun' -and $_.InstanceId -like '*WINTUN*'
  } |
  Select-Object FriendlyName, InstanceId, Status)
$processes = @(Get-CimInstance Win32_Process -ErrorAction SilentlyContinue |
  Where-Object {
    $_.Name -eq 'hypomux-engine.exe' -or $_.Name -eq 'sing-box.exe'
  } |
  Select-Object ProcessId, Name, ExecutablePath)
[ordered]@{
  supported = $true
  routes = $routes
  devices = $devices
  processes = $processes
} | ConvertTo-Json -Depth 5 -Compress
"""

_AUTHENTICODE_SCRIPT = r"""
$signature = Get-AuthenticodeSignature -LiteralPath $env:HYPOMUX_QUALIFICATION_ENGINE
[ordered]@{
  status = [string]$signature.Status
  status_message = [string]$signature.StatusMessage
  signature_type = [string]$signature.SignatureType
  signer_subject = [string]$signature.SignerCertificate.Subject
  signer_issuer = [string]$signature.SignerCertificate.Issuer
  signer_thumbprint = [string]$signature.SignerCertificate.Thumbprint
} | ConvertTo-Json -Compress
"""


def _utc_now() -> str:
    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")


def inspect_executable_identity(executable: str) -> dict[str, Any]:
    """Return a stable identity used to bind reports to one exact candidate."""

    path = Path(executable)
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return {
        "size": path.stat().st_size,
        "sha256": digest.hexdigest(),
    }


def _powershell_json(
    script: str,
    *,
    environment: dict[str, str] | None = None,
    timeout: float = 15.0,
) -> dict[str, Any]:
    utf8_script = (
        "$utf8 = New-Object System.Text.UTF8Encoding($false); "
        "[Console]::OutputEncoding = $utf8; $OutputEncoding = $utf8; "
        + script
    )
    child_environment = os.environ.copy()
    if environment:
        child_environment.update(environment)
    completed = subprocess.run(
        [
            "powershell",
            "-NoProfile",
            "-NonInteractive",
            "-Command",
            utf8_script,
        ],
        stdin=subprocess.DEVNULL,
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
        timeout=timeout,
        env=child_environment,
        creationflags=_CREATE_NO_WINDOW,
    )
    if completed.returncode != 0:
        detail = (completed.stderr or completed.stdout).strip()
        raise RuntimeError(
            f"PowerShell audit failed (code={completed.returncode}): {detail}"
        )
    payload = completed.stdout.strip()
    if not payload:
        raise RuntimeError("PowerShell audit returned no JSON")
    value = json.loads(payload)
    if not isinstance(value, dict):
        raise RuntimeError("PowerShell audit returned a non-object")
    return value


def capture_windows_snapshot() -> dict[str, Any]:
    """Capture exact HypoMux network/process residue without changing it."""

    if os.name != "nt":
        return {
            "supported": False,
            "routes": [],
            "devices": [],
            "processes": [],
            "note": "Windows residue audit is unavailable on this platform",
        }
    return _powershell_json(_WINDOWS_SNAPSHOT_SCRIPT)


def inspect_authenticode(executable: str) -> dict[str, Any]:
    if os.name != "nt":
        return {
            "status": "UnsupportedPlatform",
            "status_message": "Authenticode is available only on Windows",
        }
    return _powershell_json(
        _AUTHENTICODE_SCRIPT,
        environment={"HYPOMUX_QUALIFICATION_ENGINE": executable},
    )


def _residue(snapshot: dict[str, Any]) -> dict[str, int]:
    def count(name: str) -> int:
        value = snapshot.get(name)
        return len(value) if isinstance(value, list) else 0

    return {
        "routes": count("routes"),
        "devices": count("devices"),
    }


def _owned_processes(
    snapshot: dict[str, Any],
    engine_executable: str,
) -> list[dict[str, Any]]:
    processes = snapshot.get("processes")
    if not isinstance(processes, list):
        return []
    engine_path = os.path.normcase(os.path.abspath(engine_executable))
    singbox_path = os.path.normcase(
        os.path.join(os.path.dirname(engine_path), "sing-box.exe")
    )
    owned = []
    for item in processes:
        if not isinstance(item, dict):
            continue
        executable_path = item.get("ExecutablePath", item.get("executable_path"))
        if not executable_path:
            continue
        normalized = os.path.normcase(os.path.abspath(str(executable_path)))
        if normalized in {engine_path, singbox_path}:
            owned.append(item)
    return owned


def _add_check(
    checks: list[dict[str, Any]],
    name: str,
    passed: bool,
    detail: Any,
    *,
    required: bool = True,
) -> None:
    checks.append(
        {
            "name": name,
            "passed": bool(passed),
            "required": bool(required),
            "detail": detail,
        }
    )


def run_read_only_qualification(
    command: str | os.PathLike[str] | Sequence[str | os.PathLike[str]],
    *,
    require_elevated: bool = False,
    require_signed: bool = False,
    allowed_test_signer_thumbprints: Sequence[str] = (),
    snapshotter: Callable[[], dict[str, Any]] = capture_windows_snapshot,
    signature_inspector: Callable[[str], dict[str, Any]] = inspect_authenticode,
    client_factory: Callable[..., EngineClient] = EngineClient,
) -> dict[str, Any]:
    """Qualify packaging/protocol/cleanup without starting a network mode."""

    command_parts = (
        [os.fspath(command)]
        if isinstance(command, (str, os.PathLike))
        else [os.fspath(part) for part in command]
    )
    executable = str(Path(command_parts[0]).resolve())
    report: dict[str, Any] = {
        "schema_version": REPORT_SCHEMA_VERSION,
        "kind": "hypomux.strict_go.read_only",
        "started_at": _utc_now(),
        "host": {
            "os": platform.system().lower(),
            "release": platform.release(),
            "machine": platform.machine().lower(),
        },
        "engine": {"executable": executable},
        "network_modes_started": False,
        "checks": [],
        "snapshots": {},
    }
    checks = report["checks"]

    try:
        identity = inspect_executable_identity(executable)
    except Exception as exc:
        identity = {"error": f"{type(exc).__name__}: {exc}"}
    report["engine"].update(identity)
    _add_check(
        checks,
        "engine_executable_identified",
        isinstance(identity.get("sha256"), str)
        and len(identity["sha256"]) == 64,
        identity,
    )

    try:
        before = snapshotter()
    except Exception as exc:
        before = {"supported": False, "error": str(exc)}
    report["snapshots"]["before"] = before
    _add_check(
        checks,
        "windows_residue_audit_available",
        before.get("supported") is True,
        before.get("error") or before.get("note") or "available",
    )
    before_residue = _residue(before)
    _add_check(
        checks,
        "no_preexisting_hypomux_tun_residue",
        not any(before_residue.values()),
        before_residue,
    )
    before_owned_processes = _owned_processes(before, executable)
    _add_check(
        checks,
        "no_preexisting_hypomux_owned_processes",
        not before_owned_processes,
        before_owned_processes,
    )

    try:
        signature = signature_inspector(executable)
    except Exception as exc:
        signature = {"status": "AuditFailed", "status_message": str(exc)}
    report["engine"]["authenticode"] = signature
    signature_valid = signature.get("status") == "Valid"
    allowed_thumbprints = {
        str(value).replace(" ", "").upper()
        for value in allowed_test_signer_thumbprints
        if str(value).strip()
    }
    signer_thumbprint = (
        str(signature.get("signer_thumbprint", ""))
        .replace(" ", "")
        .upper()
    )
    pinned_test_signature = (
        signature.get("status") in {"UnknownError", "NotTrusted"}
        and signature.get("signature_type") == "Authenticode"
        and bool(signer_thumbprint)
        and signer_thumbprint in allowed_thumbprints
    )
    signature_acceptance = (
        "trusted"
        if signature_valid
        else "pinned-test"
        if pinned_test_signature
        else "rejected"
    )
    report["engine"]["signature_acceptance"] = signature_acceptance
    _add_check(
        checks,
        "engine_authenticode_valid",
        signature_valid,
        signature,
        required=False,
    )
    _add_check(
        checks,
        "engine_signature_policy",
        signature_valid or pinned_test_signature,
        {
            "acceptance": signature_acceptance,
            "signer_thumbprint": signer_thumbprint,
            "allowed_test_signer_thumbprints": sorted(allowed_thumbprints),
        },
        required=require_signed,
    )

    client = client_factory(command_parts)
    engine_pid = 0
    try:
        hello = client.start()
        report["engine"]["hello"] = hello
        engine_pid = int(hello.get("pid", 0) or 0)
        _add_check(
            checks,
            "protocol_v1",
            hello.get("protocol_version") == 1,
            hello.get("protocol_version"),
        )
        _add_check(
            checks,
            "engine_elevated",
            bool(hello.get("elevated")),
            bool(hello.get("elevated")),
            required=require_elevated,
        )
        for mode in (PROXY_MODE, TUN_MODE):
            missing = missing_mode_requirements(hello, mode)
            _add_check(
                checks,
                f"{mode}_strict_contract",
                not any(missing.values()),
                missing,
            )

        status = client.request("engine.status", timeout=5.0)
        health = client.request("health.check", timeout=5.0)
        tun_status = client.request("tun.status", timeout=5.0)
        report["engine"]["status"] = status
        report["engine"]["health"] = health
        report["engine"]["tun_status"] = tun_status
        _add_check(
            checks,
            "engine_initially_stopped",
            status.get("engine", {}).get("state") == "stopped",
            status.get("engine", {}).get("state"),
        )
        _add_check(
            checks,
            "tun_initially_stopped",
            tun_status.get("state") == "stopped",
            tun_status.get("state"),
        )
        _add_check(
            checks,
            "health_check",
            health.get("ok") is True,
            health,
        )
    except Exception as exc:
        _add_check(
            checks,
            "engine_protocol_session",
            False,
            f"{type(exc).__name__}: {exc}",
        )
    finally:
        try:
            stop_action = client.stop(graceful=True)
            report["engine"]["stop_action"] = stop_action
            _add_check(
                checks,
                "engine_graceful_stop",
                stop_action in {"exited", "not-started"},
                stop_action,
            )
        except Exception as exc:
            _add_check(
                checks,
                "engine_graceful_stop",
                False,
                f"{type(exc).__name__}: {exc}",
            )

    try:
        after = snapshotter()
    except Exception as exc:
        after = {"supported": False, "error": str(exc)}
    report["snapshots"]["after"] = after
    _add_check(
        checks,
        "windows_postflight_audit_available",
        after.get("supported") is True,
        after.get("error") or after.get("note") or "available",
    )
    after_residue = _residue(after)
    _add_check(
        checks,
        "no_postflight_hypomux_tun_residue",
        not any(after_residue.values()),
        after_residue,
    )
    after_owned_processes = _owned_processes(after, executable)
    _add_check(
        checks,
        "no_postflight_hypomux_owned_processes",
        not after_owned_processes,
        after_owned_processes,
    )
    processes = after.get("processes")
    lingering_engine = False
    if engine_pid and isinstance(processes, list):
        lingering_engine = any(
            int(item.get("ProcessId", item.get("process_id", 0)) or 0)
            == engine_pid
            for item in processes
            if isinstance(item, dict)
        )
    _add_check(
        checks,
        "qualification_engine_process_reaped",
        not lingering_engine,
        {"pid": engine_pid, "lingering": lingering_engine},
    )

    report["finished_at"] = _utc_now()
    report["passed"] = all(
        check["passed"]
        for check in checks
        if check.get("required", True)
    )
    return report


def _parse_arguments(arguments: Sequence[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description=(
            "Generate a read-only strict-Go qualification report. "
            "No proxy or TUN network mode is started."
        )
    )
    parser.add_argument("--engine", help="exact hypomux-engine.exe path")
    parser.add_argument(
        "--runtime-dir",
        default=".",
        help="runtime root used for packaged engine discovery",
    )
    parser.add_argument("--output", required=True, help="JSON report path")
    parser.add_argument("--require-elevated", action="store_true")
    parser.add_argument("--require-signed", action="store_true")
    parser.add_argument(
        "--allow-test-signer-thumbprint",
        action="append",
        default=[],
        help=(
            "allow an Authenticode test signer with this exact SHA-1 "
            "certificate thumbprint when its root is untrusted; repeatable"
        ),
    )
    return parser.parse_args(arguments)


def main(arguments: Sequence[str] | None = None) -> int:
    options = _parse_arguments(arguments)
    if options.engine:
        engine_path = Path(options.engine).expanduser().resolve()
        command = [str(engine_path)] if engine_path.is_file() else None
    else:
        command = resolve_engine_command(options.runtime_dir)
    if command is None:
        raise SystemExit("hypomux-engine.exe was not found")

    report = run_read_only_qualification(
        command,
        require_elevated=options.require_elevated,
        require_signed=options.require_signed,
        allowed_test_signer_thumbprints=options.allow_test_signer_thumbprint,
    )
    output = Path(options.output).expanduser().resolve()
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(
        json.dumps(report, ensure_ascii=False, indent=2) + "\n",
        encoding="utf-8",
    )
    print(
        json.dumps(
            {
                "passed": report["passed"],
                "output": str(output),
                "network_modes_started": False,
            },
            ensure_ascii=False,
        )
    )
    return 0 if report["passed"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
