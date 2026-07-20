"""GitHub Release update discovery, download and hand-off helpers.

The application deliberately uses no third-party HTTP dependency here.  All
update metadata and installers come from HypoMux's public GitHub repository.
"""

from __future__ import annotations

import hashlib
import json
import os
import re
import subprocess
import tempfile
from dataclasses import dataclass
from pathlib import Path
from typing import Callable
from urllib.error import HTTPError, URLError
from urllib.parse import urlparse
from urllib.request import Request, urlopen


REPOSITORY = "Hypostasis-Cat/HypoMux"
LATEST_RELEASE_API = f"https://api.github.com/repos/{REPOSITORY}/releases/latest"
USER_AGENT = "HypoMux-Updater"
_INSTALLER_NAME = re.compile(
    r"^HypoMux_Setup_[A-Za-z0-9][A-Za-z0-9._+-]*\.exe$", re.IGNORECASE
)
_VERSION = re.compile(r"^v?(\d+(?:\.\d+){1,3})(?:[-+].*)?$", re.IGNORECASE)


class UpdateError(RuntimeError):
    """An update check or download could not be completed safely."""


@dataclass(frozen=True)
class ReleaseInfo:
    tag_name: str
    name: str
    notes: str
    page_url: str
    installer_url: str
    installer_name: str
    installer_size: int
    installer_digest: str = ""


def version_key(value: str) -> tuple[int, int, int, int] | None:
    """Turn a v-prefixed semantic version into a comparable, fixed-size key."""
    match = _VERSION.match((value or "").strip())
    if not match:
        return None
    parts = [int(item) for item in match.group(1).split(".")]
    return tuple((parts + [0, 0, 0, 0])[:4])  # type: ignore[return-value]


def is_newer_version(candidate: str, current: str) -> bool:
    candidate_key = version_key(candidate)
    current_key = version_key(current)
    if candidate_key is None:
        return False
    # GitHub Actions 对 main 分支构建使用 dev-<commit> 版本。它不是正式
    # 语义化版本，但应当可以升级到任意有效的正式 Release，既符合测试包
    # 预期，也让更新流程可在发布前验证。
    if (current or "").strip().lower().startswith("dev-"):
        return True
    return bool(current_key and candidate_key > current_key)


def _request(url: str) -> Request:
    return Request(
        url,
        headers={
            "Accept": "application/vnd.github+json",
            "User-Agent": USER_AGENT,
            "X-GitHub-Api-Version": "2022-11-28",
        },
    )


