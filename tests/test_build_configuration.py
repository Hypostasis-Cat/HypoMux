from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]


def _read(relative_path: str) -> str:
    return (ROOT / relative_path).read_text(encoding="utf-8")


def test_build_workflow_keeps_fast_validation_and_incremental_cache():
    workflow = _read(".github/workflows/build.yml")

    assert "python -m pytest -q" in workflow
    assert "python -m compileall -q" in workflow
    assert "NUITKA_CACHE_DIR:" in workflow
    assert "Restore Nuitka compilation cache" in workflow
    assert "--clean-cache" not in workflow
    assert "--report=dist\\nuitka-compilation-report.xml" in workflow


def test_build_workflow_uses_reproducible_toolchain_inputs():
    workflow = _read(".github/workflows/build.yml")

    assert "python-version: '3.13.14'" in workflow
    assert "-r requirements.txt -r requirements-build.txt" in workflow
    assert "matrix.python-version" not in workflow

    runtime_requirements = _read("requirements.txt").splitlines()
    build_requirements = _read("requirements-build.txt").splitlines()
    assert runtime_requirements
    assert build_requirements
    assert all("==" in line for line in runtime_requirements if line.strip())
    assert all("==" in line for line in build_requirements if line.strip())


def test_installer_rejects_non_x64_compatible_systems():
    setup_script = _read("setup.iss")

    assert "ArchitecturesAllowed=x64compatible" in setup_script
    assert "ArchitecturesInstallIn64BitMode=x64compatible" in setup_script
