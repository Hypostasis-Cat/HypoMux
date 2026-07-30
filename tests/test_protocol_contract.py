from __future__ import annotations

import json
from pathlib import Path
import unittest

from engine_client import EngineRemoteError
from engine_client.protocol import (
    MAX_MESSAGE_BYTES,
    PROTOCOL_VERSION,
    decode_message,
    encode_request,
    response_value,
    validate_event,
)


CONTRACT_ROOT = Path(__file__).resolve().parents[1] / "protocol" / "v1"


class SharedProtocolContractTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.manifest = json.loads(
            (CONTRACT_ROOT / "manifest.json").read_text(encoding="utf-8")
        )
        cls.fixtures = json.loads(
            (CONTRACT_ROOT / "fixtures" / "messages.json").read_text(
                encoding="utf-8"
            )
        )

    def test_manifest_matches_python_protocol_constants(self):
        self.assertEqual(self.manifest["protocol"], PROTOCOL_VERSION)
        self.assertEqual(
            self.manifest["max_message_bytes"],
            MAX_MESSAGE_BYTES,
        )
        self.assertEqual(self.manifest["transport"], "stdio-jsonl")

        methods = self.manifest["methods"]
        self.assertEqual(len({item["name"] for item in methods}), len(methods))
        for item in methods:
            self.assertTrue(item["idempotency"])
            self.assertTrue(item["privilege"])
            self.assertTrue(item["cancellation"])

        events = self.manifest["events"]
        self.assertEqual(
            [item["name"] for item in events],
            ["engine.state_changed", "host.exiting"],
        )
        self.assertTrue(self.manifest["error_codes"])

    def test_canonical_messages_use_the_production_python_codec(self):
        advertised = {item["name"] for item in self.manifest["methods"]}
        request_methods: set[str] = set()
        response_methods: set[str] = set()
        event_names: set[str] = set()
        previous_sequence = 0

        for fixture in self.fixtures:
            with self.subTest(fixture=fixture["name"]):
                message = fixture["message"]
                payload = json.dumps(
                    message,
                    ensure_ascii=False,
                    separators=(",", ":"),
                ).encode("utf-8")
                decoded = decode_message(payload)
                self.assertEqual(decoded, message)

                kind = fixture["kind"]
                if kind == "request":
                    method = fixture["method"]
                    encoded = encode_request(
                        message["id"],
                        method,
                        message.get("params"),
                    )
                    self.assertEqual(json.loads(encoded), message)
                    request_methods.add(method)
                elif kind == "response":
                    self.assertIsNotNone(response_value(decoded))
                    response_methods.add(fixture["method"])
                elif kind == "error":
                    with self.assertRaises(EngineRemoteError) as raised:
                        response_value(decoded)
                    self.assertEqual(
                        raised.exception.code,
                        message["error"]["code"],
                    )
                elif kind == "event":
                    previous_sequence = validate_event(
                        decoded,
                        previous_sequence,
                    )
                    event_names.add(message["event"])
                else:
                    self.fail(f"unknown fixture kind {kind!r}")

        self.assertEqual(request_methods, advertised)
        self.assertEqual(response_methods, advertised)
        self.assertEqual(
            event_names,
            {"engine.state_changed", "host.exiting"},
        )


if __name__ == "__main__":
    unittest.main()
