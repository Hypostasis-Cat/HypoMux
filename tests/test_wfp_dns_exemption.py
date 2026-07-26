"""Unit tests for the scoped WFP DNS policy planner (no WFP privileges needed)."""

import unittest

from utils.wfp_dns_exemption import (
    FWP_ACTION_PERMIT,
    FWP_BYTE_BLOB_TYPE,
    FWP_UINT16,
    FWP_UINT32,
    FWP_UINT8,
    FWPM_FILTER_FLAG_CLEAR_ACTION_RIGHT,
    FWP_BYTE_BLOB,
    WfpDnsExemption,
    build_dns_rule_specs,
    ipv4_to_wfp_uint32,
)
import ctypes
from utils.config_manager import _coerce_config, default_config


class WfpDnsExemptionTests(unittest.TestCase):
    def test_product_default_enables_the_scoped_fix_but_respects_opt_out(self):
        self.assertTrue(default_config()["wfp_dns_egress_exemption"])
        self.assertTrue(_coerce_config({})["wfp_dns_egress_exemption"])
        self.assertFalse(_coerce_config({"wfp_dns_egress_exemption": False})["wfp_dns_egress_exemption"])

    def test_ipv4_value_keeps_network_byte_layout(self):
        value = ipv4_to_wfp_uint32("192.0.2.10")
        self.assertEqual(value, 0x0A0200C0)
        self.assertEqual(value.to_bytes(4, "little"), b"\xc0\x00\x02\x0a")

    def test_rules_are_limited_to_each_selected_adapter_and_port_53(self):
        rules = build_dns_rule_specs([
            {"name": "Ethernet", "ip": "192.0.2.10", "if_index": 7},
            {"name": "WLAN", "ip": "198.51.100.20", "if_index": 21},
        ])

        self.assertEqual(len(rules), 4)
        self.assertEqual({rule.protocol for rule in rules}, {"tcp", "udp"})
        self.assertEqual({rule.remote_port for rule in rules}, {53})
        self.assertEqual(
            {(rule.source_ip, rule.interface_index) for rule in rules},
            {("192.0.2.10", 7), ("198.51.100.20", 21)},
        )

    def test_invalid_or_unresolved_adapters_do_not_broaden_the_policy(self):
        rules = build_dns_rule_specs([
            {"name": "no index", "ip": "192.0.2.10", "if_index": 0},
            {"name": "bad IP", "ip": "not-an-ip", "if_index": 7},
            {"name": "valid", "ip": "203.0.113.7", "if_index": "13"},
        ])

        self.assertEqual(len(rules), 2)
        self.assertTrue(all(rule.source_ip == "203.0.113.7" for rule in rules))

    def test_filter_is_a_hard_permit_with_only_five_exact_conditions(self):
        controller = WfpDnsExemption([
            {"name": "Ethernet", "ip": "192.0.2.10", "if_index": 7},
        ])
        app_id = FWP_BYTE_BLOB()
        filter_data, conditions = controller._make_filter(
            controller.rules[0], ctypes.pointer(app_id)
        )

        self.assertEqual(filter_data.action.type, FWP_ACTION_PERMIT)
        self.assertEqual(filter_data.flags, FWPM_FILTER_FLAG_CLEAR_ACTION_RIGHT)
        self.assertEqual(len(conditions), 5)
        self.assertEqual(
            [condition.conditionValue.type for condition in conditions],
            [FWP_BYTE_BLOB_TYPE, FWP_UINT32, FWP_UINT32, FWP_UINT8, FWP_UINT16],
        )


if __name__ == "__main__":
    unittest.main()
