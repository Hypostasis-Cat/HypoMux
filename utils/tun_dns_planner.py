"""DNS-only facade for TUN startup orchestration.

The verified probes still live in ``MultiPortProxyWorker`` during this
migration slice.  This facade deliberately never starts that QThread or opens
its SOCKS listeners; it exposes only DNS planning and health probing to the
Go-backed TUN worker.
"""

from __future__ import annotations

import asyncio
from typing import Callable

from proxy_worker import MultiPortProxyWorker


class TunDnsPlanner:
    DNS_MODE_DOH_STRICT = MultiPortProxyWorker.DNS_MODE_DOH_STRICT
    DNS_MODE_LEGACY_COMPAT = MultiPortProxyWorker.DNS_MODE_LEGACY_COMPAT
    DNS_HEALTH_INTERVAL = MultiPortProxyWorker.DNS_HEALTH_INTERVAL
    DNS_HEALTH_FAILURE_LIMIT = MultiPortProxyWorker.DNS_HEALTH_FAILURE_LIMIT

    def __init__(
        self,
        selected_nics: list[dict],
        *,
        allow_degraded_start: bool = False,
    ):
        self._worker = MultiPortProxyWorker(
            selected_nics=selected_nics,
            allow_degraded_start=allow_degraded_start,
            resolve_socks_domains=False,
        )
        self._allow_degraded_start = bool(allow_degraded_start)

    def set_log_callback(self, callback: Callable[[str], None]):
        self._worker.log_signal.connect(callback)

    def set_dns_servers(self, servers: list[str]):
        self._worker.set_dns_servers(servers)

    def set_doh_provider(self, provider: str):
        self._worker.set_doh_provider(provider)

    def set_dns_mode_override(self, mode: str):
        self._worker.set_dns_mode_override(mode)

    def cancel(self):
        self._worker._stop_requested = True

    def prepare(self) -> tuple[bool, list[str]]:
        if self._worker._stop_requested:
            return False, ["TUN DNS planning cancelled"]
        if self._allow_degraded_start:
            self._worker._dns_mode = (
                self.DNS_MODE_LEGACY_COMPAT
                if (
                    self._worker._doh_provider == "off"
                    or self._worker._dns_mode_override
                )
                else self.DNS_MODE_DOH_STRICT
            )
            self._worker._dns_egress_plan = self._worker.singbox_dns_plan()
            return True, [
                "forced development mode: skipped selected-adapter DNS preflight"
            ]
        return asyncio.run(self._worker._preflight_selected_nics_dns())

    def dns_mode(self) -> str:
        return self._worker.dns_mode()

    def singbox_dns_plan(self) -> dict[str, object]:
        return self._worker.singbox_dns_plan()

    def selected_nics_snapshot(self) -> list[dict]:
        return self._worker.selected_nics_snapshot()

    def probe_selected_doh(self):
        plan = self.singbox_dns_plan()
        if plan.get("mode") != "doh":
            return
        selected = self.selected_nics_snapshot()
        nic = next(
            (
                item
                for item in selected
                if (
                    str(item.get("name") or "")
                    == str(plan.get("bind_interface") or "")
                    or str(item.get("ip") or "")
                    == str(plan.get("bind_ip") or "")
                )
            ),
            selected[0] if selected else None,
        )
        if nic is None:
            raise RuntimeError("selected DoH adapter is unavailable")
        self._worker._query_dns_doh_sync(
            self._worker.DNS_PREFLIGHT_DOMAIN,
            nic,
            str(plan.get("server") or ""),
            str(plan.get("tls_server_name") or ""),
            str(plan.get("path") or "/dns-query"),
            2.0,
            3.0,
        )