def _read_json(url: str, timeout: float) -> dict:
    try:
        with urlopen(_request(url), timeout=timeout) as response:
            payload = response.read()
    except HTTPError as exc:
        raise UpdateError(f"GitHub HTTP {exc.code}") from exc
    except URLError as exc:
        raise UpdateError("无法连接 GitHub，请检查网络后重试") from exc
    except OSError as exc:
        raise UpdateError(str(exc)) from exc

    try:
        data = json.loads(payload.decode("utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise UpdateError("GitHub 返回了无效的更新信息") from exc
    if not isinstance(data, dict):
        raise UpdateError("GitHub 返回了无效的更新信息")
    return data


def _github_url(value: object, *, field: str) -> str:
    url = str(value or "").strip()
    parsed = urlparse(url)
    if parsed.scheme != "https" or parsed.netloc.lower() != "github.com":
        raise UpdateError(f"GitHub 发布信息中的 {field} 无效")
    return url


def fetch_latest_release(timeout: float = 8.0) -> ReleaseInfo:
    """Fetch the latest stable release and its HypoMux installer asset."""
    data = _read_json(LATEST_RELEASE_API, timeout)
    tag_name = str(data.get("tag_name") or "").strip()
    if version_key(tag_name) is None:
        raise UpdateError("GitHub 最新发布的版本号无效")

    page_url = _github_url(data.get("html_url"), field="发布页地址")
    assets = data.get("assets")
    if not isinstance(assets, list):
        raise UpdateError("GitHub 最新发布未包含安装包")

    for asset in assets:
        if not isinstance(asset, dict):
            continue
        name = str(asset.get("name") or "")
        if not _INSTALLER_NAME.fullmatch(name):
            continue
        url = _github_url(asset.get("browser_download_url"), field="安装包地址")
        size = asset.get("size")
        if not isinstance(size, int) or size <= 0:
            raise UpdateError("GitHub 安装包大小无效")
        return ReleaseInfo(
            tag_name=tag_name,
            name=str(data.get("name") or tag_name),
            notes=str(data.get("body") or "").strip(),
            page_url=page_url,
            installer_url=url,
            installer_name=name,
            installer_size=size,
            installer_digest=str(asset.get("digest") or "").strip(),
        )
    raise UpdateError("GitHub 最新发布未找到 HypoMux 安装包")


def download_installer(
    release: ReleaseInfo,
    progress: Callable[[int, int], None] | None = None,
    timeout: float = 30.0,
) -> str:
    """Download one selected GitHub installer into an isolated temp directory."""
    target_dir = Path(tempfile.mkdtemp(prefix="HypoMuxUpdate-"))
    target = target_dir / release.installer_name
    partial = target.with_suffix(target.suffix + ".part")
    downloaded = 0
    digest = hashlib.sha256()

    try:
        with urlopen(_request(release.installer_url), timeout=timeout) as response:
            with open(partial, "wb") as stream:
                while True:
                    chunk = response.read(1024 * 1024)
                    if not chunk:
                        break
                    stream.write(chunk)
                    digest.update(chunk)
                    downloaded += len(chunk)
                    if progress is not None:
                        progress(downloaded, release.installer_size)

        if downloaded != release.installer_size:
            raise UpdateError(
                f"安装包大小校验失败（预期 {release.installer_size}，实际 {downloaded}）"
            )
        expected_digest = release.installer_digest.lower()
        if expected_digest.startswith("sha256:"):
            if digest.hexdigest().lower() != expected_digest.removeprefix("sha256:"):
                raise UpdateError("安装包 SHA-256 校验失败")
        os.replace(partial, target)
        return str(target)
    except UpdateError:
        raise
    except HTTPError as exc:
        raise UpdateError(f"GitHub 下载失败（HTTP {exc.code}）") from exc
    except URLError as exc:
        raise UpdateError("无法从 GitHub 下载安装包，请检查网络后重试") from exc
    except OSError as exc:
        raise UpdateError(str(exc)) from exc
    finally:
        try:
            partial.unlink(missing_ok=True)
        except OSError:
            pass


def launch_installer_after_exit(installer_path: str, process_id: int) -> None:
    """Start Inno Setup only after this process has left its install directory."""
    installer = Path(installer_path)
    if not installer.is_file() or installer.suffix.lower() != ".exe":
        raise UpdateError("下载的安装包不存在")

    launcher_path = installer.parent / "run-update.cmd"
    # The installer path is generated by this module from a controlled Release
    # asset name.  A detached cmd file lets Inno Setup replace the live exe
    # after HypoMux has completed its normal network cleanup.
    launcher_path.write_text(
        "@echo off\r\n"
        f"set \"target_pid={int(process_id)}\"\r\n"
        ":wait_for_hypomux\r\n"
        "tasklist /FI \"PID eq %target_pid%\" /NH | find \"%target_pid%\" >nul\r\n"
        "if not errorlevel 1 (\r\n"
        "  timeout /t 1 /nobreak >nul\r\n"
        "  goto wait_for_hypomux\r\n"
        ")\r\n"
        f"start \"\" /wait \"{installer}\" /VERYSILENT /SUPPRESSMSGBOXES /NORESTART\r\n"
        f"del /q \"{installer}\"\r\n"
        "del \"%~f0\"\r\n",
        encoding="utf-8",
    )
    subprocess.Popen(
        [os.environ.get("COMSPEC", r"C:\\Windows\\System32\\cmd.exe"), "/d", "/c", str(launcher_path)],
        creationflags=getattr(subprocess, "CREATE_NO_WINDOW", 0x08000000)
        | getattr(subprocess, "DETACHED_PROCESS", 0x00000008),
        close_fds=True,
    )
