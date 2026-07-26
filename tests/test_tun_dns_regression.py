"""Regression coverage for the v2.1.1 -> v2.1.2 TUN DNS breakage."""

import unittest

from utils.singbox_config import DNS_FAKEIP_TAG, DNS_LOCAL_TAG, build_config
from utils.socket_binding import IP_UNICAST_IF, configure_bound_ipv4_socket


class TunDnsRegressionTests(unittest.TestCase):
    def test_local_dns_does_not_reenter_aggregation_pool(self):
        config = build_config()
        local_dns = next(
            server for server in config["dns"]["servers"]
            if server["tag"] == DNS_LOCAL_TAG
        )

        self.assertEqual(local_dns["type"], "local")
        self.assertNotIn("detour", local_dns)

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


if __name__ == "__main__":
    unittest.main()
