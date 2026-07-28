"""Regression coverage for the v2.1.1 -> v2.1.2 TUN DNS breakage."""

import asyncio
import subprocess
import tempfile
import unittest
from pathlib import Path

from utils.singbox_config import (
    DNS_FAKEIP_TAG,
    DNS_LOCAL_TAG,
    build_config,
    write_config,
)
from utils.socket_binding import IP_UNICAST_IF, configure_bound_ipv4_socket
from utils.tun_manager import TunManager
from ui.main_window import _should_finish_acceleration_log


class TunDnsRegressionTests(unittest.TestCase):
    def test_internal_tun_restart_keeps_diagnostic_session_open(self):
        common = {
            "active": True,
            "tun_active": False,
            "tun_starting": False,
            "proxy_active": False,
            "proxy_worker_present": False,
        }

        self.assertFalse(_should_finish_acceleration_log(
            **common, tun_restart_pending=True,
        ))
        self.assertTrue(_should_finish_acceleration_log(
            **common, tun_restart_pending=False,
        ))

    def test_singbox_owned_doh_is_bound_outside_aggregation_pool(self):
        config = build_config(dns_plan={
            "mode": "doh",
            "server": "223.5.5.5",
            "tls_server_name": "dns.alidns.com",
            "path": "/dns-query",
            "bind_interface": "Ethernet",
            "bind_ip": "192.0.2.10",
        })
        upstream_dns = next(
            server for server in config["dns"]["servers"]
            if server["tag"] == DNS_LOCAL_TAG
        )

        self.assertEqual(upstream_dns["type"], "https")
        self.assertEqual(upstream_dns["server"], "223.5.5.5")
        self.assertEqual(upstream_dns["tls"]["server_name"], "dns.alidns.com")
        self.assertEqual(upstream_dns["bind_interface"], "Ethernet")
        self.assertEqual(upstream_dns["inet4_bind_address"], "192.0.2.10")
        self.assertNotIn("detour", upstream_dns)

    def test_traditional_dns_stays_inside_singbox_with_strict_route(self):
        config = build_config(
            strict_route=True,
            dns_plan={
                "mode": "legacy",
                "server": "192.0.2.53",
                "bind_interface": "Ethernet",
                "bind_ip": "192.0.2.10",
            },
        )
        upstream_dns = next(
            server for server in config["dns"]["servers"]
            if server["tag"] == DNS_LOCAL_TAG
        )

        self.assertTrue(config["inbounds"][0]["strict_route"])
        self.assertEqual(upstream_dns["type"], "udp")
        self.assertEqual(upstream_dns["server_port"], 53)

    def test_route_resolves_domains_before_local_socks_selection(self):
        route_rules = build_config()["route"]["rules"]
        resolve_index = next(
            index for index, rule in enumerate(route_rules)
            if rule.get("action") == "resolve"
        )
        dns_hijack_index = next(
            index for index, rule in enumerate(route_rules)
            if rule.get("action") == "hijack-dns"
        )

        self.assertGreater(resolve_index, dns_hijack_index)
        self.assertEqual(route_rules[resolve_index]["server"], DNS_LOCAL_TAG)
        self.assertEqual(route_rules[resolve_index]["strategy"], "prefer_ipv4")
        self.assertFalse(any(
            rule.get("protocol") == ["quic"] and rule.get("action") == "reject"
            for rule in route_rules
        ))
        self.assertFalse(any(
            rule.get("network") == ["udp"] and rule.get("outbound") == "direct"
            for rule in route_rules
        ))

    def test_fakeip_policy_matches_v211_after_native_dns_restore(self):
        dns_rules = build_config()["dns"]["rules"]

        self.assertEqual(dns_rules, [{
            "query_type": ["A", "AAAA"],
            "server": DNS_FAKEIP_TAG,
        }])

    def test_strict_route_is_enabled_by_default_for_doh_mode(self):
        config = build_config()
        tun = config["inbounds"][0]
        self.assertTrue(tun["strict_route"])
        self.assertIn("fdfe:dcba:9876::1/126", tun["address"])
        fakeip = next(server for server in config["dns"]["servers"] if server["tag"] == DNS_FAKEIP_TAG)
        self.assertEqual(fakeip["inet6_range"], "fc00::/18")

    def test_legacy_compatibility_mode_disables_strict_route(self):
        config = build_config(strict_route=False)
        self.assertFalse(config["inbounds"][0]["strict_route"])

    def test_source_bind_failure_keeps_v211_interface_pin_behavior(self):
        class SourceBindFailingSocket:
            def setsockopt(self, *_args):
                pass

            def bind(self, _address):
                raise OSError("address changed during reconnect")

        info = configure_bound_ipv4_socket(
            SourceBindFailingSocket(),
            {"name": "test", "ip": "192.0.2.10", "if_index": 7},
            "test",
        )

        self.assertEqual(IP_UNICAST_IF, 31)
        self.assertIn("address changed", info["source_bind_error"])

    def test_wfp_error_is_retained_when_later_stderr_lines_arrive(self):
        class Stream:
            def __init__(self):
                self.lines = [
                    b"FATAL starting TUN interface: FwpmEngineOpen0: invalid argument\n",
                    b"INFO shutting down\n",
                    b"",
                ]

            async def readline(self):
                return self.lines.pop(0)

        manager = TunManager("test.json")
        asyncio.run(manager._pump_stream(Stream(), "stderr"))

        self.assertTrue(manager._saw_wfp_engine_error)
        self.assertIn("FwpmEngineOpen0", manager._wfp_engine_error_detail)
        self.assertEqual(manager._last_stderr_line, "INFO shutting down")

    def test_generated_dns_config_is_accepted_by_bundled_singbox(self):
        binary = Path(__file__).resolve().parents[1] / "bin" / "sing-box.exe"
        if not binary.exists():
            self.skipTest("bundled sing-box is not present")

        with tempfile.TemporaryDirectory() as temp_dir:
            config_path = Path(temp_dir) / "singbox-config.json"
            self.assertTrue(write_config(build_config(dns_plan={
                "mode": "doh",
                "server": "223.5.5.5",
                "tls_server_name": "dns.alidns.com",
                "path": "/dns-query",
                "bind_interface": "Ethernet",
                "bind_ip": "192.0.2.10",
            }), config_path))
            result = subprocess.run(
                [str(binary), "check", "-c", str(config_path)],
                capture_output=True,
                text=True,
                timeout=10,
                check=False,
            )

        self.assertEqual(
            result.returncode,
            0,
            msg=(result.stderr or result.stdout).strip(),
        )


if __name__ == "__main__":
    unittest.main()
