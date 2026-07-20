"""Windows IPv4 outbound-interface binding and diagnostics.

Every HypoMux path that promises a selected physical NIC must use this module.
``bind(local_ip)`` alone only selects a source address on multihomed Windows
hosts; it is not accepted here as a replacement for IP_UNICAST_IF.
"""

from __future__ import annotations

import ctypes
from ctypes import wintypes
import logging
import socket
import struct
from typing import Any, Callable, Dict, Optional, Tuple


logger = logging.getLogger(__name__)

TraceCallback = Callable[[str], None]

IP_UNICAST_IF = 31
NO_ERROR = 0


class _MIB_IPFORWARDROW(ctypes.Structure):
    _fields_ = [
        ("dwForwardDest", wintypes.DWORD),
        ("dwForwardMask", wintypes.DWORD),
        ("dwForwardPolicy", wintypes.DWORD),
        ("dwForwardNextHop", wintypes.DWORD),
        ("dwForwardIfIndex", wintypes.DWORD),
        ("dwForwardType", wintypes.DWORD),
        ("dwForwardProto", wintypes.DWORD),
        ("dwForwardAge", wintypes.DWORD),
        ("dwForwardNextHopAS", wintypes.DWORD),
        ("dwForwardMetric1", wintypes.DWORD),
        ("dwForwardMetric2", wintypes.DWORD),
        ("dwForwardMetric3", wintypes.DWORD),
        ("dwForwardMetric4", wintypes.DWORD),
        ("dwForwardMetric5", wintypes.DWORD),
    ]


def _winerror(message: str, code: int = 10022) -> OSError:
    error = OSError(code, message)
    error.winerror = code
    return error


def _as_ipv4(value: Any, field: str) -> str:
    text = str(value or "").strip()
    try:
        socket.inet_aton(text)
    except OSError as exc:
        raise _winerror(f"invalid {field}: {text!r}") from exc
    return text


def adapter_luid(if_index: int) -> Optional[int]:
    """Return a best-effort Windows Interface LUID for diagnostics."""
    if if_index <= 0 or not hasattr(ctypes, "windll"):
        return None
    try:
        luid = ctypes.c_ulonglong()
        fn = ctypes.windll.Iphlpapi.ConvertInterfaceIndexToLuid
        fn.argtypes = [wintypes.DWORD, ctypes.POINTER(ctypes.c_ulonglong)]
        fn.restype = wintypes.DWORD
        if fn(wintypes.DWORD(if_index), ctypes.byref(luid)) == NO_ERROR:
            return int(luid.value)
    except Exception:
        logger.debug("ConvertInterfaceIndexToLuid failed", exc_info=True)
    return None


def best_route_ipv4(source_ip: str, destination_ip: str) -> Dict[str, Any]:
    """Read Windows' current route decision for diagnostic logging only."""
    result: Dict[str, Any] = {"route_if_index": None, "next_hop": "", "metric": None}
    if not hasattr(ctypes, "windll"):
        return result
    try:
        source = struct.unpack("=I", socket.inet_aton(source_ip))[0]
        destination = struct.unpack("=I", socket.inet_aton(destination_ip))[0]
        row = _MIB_IPFORWARDROW()
        fn = ctypes.windll.Iphlpapi.GetBestRoute
        fn.argtypes = [wintypes.DWORD, wintypes.DWORD, ctypes.POINTER(_MIB_IPFORWARDROW)]
        fn.restype = wintypes.DWORD
        code = int(fn(destination, source, ctypes.byref(row)))
        if code != NO_ERROR:
            result["route_error"] = code
            return result
        result.update({
            "route_if_index": int(row.dwForwardIfIndex),
            "next_hop": socket.inet_ntoa(struct.pack("=I", row.dwForwardNextHop)),
            "metric": int(row.dwForwardMetric1),
        })
    except Exception as exc:
        result["route_error"] = f"{type(exc).__name__}: {exc}"
    return result


def _notify_trace(callback: Optional[TraceCallback], message: str) -> None:
    """Forward a diagnostic event to the UI-owned persistent log when present."""
    if callback is None:
        return
    try:
        callback(message)
    except Exception:
        logger.debug("socket binding trace callback failed", exc_info=True)


