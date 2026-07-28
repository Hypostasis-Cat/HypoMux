import tempfile
import unittest
from pathlib import Path

from utils.acceleration_log import (
    AccelerationLogStore,
    MAX_LOG_BYTES,
    TRUNCATION_MARKER,
)


class AccelerationLogTests(unittest.TestCase):
    def test_default_log_has_a_hard_size_limit(self):
        store = AccelerationLogStore(Path("unused.log"))
        self.assertEqual(store.max_log_bytes, MAX_LOG_BYTES)

    def test_noisy_connection_failures_are_aggregated_at_session_end(self):
        with tempfile.TemporaryDirectory() as temp_dir:
            path = Path(temp_dir) / "app.log"
            store = AccelerationLogStore(
                path,
                rate_limit_window_seconds=60,
            )
            store.start("tun", ["Ethernet"])
            message = (
                "[出站池-aggregation][连通失败] 以太网 -> 203.0.113.1:443 "
                "(203.0.113.1) | TimeoutError:"
            )
            for _ in range(6):
                store.record(message)
            store.finish("test")
            content = path.read_text(encoding="utf-8")

        self.assertEqual(content.count(message), 1)
        self.assertIn("出站池连通失败（aggregation/TimeoutError）", content)
        self.assertIn("重复 5 次，已省略", content)

    def test_oversized_active_session_keeps_header_and_recent_tail(self):
        with tempfile.TemporaryDirectory() as temp_dir:
            path = Path(temp_dir) / "app.log"
            store = AccelerationLogStore(
                path,
                max_log_bytes=4096,
                rate_limit_window_seconds=0,
            )
            store.start("tun", ["Ethernet"], context={"case": "size-limit"})
            for index in range(200):
                store.record(
                    f"[TUN] unique diagnostic line {index:03d} " + ("x" * 120),
                    force=True,
                )
            store.finish("test")
            content = path.read_text(encoding="utf-8")

        self.assertLessEqual(len(content.encode("utf-8")), 4096)
        self.assertTrue(content.startswith("=== HypoMux Acceleration Session |"))
        self.assertIn("session_context=", content)
        self.assertIn(TRUNCATION_MARKER, content)
        self.assertIn("unique diagnostic line 199", content)
        self.assertIn("HypoMux Acceleration Session End", content)


if __name__ == "__main__":
    unittest.main()
