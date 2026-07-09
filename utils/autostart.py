"""
HypoMux startup task management.

The installer and the in-app switch both manage the same scheduled task so
startup state stays consistent even for an administrator-level installation.
"""

from __future__ import annotations

import logging
import os
import subprocess
import sys
from pathlib import Path

logger = logging.getLogger(__name__)

TASK_NAME = "HypoMuxAutoStart"
SILENT_FLAG = "--silent"


def get_executable_path() -> str:
    """Return the executable command used by the scheduled task."""
    is_frozen = getattr(sys, "frozen", False) or ("__compiled__" in globals())
    if is_frozen:
        # Nuitka onefile 会把自身解压到 %TEMP%\onefile_<pid>_<random>\ 然后运行，
        # sys.executable 指向临时目录里的 python.exe，退出后即被删除。
        # 必须用 sys.argv[0] 获取用户实际启动的原始 EXE 路径。
        return os.path.abspath(sys.argv[0])

    script_path = Path(__file__).resolve().parents[1] / "main.py"
    return f'"{os.path.abspath(sys.executable)}" "{script_path}"'


def build_run_command() -> str:
    """Build the command line stored in Windows Task Scheduler."""
    exe = get_executable_path()
    is_frozen = getattr(sys, "frozen", False) or ("__compiled__" in globals())
    if is_frozen:
        return f'"{exe}" {SILENT_FLAG}'
    return f"{exe} {SILENT_FLAG}"


def _run_schtasks(args: list[str]) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        ["schtasks", *args],
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
        timeout=15,
        creationflags=subprocess.CREATE_NO_WINDOW if os.name == "nt" else 0,
    )


def enable_autostart() -> bool:
    """Create or update the highest-privilege logon scheduled task."""
    command = build_run_command()
    result = _run_schtasks(
        [
            "/Create",
            "/TN",
            TASK_NAME,
            "/TR",
            command,
            "/SC",
            "ONLOGON",
            "/RL",
            "HIGHEST",
            "/F",
        ]
    )
    if result.returncode == 0:
        logger.info("Autostart task enabled: %s", command)
        return True

    logger.warning("Failed to enable autostart task: %s", result.stderr.strip())
    return False


def disable_autostart() -> bool:
    """Delete the scheduled task. Missing tasks are treated as disabled."""
    result = _run_schtasks(["/Delete", "/TN", TASK_NAME, "/F"])
    if result.returncode == 0:
        logger.info("Autostart task disabled")
        return True

    output = f"{result.stdout}\n{result.stderr}".lower()
    if "cannot find" in output or "not found" in output or "找不到" in output:
        logger.info("Autostart task was already absent")
        return True

    logger.warning("Failed to disable autostart task: %s", result.stderr.strip())
    return False


def is_autostart_enabled() -> bool:
    """Return whether the shared scheduled task currently exists."""
    result = _run_schtasks(["/Query", "/TN", TASK_NAME])
    return result.returncode == 0


def set_autostart(enabled: bool) -> bool:
    """Set startup state for the UI switch."""
    return enable_autostart() if enabled else disable_autostart()