def configure_bound_ipv4_socket(
    sock: socket.socket,
    nic: Dict[str, Any],
    purpose: str,
    trace: Optional[TraceCallback] = None,
) -> Dict[str, Any]:
    """Require and verify an IPv4 interface+source binding before connect/send.

    Raises on every binding failure.  In particular, interface index zero is
    rejected because IP_UNICAST_IF=0 deliberately means "unspecified".
    """
    try:
        if_index = int(nic.get("if_index", nic.get("index", 0)) or 0)
    except (TypeError, ValueError) as exc:
        raise _winerror("invalid interface index") from exc
    if if_index <= 0:
        raise _winerror("refusing unpinned socket: missing IPv4 IfIndex")

    source_ip = _as_ipv4(nic.get("ip"), "source IPv4")
    name = str(nic.get("name") or source_ip)
    luid = adapter_luid(if_index)
    payload = struct.pack("!I", if_index)
    try:
        sock.setsockopt(socket.IPPROTO_IP, IP_UNICAST_IF, payload)
        # Winsock returns this option in host byte order when queried.
        actual = struct.unpack("=I", sock.getsockopt(socket.IPPROTO_IP, IP_UNICAST_IF, 4))[0]
        if actual != if_index:
            raise _winerror(
                f"IP_UNICAST_IF verification mismatch: requested={if_index}, actual={actual}"
            )
        sock.bind((source_ip, 0))
    except OSError as exc:
        message = (
            "[socket-bind] failed purpose=%s adapter=%s source=%s if_index=%s "
            "luid=%s gateway=%s ip_unicast_if=failed bind=failed "
            "winerror=%s errno=%s error=%s"
        ) % (
            purpose, name, source_ip, if_index,
            f"0x{luid:016X}" if luid is not None else "unknown",
            nic.get("gateway", ""), getattr(exc, "winerror", None),
            getattr(exc, "errno", None), exc,
        )
        logger.warning(message)
        _notify_trace(trace, message)
        raise

    info = {
        "adapter": name,
        "source_ip": source_ip,
        "if_index": if_index,
        "luid": luid,
        "gateway": str(nic.get("gateway") or ""),
        "purpose": purpose,
    }
    message = (
        "[socket-bind] ready purpose=%s adapter=%s source=%s if_index=%s luid=%s "
        "gateway=%s ip_unicast_if=set+verified(%s) bind=ok"
    ) % (
        purpose, name, source_ip, if_index,
        f"0x{luid:016X}" if luid is not None else "unknown", info["gateway"], actual,
    )
    logger.info(message)
    _notify_trace(trace, message)
    return info


def log_connected_ipv4_socket(
    sock: socket.socket,
    nic: Dict[str, Any],
    destination: Tuple[str, int],
    purpose: str,
    trace: Optional[TraceCallback] = None,
) -> None:
    """Log actual local endpoint and Windows route evidence after a send/connect."""
    source_ip = str(nic.get("ip") or "")
    route = best_route_ipv4(source_ip, destination[0])
    try:
        local = sock.getsockname()
    except OSError as exc:
        local = f"getsockname failed: {exc}"
    message = (
        "[socket-route] purpose=%s adapter=%s destination=%s:%s local=%s "
        "expected_if=%s route_if=%s next_hop=%s metric=%s route_error=%s"
    ) % (
        purpose, nic.get("name", source_ip), destination[0], destination[1], local,
        nic.get("if_index", nic.get("index")), route.get("route_if_index"),
        route.get("next_hop"), route.get("metric"), route.get("route_error", ""),
    )
    logger.info(message)
    _notify_trace(trace, message)


def probe_bound_tcp(
    nic: Dict[str, Any],
    endpoints: Tuple[Tuple[str, int], ...],
    timeout: float = 2.0,
    trace: Optional[TraceCallback] = None,
) -> Tuple[bool, str]:
    """Perform a real selected-interface TCP probe for adapter health checks."""
    failures = []
    for destination in endpoints:
        sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        try:
            sock.settimeout(timeout)
            configure_bound_ipv4_socket(sock, nic, "health-tcp", trace)
            sock.connect(destination)
            log_connected_ipv4_socket(sock, nic, destination, "health-tcp", trace)
            return True, f"TCP {destination[0]}:{destination[1]} via {sock.getsockname()[0]}"
        except OSError as exc:
            route = best_route_ipv4(str(nic.get("ip") or ""), destination[0])
            detail = (
                f"{destination[0]}:{destination[1]} winerror={getattr(exc, 'winerror', None)} "
                f"errno={getattr(exc, 'errno', None)} error={exc} "
                f"route_if={route.get('route_if_index')} next_hop={route.get('next_hop')} "
                f"metric={route.get('metric')} route_error={route.get('route_error', '')}"
            )
            failures.append(detail)
            message = f"[socket-probe] failed adapter={nic.get('name', nic.get('ip', ''))} {detail}"
            logger.warning(message)
            _notify_trace(trace, message)
        finally:
            try:
                sock.close()
            except OSError:
                pass
    return False, "; ".join(failures[-3:]) or "no probe endpoints"
