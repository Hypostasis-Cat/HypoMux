from __future__ import annotations

import json
from pathlib import Path
import tempfile
import unittest
from unittest import mock

from engine_client.capabilities import PROXY_MODE, TUN_MODE
from engine_client.qualification import inspect_executable_identity
from engine_client.qualification_session import (
    POSTFLIGHT_CHECKS,
    SCENARIOS,
    prepare_session,
    record_scenario,
    session_summary,
    validate_postflight,
)


def _passing_report(engine: Path) -> dict:
    identity = inspect_executable_identity(str(engine))
    return {
        "schema_version": 1,
        "kind": "hypomux.strict_go.read_only",
        "passed": True,
        "network_modes_started": False,
        "engine": {
            "executable": str(engine),
            **identity,
            "hello": {
                "engine_version": "qualification-test",
                "commit": "abc123",
            },
        },
        "checks": [
            {
                "name": name,
                "passed": True,
                "required": True,
                "detail": {},
            }
            for name in (
                *POSTFLIGHT_CHECKS,
                "engine_authenticode_valid",
                "engine_signature_policy",
                "engine_elevated",
            )
        ],
    }


class QualificationSessionTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.engine = self.root / "hypomux-engine.exe"
        self.engine.write_bytes(b"exact qualification candidate")
        self.report = _passing_report(self.engine)

    def _prepare(self, *, development: bool = False) -> Path:
        with mock.patch(
            "engine_client.qualification_session.run_read_only_qualification",
            return_value=self.report,
        ):
            path, _session = prepare_session(
                self.engine,
                self.root / "qualification",
                require_elevated=not development,
                require_signed=not development,
            )
        return path

    def test_prepare_binds_session_to_exact_candidate(self):
        path = self._prepare()
        session = json.loads(path.read_text(encoding="utf-8"))
        identity = inspect_executable_identity(str(self.engine))
        self.assertEqual(session["candidate"]["sha256"], identity["sha256"])
        self.assertEqual(session["candidate"]["engine_version"], "qualification-test")
        self.assertEqual(len(session["scenarios"]), len(SCENARIOS))
        self.assertFalse(session["verdict"]["python_removal_ready"])

    def test_postflight_uses_canonical_mode_contract_names(self):
        self.assertIn(f"{PROXY_MODE}_strict_contract", POSTFLIGHT_CHECKS)
        self.assertIn(f"{TUN_MODE}_strict_contract", POSTFLIGHT_CHECKS)

    def test_all_formal_scenarios_with_clean_evidence_open_removal_gate(self):
        path = self._prepare()
        postflight = path.parent / "postflight.json"
        postflight.write_text(json.dumps(self.report), encoding="utf-8")
        session = None
        for scenario_id, _title in SCENARIOS:
            session = record_scenario(
                path,
                scenario_id,
                "passed",
                evidence=[f"verified {scenario_id}"],
                postflight_path=postflight,
            )
        assert session is not None
        summary = session_summary(session)
        self.assertTrue(summary["verdict"]["python_removal_ready"])
        self.assertEqual(summary["counts"]["passed"], len(SCENARIOS))

    def test_pass_requires_evidence_and_exact_candidate_postflight(self):
        path = self._prepare()
        postflight = path.parent / "postflight.json"
        postflight.write_text(json.dumps(self.report), encoding="utf-8")
        with self.assertRaisesRegex(ValueError, "evidence"):
            record_scenario(
                path,
                "proxy",
                "passed",
                postflight_path=postflight,
            )

        mismatched = dict(self.report)
        mismatched["engine"] = dict(mismatched["engine"])
        mismatched["engine"]["sha256"] = "0" * 64
        self.assertIn(
            "postflight engine SHA-256 does not match the candidate",
            validate_postflight(
                mismatched,
                {
                    "sha256": self.report["engine"]["sha256"],
                    "engine_version": "qualification-test",
                    "commit": "abc123",
                },
            ),
        )

    def test_formal_postflight_cannot_import_optional_security_checks(self):
        report = _passing_report(self.engine)
        for check in report["checks"]:
            if check["name"] in {
                "engine_signature_policy",
                "engine_elevated",
            }:
                check["passed"] = False
                check["required"] = False
        errors = validate_postflight(
            report,
            {
                "sha256": report["engine"]["sha256"],
                "engine_version": "qualification-test",
                "commit": "abc123",
            },
            {"require_signed": True, "require_elevated": True},
        )
        self.assertIn(
            "postflight check failed: engine_signature_policy",
            errors,
        )
        self.assertIn("postflight check failed: engine_elevated", errors)

    def test_development_session_can_never_authorize_python_removal(self):
        path = self._prepare(development=True)
        postflight = path.parent / "postflight.json"
        postflight.write_text(json.dumps(self.report), encoding="utf-8")
        session = None
        for scenario_id, _title in SCENARIOS:
            session = record_scenario(
                path,
                scenario_id,
                "passed",
                evidence=["development evidence"],
                postflight_path=postflight,
            )
        assert session is not None
        verdict = session["verdict"]
        self.assertFalse(verdict["python_removal_ready"])
        self.assertIn(
            "session did not require an elevated engine",
            verdict["reasons"],
        )
        self.assertIn(
            "session did not require a signed engine",
            verdict["reasons"],
        )


if __name__ == "__main__":
    unittest.main()
