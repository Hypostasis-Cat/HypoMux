"""Validation and canonicalization for user-defined split-routing rules."""

from __future__ import annotations

import ipaddress
from typing import Any, Iterable, Optional


MATCH_PROCESS = "process"
MATCH_DOMAIN = "domain"
MATCH_IP = "ip"
VALID_MATCH_TYPES = (MATCH_PROCESS, MATCH_DOMAIN, MATCH_IP)


def is_valid_outbound(outbound: str) -> bool:
    tag = str(outbound or "").strip()
    return tag in {"aggregation", "direct", "nic_ethernet", "nic_wifi"} or (
        tag.startswith("nic_") and len(tag) > 4
    )


def normalize_match_value(match_type: str, value: Any) -> Optional[str]:
    """Normalize one UI value; return ``None`` when it is invalid."""
    kind = str(match_type or "").strip().lower()
    text = str(value or "").strip()
    if kind == MATCH_PROCESS:
        if (
            not text
            or len(text) > 260
            or any(ch in text for ch in ("/", "\\", ":", "\0"))
        ):
            return None
        return text

    if kind == MATCH_IP:
        if not text or len(text) > 64:
            return None
        try:
            if "/" not in text:
                address = ipaddress.ip_address(text)
                text = f"{address}/{address.max_prefixlen}"
            return str(ipaddress.ip_network(text, strict=False))
        except ValueError:
            return None

    if kind == MATCH_DOMAIN:
        text = text.rstrip(".").lower()
        if text.startswith("*."):
            text = text[2:]
        elif text.startswith("."):
            text = text[1:]
        if (
            not text
            or len(text) > 253
            or any(ch in text for ch in ("/", "\\", ":", "\0", "?", "#", "@", " "))
        ):
            return None
        try:
            ipaddress.ip_address(text)
            return None
        except ValueError:
            pass
        try:
            labels = [label.encode("idna").decode("ascii") for label in text.split(".")]
        except UnicodeError:
            return None
        if not labels or any(
            not label
            or len(label) > 63
            or label.startswith("-")
            or label.endswith("-")
            or any(not (ch.isalnum() or ch in "-_") for ch in label)
            for label in labels
        ):
            return None
        return ".".join(labels).lower()

    return None


def _as_values(value: Any) -> list[Any]:
    if isinstance(value, list):
        return value
    if isinstance(value, str):
        return [value]
    return []


def expand_routing_rule(raw: Any) -> list[dict]:
    """Expand a stored rule into one canonical UI rule per match value.

    Rules saved before match types were introduced contain only
    ``process_name`` and are treated as process rules.
    """
    if not isinstance(raw, dict):
        return []
    outbound = str(raw.get("outbound", "")).strip()
    if not is_valid_outbound(outbound):
        return []

    requested_type = str(raw.get("match_type", "")).strip().lower()
    aliases = {
        "process_name": MATCH_PROCESS,
        "process": MATCH_PROCESS,
        "domain": MATCH_DOMAIN,
        "domain_suffix": MATCH_DOMAIN,
        "ip": MATCH_IP,
        "ip_cidr": MATCH_IP,
    }
    match_type = aliases.get(requested_type, requested_type)
    if not match_type:
        present = [
            kind for field, kind in (
                ("process_name", MATCH_PROCESS),
                ("domain", MATCH_DOMAIN),
                ("domain_suffix", MATCH_DOMAIN),
                ("ip_cidr", MATCH_IP),
            )
            if _as_values(raw.get(field))
        ]
        if len(set(present)) != 1:
            return []
        match_type = present[0]
    if match_type not in VALID_MATCH_TYPES:
        return []

    field_candidates = {
        MATCH_PROCESS: ("process_name",),
        MATCH_DOMAIN: ("domain", "domain_suffix"),
        MATCH_IP: ("ip_cidr", "ip"),
    }[match_type]
    values: list[Any] = []
    for field in field_candidates:
        values.extend(_as_values(raw.get(field)))
    if not values and "value" in raw:
        values = _as_values(raw.get("value"))

    rules = []
    seen = set()
    for raw_value in values:
        value = normalize_match_value(match_type, raw_value)
        if value is None:
            return []
        identity = value.casefold()
        if identity in seen:
            continue
        seen.add(identity)
        if match_type == MATCH_PROCESS:
            rules.append({
                "match_type": MATCH_PROCESS,
                "process_name": [value],
                "outbound": outbound,
            })
        elif match_type == MATCH_DOMAIN:
            rules.append({
                "match_type": MATCH_DOMAIN,
                "domain": [value],
                "outbound": outbound,
            })
        else:
            rules.append({
                "match_type": MATCH_IP,
                "ip_cidr": [value],
                "outbound": outbound,
            })
    return rules


def routing_rule_value(rule: dict) -> str:
    kind = str(rule.get("match_type", MATCH_PROCESS))
    field = {
        MATCH_PROCESS: "process_name",
        MATCH_DOMAIN: "domain",
        MATCH_IP: "ip_cidr",
    }.get(kind, "process_name")
    values = _as_values(rule.get(field))
    return str(values[0]).strip() if values else ""


def routing_rule_identity(match_type: str, value: str) -> tuple[str, str]:
    normalized = normalize_match_value(match_type, value)
    return str(match_type), (normalized or str(value or "").strip()).casefold()


def routing_rule_sort_key(rule: dict) -> tuple:
    """Deterministic precedence: process, domain, then destination IP."""
    kind = str(rule.get("match_type", MATCH_PROCESS))
    value = routing_rule_value(rule)
    rank = {MATCH_PROCESS: 0, MATCH_DOMAIN: 1, MATCH_IP: 2}.get(kind, 9)
    if kind == MATCH_IP:
        try:
            network = ipaddress.ip_network(value, strict=False)
            return rank, network.version, -network.prefixlen, int(network.network_address)
        except ValueError:
            pass
    return rank, value.casefold()


def to_singbox_route_rule(rule: dict) -> Optional[dict]:
    """Convert one stored rule to a sing-box route rule."""
    expanded = expand_routing_rule(rule)
    if not expanded:
        return None
    outbound = expanded[0]["outbound"]
    match_type = expanded[0]["match_type"]
    if any(
        item["outbound"] != outbound or item["match_type"] != match_type
        for item in expanded
    ):
        return None
    values = [routing_rule_value(item) for item in expanded]
    if match_type == MATCH_PROCESS:
        return {"process_name": values, "outbound": outbound}
    if match_type == MATCH_IP:
        return {"ip_cidr": values, "outbound": outbound}
    # A domain entered by the user means the apex and all subdomains.
    return {
        "domain": values,
        "domain_suffix": [f".{value}" for value in values],
        "outbound": outbound,
    }


def normalize_routing_rules(rules: Iterable[Any]) -> list[dict]:
    normalized = []
    seen = set()
    for raw in rules or []:
        for rule in expand_routing_rule(raw):
            identity = routing_rule_identity(
                rule["match_type"], routing_rule_value(rule)
            )
            if identity in seen:
                continue
            seen.add(identity)
            normalized.append(rule)
    return sorted(normalized, key=routing_rule_sort_key)
