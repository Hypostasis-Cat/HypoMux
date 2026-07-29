"""Process helpers kept separate from protocol and UI concerns."""

from __future__ import annotations

import os
from pathlib import Path
import subprocess
from typing import Mapping, Sequence

from .exceptions import EngineProcessError


EngineCommand = str | os.PathLike[str] | Sequence[str | os.PathLike[str]]


def normalize_command(command: EngineCommand) -> list[str]:
    if isinstance(command, (str, os.PathLike)):
        normalized = [os.fspath(command)]
    else:
        normalized = [os.fspath(part) for part in command]
    if not normalized or not normalized[0].strip():
        raise EngineProcessError("engine command is empty")

    executable = Path(normalized[0])
    if executable.is_absolute() and not executable.is_file():
        raise EngineProcessError(f"engine executable does not exist: {executable}")
    return normalized


def start_engine_process(
    command: EngineCommand,
    *,
    environment: Mapping[str, str] | None = None,
) -> subprocess.Popen[bytes]:
    normalized = normalize_command(command)
    startupinfo = None
    if hasattr(subprocess, "STARTUPINFO"):
        startupinfo = subprocess.STARTUPINFO()
        startupinfo.dwFlags |= subprocess.STARTF_USESHOWWINDOW
        startupinfo.wShowWindow = 0

    child_environment = os.environ.copy()
    if environment:
        child_environment.update({str(key): str(value) for key, value in environment.items()})

    try:
        return subprocess.Popen(
            [*normalized, "serve"],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            cwd=str(Path(normalized[0]).resolve().parent),
            env=child_environment,
            startupinfo=startupinfo,
            creationflags=getattr(subprocess, "CREATE_NO_WINDOW", 0),
            bufsize=0,
        )
    except OSError as exc:
        raise EngineProcessError(f"could not start engine: {exc}") from exc


def stop_process(process: subprocess.Popen[bytes], timeout: float) -> str:
    """Stop a stubborn host and return the strongest action that was required."""
    if process.poll() is not None:
        return "exited"
    try:
        process.terminate()
        process.wait(timeout=max(timeout, 0.05))
        return "terminated"
    except subprocess.TimeoutExpired:
        process.kill()
        try:
            process.wait(timeout=max(timeout, 0.05))
        except subprocess.TimeoutExpired as exc:
            raise EngineProcessError("engine process could not be killed") from exc
        return "killed"
