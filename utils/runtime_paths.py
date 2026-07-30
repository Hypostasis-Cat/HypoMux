"""Runtime asset discovery shared by managed and compatibility backends."""

from __future__ import annotations

import os
import sys
from pathlib import Path


def runtime_root() -> Path:
    if getattr(sys, "frozen", False) or ("__compiled__" in globals()):
        return Path(sys.executable or sys.argv[0]).resolve().parent
    return Path(__file__).resolve().parents[1]


def get_singbox_path() -> str | None:
    candidate = runtime_root() / "bin" / "sing-box.exe"
    return str(candidate) if candidate.is_file() else None
