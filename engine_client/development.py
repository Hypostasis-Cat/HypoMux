"""Network-backend selection without coupling it to Qt or user configuration."""

from __future__ import annotations

import os
from pathlib import Path
from typing import Mapping


DEVELOPMENT_FLAG = "HYPOMUX_GO_ENGINE_DEV"
PROXY_DEVELOPMENT_FLAG = "HYPOMUX_GO_PROXY_DEV"
TUN_DEVELOPMENT_FLAG = "HYPOMUX_GO_TUN_DEV"
ENGINE_PATH_VARIABLE = "HYPOMUX_ENGINE_PATH"
NETWORK_BACKEND_VARIABLE = "HYPOMUX_NETWORK_BACKEND"

BACKEND_AUTO = "auto"
BACKEND_GO = "go"
BACKEND_PYTHON = "python"


def network_backend(environment: Mapping[str, str] | None = None) -> str:
    """Return ``auto``, ``go``, or ``python`` for the current process.

    ``auto`` is the production default: start the packaged Go host when it is
    available and retain Python only as a pre-acquisition compatibility
    fallback. ``go`` makes a missing/incomplete host a hard selection failure,
    while ``python`` is the explicit emergency rollback.

    The old development flags remain accepted by their compatibility helpers.
    With Go now enabled by default, they are equivalent to ``auto`` and never
    turn an older per-mode script into strict global selection.
    """

    env = os.environ if environment is None else environment
    configured = str(env.get(NETWORK_BACKEND_VARIABLE, "")).strip().lower()
    aliases = {
        "": BACKEND_AUTO,
        "auto": BACKEND_AUTO,
        "default": BACKEND_AUTO,
        "go": BACKEND_GO,
        "engine": BACKEND_GO,
        "python": BACKEND_PYTHON,
        "legacy": BACKEND_PYTHON,
    }
    return aliases.get(configured, BACKEND_AUTO)


def engine_host_enabled(environment: Mapping[str, str] | None = None) -> bool:
    return network_backend(environment) != BACKEND_PYTHON


def go_proxy_enabled(environment: Mapping[str, str] | None = None) -> bool:
    return engine_host_enabled(environment)


def go_tun_enabled(environment: Mapping[str, str] | None = None) -> bool:
    return engine_host_enabled(environment)


def go_backend_required(environment: Mapping[str, str] | None = None) -> bool:
    return network_backend(environment) == BACKEND_GO


def select_go_backend(
    capable: bool,
    component: str,
    environment: Mapping[str, str] | None = None,
) -> bool:
    """Resolve one capability-gated session before it acquires resources."""

    if not engine_host_enabled(environment):
        return False
    if capable:
        return True
    if go_backend_required(environment):
        raise RuntimeError(
            "HYPOMUX_NETWORK_BACKEND=go requires a connected engine with "
            f"the complete {component} capability set"
        )
    return False


def development_engine_enabled(environment: Mapping[str, str] | None = None) -> bool:
    env = os.environ if environment is None else environment
    return _enabled(env.get(DEVELOPMENT_FLAG)) or _enabled(
        env.get(PROXY_DEVELOPMENT_FLAG)
    ) or _enabled(env.get(TUN_DEVELOPMENT_FLAG))


def go_proxy_development_enabled(
    environment: Mapping[str, str] | None = None,
) -> bool:
    env = os.environ if environment is None else environment
    return _enabled(env.get(PROXY_DEVELOPMENT_FLAG))


def go_tun_development_enabled(
    environment: Mapping[str, str] | None = None,
) -> bool:
    env = os.environ if environment is None else environment
    return _enabled(env.get(TUN_DEVELOPMENT_FLAG))


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
    """Compatibility alias retained for migration-era scripts."""

    return resolve_engine_command(runtime_dir, environment)


def resolve_engine_command(
    runtime_dir: str | os.PathLike[str],
    environment: Mapping[str, str] | None = None,
) -> list[str] | None:
    """Resolve the packaged or source-built Go host.

    Installed builds place the signed engine in ``bin``. The remaining
    locations keep source checkouts and earlier development scripts working.
    """

    env = os.environ if environment is None else environment
    configured = str(env.get(ENGINE_PATH_VARIABLE, "")).strip().strip('"')
    if configured:
        path = Path(configured).expanduser()
        return [str(path.resolve())] if path.is_file() else None

    root = Path(runtime_dir).resolve()
    candidates = (
        root / "bin" / "hypomux-engine.exe",
        root / "hypomux-engine.exe",
        root / "dist" / "hypomux-engine.exe",
        root / "engine" / "hypomux-engine.exe",
    )
    for candidate in candidates:
        if candidate.is_file():
            return [str(candidate)]
    return None
