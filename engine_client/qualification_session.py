"""Operator session and evidence gate for strict-Go physical qualification."""

from __future__ import annotations

import argparse
from copy import deepcopy
from datetime import datetime, timezone
import json
import os
from pathlib import Path
from typing import Any, Sequence

from .capabilities import PROXY_MODE, TUN_MODE
from .qualification import (
    run_read_only_qualification,
)


SESSION_SCHEMA_VERSION = 1
SESSION_KIND = "hypomux.strict_go.physical_session"
REPORT_KIND = "hypomux.strict_go.read_only"

SCENARIOS: tuple[tuple[str, str], ...] = (
    ("package", "Fresh install and signed packaged engine"),
    ("proxy", "SOCKS5 and HTTP CONNECT on selected adapters"),
    ("dns", "Traditional DNS, DoH, A, and AAAA"),
    ("scheduling", "Scheduling, adapter failure, and recovery"),
    ("ipv6", "IPv6-capable and IPv4-only adapter selection"),
    ("tun_tcp", "Managed TUN TCP and sustained download"),
    ("tun_udp", "Managed TUN UDP or QUIC"),
    ("wfp", "Strict WFP route and compatibility retry"),
    ("lifecycle", "Start, stop, restart, tray, and full exit"),
    ("crash", "UI and Go-host crash recovery"),
    ("adapter_churn", "Physical adapter disable and re-enable"),
    ("power", "Sleep and resume"),
    ("upgrade", "Upgrade over previous production version"),
    ("uninstall", "Uninstall and residue cleanup"),
)

POSTFLIGHT_CHECKS = (
    "windows_residue_audit_available",
    "no_preexisting_hypomux_tun_residue",
    "no_preexisting_hypomux_owned_processes",
    f"{PROXY_MODE}_strict_contract",
    f"{TUN_MODE}_strict_contract",
    "engine_graceful_stop",
    "windows_postflight_audit_available",
    "no_postflight_hypomux_tun_residue",
    "no_postflight_hypomux_owned_processes",
    "qualification_engine_process_reaped",
)


def _utc_now() -> str:
    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")


