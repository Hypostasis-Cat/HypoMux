"""Hidden development-mode selection without touching user configuration."""

from __future__ import annotations

import os
from pathlib import Path
from typing import Mapping


DEVELOPMENT_FLAG = "HYPOMUX_GO_ENGINE_DEV"
PROXY_DEVELOPMENT_FLAG = "HYPOMUX_GO_PROXY_DEV"
ENGINE_PATH_VARIABLE = "HYPOMUX_ENGINE_PATH"


def development_engine_enabled(environment: Mapping[str, str] | None = None) -> bool:
    env = os.environ if environment is None else environment
    return _enabled(env.get(DEVELOPMENT_FLAG)) or _enabled(
        env.get(PROXY_DEVELOPMENT_FLAG)
    )


def go_proxy_development_enabled(
    environment: Mapping[str, str] | None = None,
) -> bool:
    env = os.environ if environment is None else environment
    return _enabled(env.get(PROXY_DEVELOPMENT_FLAG))


def _enabled(value: object) -> bool:
    return str(value or "").strip().lower() in {
        "1",
        "true",
        "yes",
        "on",
    }


def resolve_development_engine_command(
    runtime_dir: str | os.PathLike[str],
    environment: Mapping[str, str] | None = None,
) -> list[str] | None:
    env = os.environ if environment is None else environment
    configured = str(env.get(ENGINE_PATH_VARIABLE, "")).strip().strip('"')
    if configured:
        path = Path(configured).expanduser()
        return [str(path.resolve())] if path.is_file() else None

    root = Path(runtime_dir).resolve()
    candidates = (
        root / "hypomux-engine.exe",
        root / "dist" / "hypomux-engine.exe",
        root / "engine" / "hypomux-engine.exe",
    )
    for candidate in candidates:
        if candidate.is_file():
            return [str(candidate)]
    return None
