"""Pure TUN orchestration constants shared by Go and compatibility workers."""

from __future__ import annotations

from typing import Any


PORT_ETHERNET = 2001
PORT_WIFI = 2002
PORT_AGGREGATION = 2003

DNS_MODE_DOH_STRICT = "doh_strict"
DNS_MODE_LEGACY_COMPAT = "legacy_compat"
DNS_HEALTH_INTERVAL = 20.0
DNS_HEALTH_FAILURE_LIMIT = 3

_IFTYPE_ETHERNET = 6
_IFTYPE_PPP = 23


def classify_nics(
    selected_nics: list[dict[str, Any]],
) -> tuple[list[dict[str, Any]], list[dict[str, Any]]]:
    """Split selected adapters into wired/PPP and Wi-Fi channel groups."""

    wired: list[dict[str, Any]] = []
    wifi: list[dict[str, Any]] = []
    for nic in selected_nics:
        iftype = int(nic.get("iftype", -1) or -1)
        alias = str(nic.get("name", nic.get("alias", "")))
        is_ppp = bool(nic.get("is_ppp", False)) or iftype == _IFTYPE_PPP
        is_wifi = iftype == 71 or any(
            keyword in alias.lower()
            for keyword in (
                "wlan",
                "wi-fi",
                "wifi",
                "wireless",
                "无线",
            )
        )
        if is_ppp or iftype == _IFTYPE_ETHERNET:
            wired.append(nic)
        elif is_wifi:
            wifi.append(nic)
        else:
            wired.append(nic)
    return wired, wifi