def _write_json(path: Path, value: dict[str, Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = path.with_suffix(path.suffix + ".tmp")
    temporary.write_text(
        json.dumps(value, ensure_ascii=False, indent=2) + "\n",
        encoding="utf-8",
    )
    os.replace(temporary, path)


def _read_json(path: Path) -> dict[str, Any]:
    value = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(value, dict):
        raise ValueError(f"{path} does not contain a JSON object")
    return value


def _relative_artifact(path: Path, session_path: Path) -> str:
    try:
        return str(path.resolve().relative_to(session_path.parent.resolve()))
    except ValueError:
        return str(path.resolve())


def _scenario(session: dict[str, Any], scenario_id: str) -> dict[str, Any]:
    for item in session.get("scenarios", []):
        if item.get("id") == scenario_id:
            return item
    valid = ", ".join(identifier for identifier, _title in SCENARIOS)
    raise ValueError(f"unknown scenario {scenario_id!r}; expected one of: {valid}")


def _checks_by_name(report: dict[str, Any]) -> dict[str, dict[str, Any]]:
    return {
        str(item.get("name")): item
        for item in report.get("checks", [])
        if isinstance(item, dict) and item.get("name")
    }


def validate_postflight(
    report: dict[str, Any],
    candidate: dict[str, Any],
    policy: dict[str, Any] | None = None,
) -> list[str]:
    """Return exact reasons a postflight report cannot close a scenario."""

    errors: list[str] = []
    if report.get("kind") != REPORT_KIND:
        errors.append("unexpected report kind")
    if report.get("network_modes_started") is not False:
        errors.append("postflight must be read-only")
    if report.get("passed") is not True:
        errors.append("postflight report did not pass")

    report_engine = report.get("engine", {})
    if report_engine.get("sha256") != candidate.get("sha256"):
        errors.append("postflight engine SHA-256 does not match the candidate")
    hello = report_engine.get("hello", {})
    for key in ("engine_version", "commit"):
        expected = candidate.get(key)
        if expected and hello.get(key) != expected:
            errors.append(f"postflight engine {key} does not match the candidate")

    checks = _checks_by_name(report)
    required_checks = list(POSTFLIGHT_CHECKS)
    policy = policy or {}
    if policy.get("require_signed"):
        required_checks.append("engine_signature_policy")
    if policy.get("require_elevated"):
        required_checks.append("engine_elevated")
    for name in required_checks:
        check = checks.get(name)
        if not check:
            errors.append(f"postflight check is missing: {name}")
        elif check.get("passed") is not True:
            errors.append(f"postflight check failed: {name}")
    return errors


def refresh_verdict(session: dict[str, Any]) -> dict[str, Any]:
    policy = session.get("policy", {})
    preflight = session.get("preflight", {})
    scenarios = session.get("scenarios", [])
    pending = [
        str(item.get("id"))
        for item in scenarios
        if item.get("status") != "passed"
    ]
    reasons: list[str] = []
    if not policy.get("require_elevated"):
        reasons.append("session did not require an elevated engine")
    if not policy.get("require_signed"):
        reasons.append("session did not require a signed engine")
    if preflight.get("passed") is not True:
        reasons.append("candidate preflight did not pass")
    if pending:
        reasons.append("physical scenarios not passed: " + ", ".join(pending))
    verdict = {
        "python_removal_ready": not reasons,
        "reasons": reasons,
        "passed": len(scenarios) - len(pending),
        "total": len(scenarios),
    }
    session["verdict"] = verdict
    session["updated_at"] = _utc_now()
    return verdict


def prepare_session(
    engine: str | os.PathLike[str],
    output_directory: str | os.PathLike[str],
    *,
    require_elevated: bool = True,
    require_signed: bool = True,
    allowed_test_signer_thumbprints: Sequence[str] = (),
) -> tuple[Path, dict[str, Any]]:
    engine_path = Path(engine).expanduser().resolve()
    if not engine_path.is_file():
        raise FileNotFoundError(f"candidate engine was not found: {engine_path}")
    output = Path(output_directory).expanduser().resolve()
    session_path = output / "session.json"
    if session_path.exists():
        raise FileExistsError(
            f"qualification session already exists: {session_path}"
        )

    preflight = run_read_only_qualification(
        [str(engine_path)],
        require_elevated=require_elevated,
        require_signed=require_signed,
        allowed_test_signer_thumbprints=allowed_test_signer_thumbprints,
    )
    preflight_path = output / "preflight.json"
    _write_json(preflight_path, preflight)
    preflight_engine = preflight.get("engine", {})
    hello = preflight_engine.get("hello", {})
    session: dict[str, Any] = {
        "schema_version": SESSION_SCHEMA_VERSION,
        "kind": SESSION_KIND,
        "created_at": _utc_now(),
        "updated_at": _utc_now(),
        "candidate": {
            "executable": str(engine_path),
            "size": preflight_engine.get("size"),
            "sha256": preflight_engine.get("sha256"),
            "engine_version": hello.get("engine_version"),
            "commit": hello.get("commit"),
        },
        "policy": {
            "require_elevated": require_elevated,
            "require_signed": require_signed,
            "allowed_test_signer_thumbprints": sorted(
                {
                    str(value).replace(" ", "").upper()
                    for value in allowed_test_signer_thumbprints
                    if str(value).strip()
                }
            ),
        },
        "preflight": {
            "path": _relative_artifact(preflight_path, session_path),
            "passed": preflight.get("passed") is True,
        },
        "scenarios": [
            {
                "id": identifier,
                "title": title,
                "status": "pending",
                "evidence": [],
                "notes": "",
                "postflight": None,
                "recorded_at": None,
            }
            for identifier, title in SCENARIOS
        ],
    }
    refresh_verdict(session)
    _write_json(session_path, session)
    return session_path, session


def record_scenario(
    session_path: str | os.PathLike[str],
    scenario_id: str,
    result: str,
    *,
    evidence: Sequence[str] = (),
    notes: str = "",
    postflight_path: str | os.PathLike[str] | None = None,
) -> dict[str, Any]:
    path = Path(session_path).expanduser().resolve()
    session = _read_json(path)
    if session.get("kind") != SESSION_KIND:
        raise ValueError("unexpected qualification session kind")
    if result not in {"passed", "failed", "blocked"}:
        raise ValueError("result must be passed, failed, or blocked")

    item = _scenario(session, scenario_id)
    postflight_reference = None
    if postflight_path is not None:
        resolved_postflight = Path(postflight_path).expanduser().resolve()
        report = _read_json(resolved_postflight)
        errors = validate_postflight(
            report,
            session.get("candidate", {}),
            session.get("policy", {}),
        )
        if result == "passed" and errors:
            raise ValueError("; ".join(errors))
        postflight_reference = _relative_artifact(resolved_postflight, path)
    elif result == "passed":
        raise ValueError("a passed scenario requires a postflight report")

    clean_evidence = [value.strip() for value in evidence if value.strip()]
    if result == "passed" and not clean_evidence:
        raise ValueError("a passed scenario requires at least one evidence note")
    item.update(
        {
            "status": result,
            "evidence": clean_evidence,
            "notes": notes.strip(),
            "postflight": postflight_reference,
            "recorded_at": _utc_now(),
        }
    )
    refresh_verdict(session)
    _write_json(path, session)
    return session


def capture_postflight(
    session_path: str | os.PathLike[str],
    scenario_id: str,
    result: str,
    *,
    evidence: Sequence[str] = (),
    notes: str = "",
) -> tuple[dict[str, Any], Path]:
    path = Path(session_path).expanduser().resolve()
    session = _read_json(path)
    candidate = session.get("candidate", {})
    policy = session.get("policy", {})
    report = run_read_only_qualification(
        [str(candidate.get("executable", ""))],
        require_elevated=bool(policy.get("require_elevated")),
        require_signed=bool(policy.get("require_signed")),
        allowed_test_signer_thumbprints=policy.get(
            "allowed_test_signer_thumbprints",
            (),
        ),
    )
    report_path = path.parent / f"postflight-{scenario_id}.json"
    _write_json(report_path, report)
    errors = validate_postflight(report, candidate, policy)
    effective_result = result
    effective_notes = notes
    if result == "passed" and errors:
        effective_result = "failed"
        gate_note = "Postflight gate: " + "; ".join(errors)
        effective_notes = f"{notes.strip()} {gate_note}".strip()
    updated = record_scenario(
        path,
        scenario_id,
        effective_result,
        evidence=evidence,
        notes=effective_notes,
        postflight_path=report_path,
    )
    return updated, report_path


def session_summary(session: dict[str, Any]) -> dict[str, Any]:
    scenarios = session.get("scenarios", [])
    counts = {
        status: sum(item.get("status") == status for item in scenarios)
        for status in ("passed", "failed", "blocked", "pending")
    }
    return {
        "candidate": deepcopy(session.get("candidate", {})),
        "policy": deepcopy(session.get("policy", {})),
        "preflight": deepcopy(session.get("preflight", {})),
        "counts": counts,
        "verdict": deepcopy(session.get("verdict", {})),
    }


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="Manage the strict-Go physical qualification evidence gate"
    )
    commands = parser.add_subparsers(dest="command", required=True)

    prepare = commands.add_parser("prepare")
    prepare.add_argument("--engine", required=True)
    prepare.add_argument("--output-dir", required=True)
    prepare.add_argument(
        "--development",
        action="store_true",
        help="do not require signature/elevation; never qualifies Python removal",
    )
    prepare.add_argument(
        "--allow-test-signer-thumbprint",
        action="append",
        default=[],
        help="pin an exact SignPath test certificate thumbprint; repeatable",
    )

    for name in ("record", "capture"):
        command = commands.add_parser(name)
        command.add_argument("--session", required=True)
        command.add_argument("--scenario", required=True)
        command.add_argument(
            "--result",
            choices=("passed", "failed", "blocked"),
            required=True,
        )
        command.add_argument("--evidence", action="append", default=[])
        command.add_argument("--notes", default="")
        if name == "record":
            command.add_argument("--postflight")

    summary = commands.add_parser("summary")
    summary.add_argument("--session", required=True)
    return parser


def main(arguments: Sequence[str] | None = None) -> int:
    options = _parser().parse_args(arguments)
    if options.command == "prepare":
        path, session = prepare_session(
            options.engine,
            options.output_dir,
            require_elevated=not options.development,
            require_signed=not options.development,
            allowed_test_signer_thumbprints=(
                options.allow_test_signer_thumbprint
            ),
        )
        summary = session_summary(session)
        summary["session"] = str(path)
    elif options.command == "record":
        session = record_scenario(
            options.session,
            options.scenario,
            options.result,
            evidence=options.evidence,
            notes=options.notes,
            postflight_path=options.postflight,
        )
        summary = session_summary(session)
    elif options.command == "capture":
        session, report_path = capture_postflight(
            options.session,
            options.scenario,
            options.result,
            evidence=options.evidence,
            notes=options.notes,
        )
        summary = session_summary(session)
        summary["postflight"] = str(report_path)
    else:
        session = _read_json(Path(options.session).expanduser().resolve())
        summary = session_summary(session)

    print(json.dumps(summary, ensure_ascii=False, indent=2))
    if options.command == "prepare":
        return 0 if summary["preflight"].get("passed") else 1
    if options.command == "summary":
        return 0 if summary["verdict"].get("python_removal_ready") else 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
