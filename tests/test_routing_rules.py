import json
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from ui.pages.routing_page import (
    ROUTING_BACKUP_FORMAT,
    ROUTING_BACKUP_VERSION,
    parse_routing_rules_backup,
)
from utils.config_manager import default_config, load_config, save_config
from utils.routing_rules import (
    MATCH_DOMAIN,
    MATCH_IP,
    MATCH_PROCESS,
    normalize_match_value,
    normalize_routing_rules,
    to_singbox_route_rule,
)
from utils.singbox_config import build_config, write_config


class RoutingRuleTests(unittest.TestCase):
    def test_old_process_rule_is_migrated_without_data_loss(self):
        rules = normalize_routing_rules([{
            "process_name": ["Game.exe", "helper.exe"],
            "outbound": "direct",
        }])

        self.assertEqual([rule["match_type"] for rule in rules], [
            MATCH_PROCESS,
            MATCH_PROCESS,
        ])
        self.assertEqual(
            [rule["process_name"][0] for rule in rules],
            ["Game.exe", "helper.exe"],
        )

    def test_ip_and_domain_values_are_validated_and_normalized(self):
        self.assertEqual(
            normalize_match_value(MATCH_IP, "203.0.113.9"),
            "203.0.113.9/32",
        )
        self.assertEqual(
            normalize_match_value(MATCH_IP, "2001:db8::9/64"),
            "2001:db8::/64",
        )
        self.assertEqual(
            normalize_match_value(MATCH_DOMAIN, "*.例子.测试"),
            "xn--fsqu00a.xn--0zwm56d",
        )
        self.assertIsNone(normalize_match_value(MATCH_DOMAIN, "https://example.com"))
        self.assertIsNone(normalize_match_value(MATCH_IP, "999.1.1.1"))

    def test_domain_rule_matches_apex_and_subdomains(self):
        rule = to_singbox_route_rule({
            "match_type": MATCH_DOMAIN,
            "domain": ["example.com"],
            "outbound": "nic_Ethernet",
        })

        self.assertEqual(rule["domain"], ["example.com"])
        self.assertEqual(rule["domain_suffix"], [".example.com"])
        self.assertEqual(rule["outbound"], "nic_Ethernet")

    def test_process_domain_ip_precedence_is_deterministic(self):
        config = build_config(rules=[
            {
                "match_type": MATCH_IP,
                "ip_cidr": ["203.0.113.0/24"],
                "outbound": "direct",
            },
            {
                "match_type": MATCH_DOMAIN,
                "domain": ["example.com"],
                "outbound": "nic_Ethernet",
            },
            {
                "process_name": ["game.exe"],
                "outbound": "aggregation",
            },
        ])
        user_rules = [
            rule for rule in config["route"]["rules"]
            if rule.get("process_name") == ["game.exe"]
            or rule.get("domain") == ["example.com"]
            or rule.get("ip_cidr") == ["203.0.113.0/24"]
        ]

        self.assertEqual(user_rules[0]["process_name"], ["game.exe"])
        self.assertEqual(user_rules[1]["domain"], ["example.com"])
        self.assertEqual(user_rules[2]["ip_cidr"], ["203.0.113.0/24"])

    def test_backup_v1_and_v2_are_both_accepted(self):
        old = parse_routing_rules_backup({
            "format": ROUTING_BACKUP_FORMAT,
            "version": 1,
            "rules": [{"process_name": ["old.exe"], "outbound": "direct"}],
        })
        new = parse_routing_rules_backup({
            "format": ROUTING_BACKUP_FORMAT,
            "version": ROUTING_BACKUP_VERSION,
            "rules": [{
                "match_type": MATCH_IP,
                "ip_cidr": ["198.51.100.0/24"],
                "outbound": "aggregation",
            }],
        })

        self.assertEqual(old[0]["match_type"], MATCH_PROCESS)
        self.assertEqual(new[0]["match_type"], MATCH_IP)

    def test_invalid_backup_rule_rejects_entire_import(self):
        with self.assertRaises(ValueError):
            parse_routing_rules_backup({
                "format": ROUTING_BACKUP_FORMAT,
                "version": ROUTING_BACKUP_VERSION,
                "rules": [{
                    "match_type": MATCH_IP,
                    "ip_cidr": ["not-an-ip"],
                    "outbound": "direct",
                }],
            })

    def test_three_rule_types_survive_config_save_and_load(self):
        rules = [
            {
                "match_type": MATCH_PROCESS,
                "process_name": ["game.exe"],
                "outbound": "direct",
            },
            {
                "match_type": MATCH_DOMAIN,
                "domain": ["example.com"],
                "outbound": "nic_Ethernet",
            },
            {
                "match_type": MATCH_IP,
                "ip_cidr": ["203.0.113.0/24"],
                "outbound": "aggregation",
            },
        ]
        config = default_config()
        config["routing_rules"] = rules

        with tempfile.TemporaryDirectory() as temp_dir:
            config_path = Path(temp_dir) / "config.json"
            with patch(
                "utils.config_manager.get_config_path",
                return_value=config_path,
            ):
                self.assertTrue(save_config(config))
                loaded = load_config()

        self.assertEqual(
            normalize_routing_rules(loaded["routing_rules"]),
            normalize_routing_rules(rules),
        )

    def test_exported_backup_round_trips_all_three_rule_types(self):
        rules = normalize_routing_rules([
            {
                "match_type": MATCH_PROCESS,
                "process_name": ["game.exe"],
                "outbound": "direct",
            },
            {
                "match_type": MATCH_DOMAIN,
                "domain": ["example.com"],
                "outbound": "nic_Ethernet",
            },
            {
                "match_type": MATCH_IP,
                "ip_cidr": ["203.0.113.9", "2001:db8::/32"],
                "outbound": "aggregation",
            },
        ])
        payload = {
            "format": ROUTING_BACKUP_FORMAT,
            "version": ROUTING_BACKUP_VERSION,
            "rules": rules,
        }

        with tempfile.TemporaryDirectory() as temp_dir:
            backup_path = Path(temp_dir) / "routing-rules.json"
            backup_path.write_text(
                json.dumps(payload, ensure_ascii=False, indent=2),
                encoding="utf-8",
            )
            imported_payload = json.loads(
                backup_path.read_text(encoding="utf-8-sig")
            )

        self.assertEqual(parse_routing_rules_backup(imported_payload), rules)

    def test_bundled_singbox_accepts_domain_and_ip_rules(self):
        binary = Path(__file__).resolve().parents[1] / "bin" / "sing-box.exe"
        if not binary.exists():
            self.skipTest("bundled sing-box is not present")
        config = build_config(rules=[
            {
                "match_type": MATCH_DOMAIN,
                "domain": ["example.com"],
                "outbound": "aggregation",
            },
            {
                "match_type": MATCH_IP,
                "ip_cidr": ["203.0.113.0/24", "2001:db8::/32"],
                "outbound": "direct",
            },
        ])
        with tempfile.TemporaryDirectory() as temp_dir:
            path = Path(temp_dir) / "config.json"
            self.assertTrue(write_config(config, path))
            result = subprocess.run(
                [str(binary), "check", "-c", str(path)],
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
