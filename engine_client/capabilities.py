"""Toolkit-independent capability requirements for production network modes."""

from __future__ import annotations

from typing import Any


PROXY_MODE = "proxy"
TUN_MODE = "tun_tcp_pool"

ENGINE_RUNTIME_METHODS = (
    "engine.start",
    "engine.stop",
    "engine.telemetry",
)
TUN_LIFECYCLE_METHODS = (
    "tun.activate",
    "tun.status",
    "tun.deactivate",
)

PROXY_REQUIRED_METHODS = ENGINE_RUNTIME_METHODS
PROXY_REQUIRED_FEATURES = (
    "socks5_connect",
    "http_connect",
    "source_bound_dns",
    "ipv6_egress",
    "adaptive_health",
    "domain_quarantine",
)

TUN_REQUIRED_METHODS = ENGINE_RUNTIME_METHODS + TUN_LIFECYCLE_METHODS
TUN_REQUIRED_FEATURES = (
    "tcp_connect",
    "udp_associate",
    "ipv6_egress",
    "adaptive_health",
    "managed_tun_lifecycle",
)


def missing_mode_requirements(
    hello: dict[str, Any],
    mode: str,
) -> dict[str, list[str]]:
    """Return missing modes, methods, and features for one production mode."""

    if mode == PROXY_MODE:
        required_methods = PROXY_REQUIRED_METHODS
        required_features = PROXY_REQUIRED_FEATURES
    elif mode == TUN_MODE:
        required_methods = TUN_REQUIRED_METHODS
        required_features = TUN_REQUIRED_FEATURES
    else:
        return {
            "modes": [mode],
            "methods": [],
            "features": [],
        }

    capabilities = hello.get("capabilities")
    advertised_methods = (
        {str(value) for value in capabilities}
        if isinstance(capabilities, list)
        else set()
    )
    modes = hello.get("modes")
    advertised_modes = (
        {str(value) for value in modes}
        if isinstance(modes, list)
        else set()
    )
    mode_features = hello.get("mode_features")
    advertised_features: set[str] = set()
    if isinstance(mode_features, dict):
        values = mode_features.get(mode)
        if isinstance(values, list):
            advertised_features = {str(value) for value in values}

    return {
        "modes": [] if mode in advertised_modes else [mode],
        "methods": [
            method
            for method in required_methods
            if method not in advertised_methods
        ],
        "features": [
            feature
            for feature in required_features
            if feature not in advertised_features
        ],
    }


def supports_mode_contract(hello: dict[str, Any], mode: str) -> bool:
    missing = missing_mode_requirements(hello, mode)
    return not any(missing.values())
