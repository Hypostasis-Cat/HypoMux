from __future__ import annotations

import ast
from pathlib import Path
import tempfile
import unittest
from unittest import mock

from engine_client.capabilities import (
    PROXY_MODE,
    PROXY_REQUIRED_FEATURES,
    PROXY_REQUIRED_METHODS,
    TUN_MODE,
    TUN_REQUIRED_FEATURES,
    TUN_REQUIRED_METHODS,
    missing_mode_requirements,
    supports_mode_contract,
)
from engine_client.qualification import run_read_only_qualification
from engine_client.tun_contract import (
    DNS_MODE_DOH_STRICT,
    PORT_AGGREGATION,
    PORT_ETHERNET,
    PORT_WIFI,
    classify_nics,
)
from utils.runtime_paths import get_singbox_path


def _complete_hello() -> dict:
    return {
        "engine": "hypomux-engine",
        "protocol_version": 1,
        "pid": 4321,
        "elevated": True,
        "capabilities": sorted(
            {
                *PROXY_REQUIRED_METHODS,
                *TUN_REQUIRED_METHODS,
                "engine.hello",
                "engine.status",
                "health.check",
                "host.shutdown",
            }
        ),
        "modes": [PROXY_MODE, TUN_MODE],
        "mode_features": {
            PROXY_MODE: list(PROXY_REQUIRED_FEATURES),
            TUN_MODE: list(TUN_REQUIRED_FEATURES),
        },
    }


class _FakeClient:
    hello = _complete_hello()

    def __init__(self, command):
        self.command = command

    def start(self):
        return dict(self.hello)

    def request(self, method, params=None, *, timeout=None):
        if method == "engine.status":
            return {"engine": {"state": "stopped"}}
        if method == "health.check":
            return {"ok": True, "state": "stopped"}
        if method == "tun.status":
            return {"state": "stopped"}
        raise AssertionError(method)

    def stop(self, *, graceful=True):
        return "exited"


def _clean_snapshot() -> dict:
    return {
        "supported": True,
        "routes": [],
        "devices": [],
        "processes": [],
    }


