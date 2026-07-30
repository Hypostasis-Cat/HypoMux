from __future__ import annotations

import unittest
from unittest import mock

import main
from engine_client import BACKEND_AUTO, BACKEND_PYTHON


class StartupCleanupOwnershipTests(unittest.TestCase):
    def test_default_backend_never_kills_sing_box_by_image_name(self):
        with (
            mock.patch.object(
                main,
                "network_backend",
                return_value=BACKEND_AUTO,
            ),
            mock.patch.object(main, "_run_silent_command") as run,
        ):
            main.force_evict_zombie_backends()

        commands = [call.args[0] for call in run.call_args_list]
        self.assertFalse(
            any(command and command[0] == "taskkill" for command in commands)
        )
        joined = "\n".join(" ".join(command) for command in commands)
        self.assertIn("HypoMux-Tun", joined)
        self.assertIn("0.0.0.0/0", joined)
        self.assertIn("::/0", joined)

    def test_explicit_python_rollback_retains_legacy_cleanup(self):
        with (
            mock.patch.object(
                main,
                "network_backend",
                return_value=BACKEND_PYTHON,
            ),
            mock.patch.object(main, "_run_silent_command") as run,
        ):
            main.force_evict_zombie_backends()

        commands = [call.args[0] for call in run.call_args_list]
        self.assertIn(
            ["taskkill", "/F", "/IM", "sing-box.exe", "/T"],
            commands,
        )


if __name__ == "__main__":
    unittest.main()
