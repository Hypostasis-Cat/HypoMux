from __future__ import annotations

import asyncio
import json
import os
import unittest
from unittest.mock import patch

from utils import diagnostic_runner


class _FakeProcess:
    def __init__(self, output: dict):
        self._output = output
        self.killed = False

    async def communicate(self):
        return json.dumps(self._output).encode("utf-8"), b""

    def kill(self):
        self.killed = True


def test_engine_path_prefers_explicit_development_override(tmp_path, monkeypatch):
    engine = tmp_path / diagnostic_runner.ENGINE_EXE_NAME
    engine.write_bytes(b"engine")
    monkeypatch.setenv(diagnostic_runner.ENGINE_PATH_ENV, str(engine))

    assert diagnostic_runner.get_engine_path() == str(engine.resolve())
    assert diagnostic_runner.get_diagnostic_path() == str(engine.resolve())


def test_engine_path_finds_installed_bin_layout(tmp_path, monkeypatch):
    engine = tmp_path / "bin" / diagnostic_runner.ENGINE_EXE_NAME
    engine.parent.mkdir()
    engine.write_bytes(b"engine")
    monkeypatch.delenv(diagnostic_runner.ENGINE_PATH_ENV, raising=False)
    monkeypatch.setattr(diagnostic_runner, "_base_dir", lambda: str(tmp_path))

    assert diagnostic_runner.get_engine_path() == str(engine.resolve())


def test_run_diagnostic_invokes_go_diagnose_command(tmp_path, monkeypatch):
    engine = tmp_path / diagnostic_runner.ENGINE_EXE_NAME
    engine.write_bytes(b"engine")
    monkeypatch.setenv(diagnostic_runner.ENGINE_PATH_ENV, str(engine))
    calls: list[tuple] = []

    async def create_process(*args, **kwargs):
        calls.append((args, kwargs))
        return _FakeProcess(
            {
                "status": "available",
                "loss_rate": 0,
                "avg_latency_ms": 12,
                "jitter_ms": 3,
                "sent": 10,
                "received": 10,
                "src_ip": "192.0.2.10",
                "target_ip": "223.5.5.5",
                "note": "",
            }
        )

    with patch.object(asyncio, "create_subprocess_exec", create_process):
        result = asyncio.run(diagnostic_runner.run_diagnostic("192.0.2.10"))

    assert result["status"] == "available"
    command = calls[0][0]
    assert command[:4] == (
        str(engine.resolve()),
        "diagnose",
        "--src-ip",
        "192.0.2.10",
    )
    assert "--target-ip" in command
    assert calls[0][1]["stdout"] is asyncio.subprocess.PIPE


def test_run_diagnostic_missing_engine_is_graceful(monkeypatch):
    monkeypatch.setattr(diagnostic_runner, "get_engine_path", lambda: None)

    result = asyncio.run(diagnostic_runner.run_diagnostic("192.0.2.10"))

    assert result["status"] == "unavailable"
    assert result["note"] == "hypomux-engine.exe not found"


@unittest.skipUnless(
    os.environ.get("HYPOMUX_ENGINE_TEST_EXE"),
    "set HYPOMUX_ENGINE_TEST_EXE to run the real Go diagnostic integration",
)
def test_real_go_diagnostic_cli_preserves_result_contract(monkeypatch):
    monkeypatch.setenv(
        diagnostic_runner.ENGINE_PATH_ENV,
        os.environ["HYPOMUX_ENGINE_TEST_EXE"],
    )

    result = asyncio.run(diagnostic_runner.run_diagnostic("invalid"))

    assert result == {
        "status": "unavailable",
        "loss_rate": 100,
        "avg_latency_ms": 0,
        "jitter_ms": 0,
        "sent": 0,
        "received": 0,
        "src_ip": "invalid",
        "target_ip": diagnostic_runner.DEFAULT_TARGET_IP,
        "note": "invalid --src-ip",
    }