class QualificationContractTests(unittest.TestCase):
    def setUp(self):
        identity = mock.patch(
            "engine_client.qualification.inspect_executable_identity",
            return_value={"size": 1024, "sha256": "a" * 64},
        )
        identity.start()
        self.addCleanup(identity.stop)

    def test_complete_contract_supports_both_modes(self):
        hello = _complete_hello()
        self.assertTrue(supports_mode_contract(hello, PROXY_MODE))
        self.assertTrue(supports_mode_contract(hello, TUN_MODE))
        self.assertEqual(
            missing_mode_requirements(hello, TUN_MODE),
            {"modes": [], "methods": [], "features": []},
        )

    def test_missing_feature_is_reported_exactly(self):
        hello = _complete_hello()
        hello["mode_features"][TUN_MODE].remove("managed_tun_lifecycle")
        missing = missing_mode_requirements(hello, TUN_MODE)
        self.assertEqual(missing["features"], ["managed_tun_lifecycle"])
        self.assertFalse(supports_mode_contract(hello, TUN_MODE))

    def test_read_only_qualification_passes_complete_clean_host(self):
        report = run_read_only_qualification(
            ["C:\\HypoMux\\bin\\hypomux-engine.exe"],
            require_elevated=True,
            require_signed=True,
            snapshotter=_clean_snapshot,
            signature_inspector=lambda _path: {
                "status": "Valid",
                "status_message": "",
            },
            client_factory=_FakeClient,
        )
        self.assertTrue(report["passed"])
        self.assertFalse(report["network_modes_started"])
        self.assertEqual(
            report["engine"]["executable"],
            str(Path("C:\\HypoMux\\bin\\hypomux-engine.exe").resolve()),
        )

    def test_postflight_residue_fails_the_report(self):
        snapshots = iter(
            [
                _clean_snapshot(),
                {
                    "supported": True,
                    "routes": [
                        {
                            "InterfaceAlias": "HypoMux-Tun",
                            "DestinationPrefix": "::/0",
                        }
                    ],
                    "devices": [],
                    "processes": [],
                },
            ]
        )
        report = run_read_only_qualification(
            "hypomux-engine.exe",
            snapshotter=lambda: next(snapshots),
            signature_inspector=lambda _path: {"status": "NotSigned"},
            client_factory=_FakeClient,
        )
        self.assertFalse(report["passed"])
        check = next(
            item
            for item in report["checks"]
            if item["name"] == "no_postflight_hypomux_tun_residue"
        )
        self.assertEqual(check["detail"]["routes"], 1)

    def test_release_requirements_promote_signature_to_hard_gate(self):
        report = run_read_only_qualification(
            "hypomux-engine.exe",
            require_signed=True,
            snapshotter=_clean_snapshot,
            signature_inspector=lambda _path: {
                "status": "NotSigned",
                "status_message": "synthetic unsigned build",
            },
            client_factory=_FakeClient,
        )
        self.assertFalse(report["passed"])
        check = next(
            item
            for item in report["checks"]
            if item["name"] == "engine_authenticode_valid"
        )
        self.assertTrue(check["required"])
        self.assertFalse(check["passed"])

    def test_owned_singbox_process_fails_postflight(self):
        snapshots = iter(
            [
                _clean_snapshot(),
                {
                    "supported": True,
                    "routes": [],
                    "devices": [],
                    "processes": [
                        {
                            "ProcessId": 9876,
                            "Name": "sing-box.exe",
                            "ExecutablePath": (
                                "C:\\HypoMux\\bin\\sing-box.exe"
                            ),
                        }
                    ],
                },
            ]
        )
        report = run_read_only_qualification(
            "C:\\HypoMux\\bin\\hypomux-engine.exe",
            snapshotter=lambda: next(snapshots),
            signature_inspector=lambda _path: {"status": "NotSigned"},
            client_factory=_FakeClient,
        )
        self.assertFalse(report["passed"])
        check = next(
            item
            for item in report["checks"]
            if item["name"] == "no_postflight_hypomux_owned_processes"
        )
        self.assertEqual(check["detail"][0]["ProcessId"], 9876)


class SharedTunContractTests(unittest.TestCase):
    def test_channel_constants_and_adapter_grouping_are_toolkit_independent(self):
        self.assertEqual(
            (PORT_ETHERNET, PORT_WIFI, PORT_AGGREGATION),
            (2001, 2002, 2003),
        )
        self.assertEqual(DNS_MODE_DOH_STRICT, "doh_strict")
        ethernet = {"name": "Ethernet", "iftype": 6}
        wifi = {"name": "Wi-Fi", "iftype": 71}
        unknown = {"name": "Adapter", "iftype": 0}
        wired, wireless = classify_nics([ethernet, wifi, unknown])
        self.assertEqual(wired, [ethernet, unknown])
        self.assertEqual(wireless, [wifi])

    def test_singbox_runtime_path_no_longer_imports_tun_manager(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            executable = root / "bin" / "sing-box.exe"
            executable.parent.mkdir()
            executable.write_bytes(b"test")
            with mock.patch(
                "utils.runtime_paths.runtime_root",
                return_value=root,
            ):
                self.assertEqual(get_singbox_path(), str(executable))

    def test_default_go_adapters_do_not_directly_import_legacy_runtime_classes(self):
        root = Path(__file__).resolve().parents[1]
        forbidden = {
            "ui/go_proxy_worker.py": {"proxy_worker"},
            "ui/go_tun_pool_worker.py": {"proxy_worker"},
            "ui/go_tun_manager.py": {"utils.tun_manager"},
        }
        for relative_path, forbidden_modules in forbidden.items():
            with self.subTest(path=relative_path):
                tree = ast.parse(
                    (root / relative_path).read_text(encoding="utf-8")
                )
                imported = set()
                for node in ast.walk(tree):
                    if isinstance(node, ast.Import):
                        imported.update(alias.name for alias in node.names)
                    elif isinstance(node, ast.ImportFrom) and node.module:
                        imported.add(node.module)
                self.assertTrue(forbidden_modules.isdisjoint(imported))


if __name__ == "__main__":
    unittest.main()
