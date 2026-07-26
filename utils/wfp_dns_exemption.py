"""Scoped Windows Filtering Platform (WFP) DNS exemption for HypoMux.

``tun.strict_route`` uses WFP on Windows to prevent DNS from escaping the
virtual adapter.  That is correct for ordinary applications, but HypoMux's
own upstream pool deliberately opens *bound* sockets on the selected physical
adapters.  This module adds only the missing exception:

* executable identity is the running HypoMux executable (or its venv Python);
* local IPv4 address and interface index are one selected adapter;
* protocol is TCP or UDP; and
* remote port is exactly 53.

The filters live in a dynamic WFP session.  They are explicitly closed during
TUN shutdown and are also removed by BFE if HypoMux crashes.  No Windows
Firewall/netsh rules are persisted and no other application receives an
exception.

The implementation intentionally stays in user mode: standard ALE filtering
is sufficient here, so this is not a kernel driver or a packet redirector.
"""

from __future__ import annotations

import ctypes
import os
import socket
import struct
import sys
import uuid
from dataclasses import dataclass
from pathlib import Path
from typing import Dict, Iterable, List, Optional, Sequence


class WfpPolicyError(RuntimeError):
    """Raised when Windows rejects a WFP policy operation."""


@dataclass(frozen=True)
class WfpDnsRuleSpec:
    """One intentionally narrow permit rule, useful for logging and tests."""

    adapter_name: str
    source_ip: str
    interface_index: int
    protocol: str
    remote_port: int = 53


def current_application_path() -> str:
    """Return the real process image path used by WFP ALE_APP_ID.

    Nuitka standalone builds may expose a helper ``python.exe`` through
    ``sys.executable`` even though the running process is ``HypoMux.exe``.
    WFP resolves an ALE application identity from an on-disk executable, so
    use the Windows process image first and fall back to the interpreter only
    for normal source/venv execution.
    """
    candidates: List[Path] = []
    if os.name == "nt":
        try:
            # A long-path-sized buffer also covers installations below a path
            # containing non-ASCII characters without relying on ANSI APIs.
            buffer = ctypes.create_unicode_buffer(32768)
            length = ctypes.windll.kernel32.GetModuleFileNameW(
                None, buffer, len(buffer)
            )
            if length:
                candidates.append(Path(buffer.value))
        except Exception:
            # Source execution still has the interpreter path below.
            pass
    if sys.executable:
        candidates.append(Path(sys.executable))

    for candidate in candidates:
        try:
            resolved = candidate.resolve(strict=True)
        except OSError:
            continue
        if resolved.is_file():
            return str(resolved)
    raise WfpPolicyError("unable to locate the running HypoMux executable for WFP")


def ipv4_to_wfp_uint32(address: str) -> int:
    """Return an IPv4 value with the in-memory layout expected by WFP.

    Windows C callers pass ``IN_ADDR.S_un.S_addr`` to ``FWP_UINT32``.  On
    little-endian Windows that means the numeric value is unpacked little
    endian while the bytes remain normal network order.
    """
    try:
        packed = socket.inet_aton(str(address).strip())
    except OSError as exc:
        raise ValueError(f"invalid IPv4 address: {address!r}") from exc
    return struct.unpack("<I", packed)[0]


def build_dns_rule_specs(selected_nics: Iterable[Dict]) -> List[WfpDnsRuleSpec]:
    """Expand valid selected adapters into TCP/UDP DNS permit specifications."""
    result: List[WfpDnsRuleSpec] = []
    seen = set()
    for nic in selected_nics or ():
        if not isinstance(nic, dict):
            continue
        ip = str(nic.get("ip", "")).strip()
        try:
            if_index = int(nic.get("if_index", 0))
            ipv4_to_wfp_uint32(ip)
        except (TypeError, ValueError):
            continue
        if if_index <= 0:
            continue
        name = str(nic.get("name") or nic.get("alias") or ip).strip()
        for protocol in ("udp", "tcp"):
            key = (ip, if_index, protocol)
            if key not in seen:
                seen.add(key)
                result.append(WfpDnsRuleSpec(name, ip, if_index, protocol))
    return result


# These values are from the Windows SDK headers (fwpmtypes.h/fwpmu.h).
FWP_UINT8 = 1
FWP_UINT16 = 2
FWP_UINT32 = 3
FWP_BYTE_BLOB_TYPE = 12
FWP_MATCH_EQUAL = 0
FWP_ACTION_PERMIT = 0x00001002
FWPM_SESSION_FLAG_DYNAMIC = 0x00000001
FWPM_FILTER_FLAG_CLEAR_ACTION_RIGHT = 0x00000008
RPC_C_AUTHN_DEFAULT = 0xFFFFFFFF
IPPROTO_TCP = 6
IPPROTO_UDP = 17


class GUID(ctypes.Structure):
    _fields_ = [
        ("Data1", ctypes.c_uint32),
        ("Data2", ctypes.c_uint16),
        ("Data3", ctypes.c_uint16),
        ("Data4", ctypes.c_ubyte * 8),
    ]

    @classmethod
    def from_uuid(cls, value: uuid.UUID | str) -> "GUID":
        parsed = value if isinstance(value, uuid.UUID) else uuid.UUID(str(value))
        return cls.from_buffer_copy(parsed.bytes_le)


class FWPM_DISPLAY_DATA0(ctypes.Structure):
    _fields_ = [("name", ctypes.c_wchar_p), ("description", ctypes.c_wchar_p)]


class FWP_BYTE_BLOB(ctypes.Structure):
    _fields_ = [("size", ctypes.c_uint32), ("data", ctypes.POINTER(ctypes.c_ubyte))]


class FWP_VALUE0_UNION(ctypes.Union):
    _fields_ = [
        ("uint8", ctypes.c_uint8),
        ("uint16", ctypes.c_uint16),
        ("uint32", ctypes.c_uint32),
        ("byteBlob", ctypes.POINTER(FWP_BYTE_BLOB)),
    ]


class FWP_VALUE0(ctypes.Structure):
    _anonymous_ = ("value",)
    _fields_ = [("type", ctypes.c_uint32), ("value", FWP_VALUE0_UNION)]


class FWPM_FILTER_CONDITION0(ctypes.Structure):
    _fields_ = [
        ("fieldKey", GUID),
        ("matchType", ctypes.c_uint32),
        ("conditionValue", FWP_VALUE0),
    ]


class FWPM_ACTION0(ctypes.Structure):
    _fields_ = [("type", ctypes.c_uint32), ("filterType", GUID)]


class FWPM_FILTER_CONTEXT0(ctypes.Union):
    _fields_ = [("rawContext", ctypes.c_uint64), ("providerContextKey", GUID)]


class FWPM_FILTER0(ctypes.Structure):
    _fields_ = [
        ("filterKey", GUID),
        ("displayData", FWPM_DISPLAY_DATA0),
        ("flags", ctypes.c_uint32),
        ("providerKey", ctypes.POINTER(GUID)),
        ("providerData", FWP_BYTE_BLOB),
        ("layerKey", GUID),
        ("subLayerKey", GUID),
        ("weight", FWP_VALUE0),
        ("numFilterConditions", ctypes.c_uint32),
        ("filterCondition", ctypes.POINTER(FWPM_FILTER_CONDITION0)),
        ("action", FWPM_ACTION0),
        ("context", FWPM_FILTER_CONTEXT0),
        ("reserved", ctypes.POINTER(GUID)),
        ("filterId", ctypes.c_uint64),
        ("effectiveWeight", FWP_VALUE0),
    ]


class FWPM_SESSION0(ctypes.Structure):
    _fields_ = [
        ("sessionKey", GUID),
        ("displayData", FWPM_DISPLAY_DATA0),
        ("flags", ctypes.c_uint32),
        ("txnWaitTimeoutInMSec", ctypes.c_uint32),
        ("processId", ctypes.c_uint32),
        ("sid", ctypes.c_void_p),
        ("username", ctypes.c_wchar_p),
        ("kernelMode", ctypes.c_int32),
    ]


class FWPM_SUBLAYER0(ctypes.Structure):
    _fields_ = [
        ("subLayerKey", GUID),
        ("displayData", FWPM_DISPLAY_DATA0),
        ("flags", ctypes.c_uint32),
        ("providerKey", ctypes.POINTER(GUID)),
        ("providerData", FWP_BYTE_BLOB),
        ("weight", ctypes.c_uint16),
    ]


# Built-in Windows Filtering Platform keys.
FWPM_LAYER_ALE_AUTH_CONNECT_V4 = GUID.from_uuid("c38d57d1-05a7-4c33-904f-7fbceee60e82")
FWPM_CONDITION_ALE_APP_ID = GUID.from_uuid("d78e1e87-8644-4ea5-9437-d809ecefc971")
FWPM_CONDITION_IP_LOCAL_ADDRESS = GUID.from_uuid("d9ee00de-c1ef-4617-bfe3-ffd8f5a08957")
FWPM_CONDITION_INTERFACE_INDEX = GUID.from_uuid("667fd755-d695-434a-8af5-d3835a1259bc")
FWPM_CONDITION_IP_PROTOCOL = GUID.from_uuid("3971ef2b-623e-4f9a-8cb1-6e79b806b9a7")
FWPM_CONDITION_IP_REMOTE_PORT = GUID.from_uuid("c35a604d-d22b-4e1a-91b4-68f674ee674b")


class WfpDnsExemption:
    """Own a dynamic, high-priority WFP sublayer for a TUN run.

    ``install`` is idempotent for one instance.  Keep this object alive for
    the full TUN lifetime; closing its engine tears down every temporary rule.
    """

    def __init__(self, selected_nics: Sequence[Dict], application_path: Optional[str] = None):
        self._rules = build_dns_rule_specs(selected_nics)
        self.application_path = str(Path(application_path or current_application_path()).resolve())
        self._engine = ctypes.c_void_p()
        self._filter_ids: List[int] = []
        self._sub_layer_key = GUID.from_uuid(uuid.uuid4())
        self._installed = False

    @property
    def rules(self) -> Sequence[WfpDnsRuleSpec]:
        return tuple(self._rules)

    @property
    def installed(self) -> bool:
        return self._installed

    @property
    def filter_ids(self) -> Sequence[int]:
        return tuple(self._filter_ids)

    def install(self) -> Sequence[int]:
        if self._installed:
            return self.filter_ids
        if os.name != "nt":
            raise WfpPolicyError("WFP is only available on Windows")
        if not self._rules:
            raise WfpPolicyError("no selected adapter has a usable IPv4 address and interface index")

        api = _WfpApi()
        session = FWPM_SESSION0()
        session.sessionKey = GUID.from_uuid(uuid.uuid4())
        session.displayData = FWPM_DISPLAY_DATA0(
            "HypoMux temporary DNS egress exemption",
            "Dynamic, per-adapter TCP/UDP 53 permit for HypoMux upstream sockets",
        )
        session.flags = FWPM_SESSION_FLAG_DYNAMIC

        self._check(api.FwpmEngineOpen0(None, RPC_C_AUTHN_DEFAULT, None, ctypes.byref(session), ctypes.byref(self._engine)), "FwpmEngineOpen0")
        app_id = ctypes.POINTER(FWP_BYTE_BLOB)()
        try:
            self._check(api.FwpmSubLayerAdd0(self._engine, ctypes.byref(self._make_sublayer()), None), "FwpmSubLayerAdd0")
            self._check(api.FwpmGetAppIdFromFileName0(self.application_path, ctypes.byref(app_id)), "FwpmGetAppIdFromFileName0")
            self._check(api.FwpmTransactionBegin0(self._engine, 0), "FwpmTransactionBegin0")
            try:
                for rule in self._rules:
                    filter_id = ctypes.c_uint64()
                    filter_data, conditions = self._make_filter(rule, app_id)
                    self._check(api.FwpmFilterAdd0(self._engine, ctypes.byref(filter_data), None, ctypes.byref(filter_id)), "FwpmFilterAdd0")
                    self._filter_ids.append(int(filter_id.value))
                self._check(api.FwpmTransactionCommit0(self._engine), "FwpmTransactionCommit0")
            except Exception:
                api.FwpmTransactionAbort0(self._engine)
                raise
            self._installed = True
            return self.filter_ids
        except Exception:
            self.close()
            raise
        finally:
            if app_id:
                raw = ctypes.cast(app_id, ctypes.c_void_p)
                api.FwpmFreeMemory0(ctypes.byref(raw))

    def close(self) -> None:
        """Remove all dynamic filters. Safe to call repeatedly."""
        if not self._engine:
            self._installed = False
            self._filter_ids.clear()
            return
        try:
            _WfpApi().FwpmEngineClose0(self._engine)
        finally:
            self._engine = ctypes.c_void_p()
            self._installed = False
            self._filter_ids.clear()

    def _make_sublayer(self) -> FWPM_SUBLAYER0:
        sublayer = FWPM_SUBLAYER0()
        sublayer.subLayerKey = self._sub_layer_key
        sublayer.displayData = FWPM_DISPLAY_DATA0(
            "HypoMux DNS egress exemption",
            "Higher-priority dynamic sublayer for HypoMux bound DNS sockets only",
        )
        # The maximum sublayer weight makes this exact permit win before the
        # broad strict_route DNS block, while its conditions keep it narrow.
        sublayer.weight = 0xFFFF
        return sublayer

    def _make_filter(self, rule: WfpDnsRuleSpec, app_id: ctypes.POINTER(FWP_BYTE_BLOB)):
        protocol_number = IPPROTO_UDP if rule.protocol == "udp" else IPPROTO_TCP
        conditions = (FWPM_FILTER_CONDITION0 * 5)()
        conditions[0] = _condition_blob(FWPM_CONDITION_ALE_APP_ID, app_id)
        conditions[1] = _condition_uint32(FWPM_CONDITION_IP_LOCAL_ADDRESS, ipv4_to_wfp_uint32(rule.source_ip))
        conditions[2] = _condition_uint32(FWPM_CONDITION_INTERFACE_INDEX, rule.interface_index)
        conditions[3] = _condition_uint8(FWPM_CONDITION_IP_PROTOCOL, protocol_number)
        conditions[4] = _condition_uint16(FWPM_CONDITION_IP_REMOTE_PORT, rule.remote_port)

        filter_data = FWPM_FILTER0()
        filter_data.filterKey = GUID.from_uuid(uuid.uuid4())
        filter_data.displayData = FWPM_DISPLAY_DATA0(
            f"HypoMux {rule.protocol.upper()} DNS {rule.source_ip} if{rule.interface_index}",
            "Permit only the HypoMux bound upstream DNS socket through strict route",
        )
        filter_data.layerKey = FWPM_LAYER_ALE_AUTH_CONNECT_V4
        filter_data.subLayerKey = self._sub_layer_key
        # Make the permit hard.  sing-tun itself uses the same flag for its
        # self-protection permit; without it a lower-priority DNS block could
        # replace this exact allow action.
        filter_data.flags = FWPM_FILTER_FLAG_CLEAR_ACTION_RIGHT
        filter_data.weight.type = FWP_UINT8
        filter_data.weight.uint8 = 0x0F
        filter_data.numFilterConditions = len(conditions)
        filter_data.filterCondition = ctypes.cast(conditions, ctypes.POINTER(FWPM_FILTER_CONDITION0))
        filter_data.action.type = FWP_ACTION_PERMIT
        return filter_data, conditions

    @staticmethod
    def _check(status: int, operation: str) -> None:
        if status:
            try:
                detail = ctypes.WinError(int(status)).strerror
            except Exception:
                detail = "unknown WFP error"
            raise WfpPolicyError(f"{operation} failed (0x{int(status):08X}): {detail}")


def _condition_uint8(field: GUID, value: int) -> FWPM_FILTER_CONDITION0:
    condition = FWPM_FILTER_CONDITION0()
    condition.fieldKey = field
    condition.matchType = FWP_MATCH_EQUAL
    condition.conditionValue.type = FWP_UINT8
    condition.conditionValue.uint8 = int(value)
    return condition


def _condition_uint16(field: GUID, value: int) -> FWPM_FILTER_CONDITION0:
    condition = FWPM_FILTER_CONDITION0()
    condition.fieldKey = field
    condition.matchType = FWP_MATCH_EQUAL
    condition.conditionValue.type = FWP_UINT16
    condition.conditionValue.uint16 = int(value)
    return condition


def _condition_uint32(field: GUID, value: int) -> FWPM_FILTER_CONDITION0:
    condition = FWPM_FILTER_CONDITION0()
    condition.fieldKey = field
    condition.matchType = FWP_MATCH_EQUAL
    condition.conditionValue.type = FWP_UINT32
    condition.conditionValue.uint32 = int(value)
    return condition


def _condition_blob(field: GUID, value: ctypes.POINTER(FWP_BYTE_BLOB)) -> FWPM_FILTER_CONDITION0:
    condition = FWPM_FILTER_CONDITION0()
    condition.fieldKey = field
    condition.matchType = FWP_MATCH_EQUAL
    condition.conditionValue.type = FWP_BYTE_BLOB_TYPE
    condition.conditionValue.byteBlob = value
    return condition


class _WfpApi:
    """Typed lazy bindings, kept out of module import for non-Windows tests."""

    def __init__(self):
        try:
            self._dll = ctypes.WinDLL("fwpuclnt.dll", use_last_error=True)
        except Exception as exc:
            raise WfpPolicyError("Windows Filtering Platform client is unavailable") from exc
        self.FwpmEngineOpen0 = self._bind("FwpmEngineOpen0", ctypes.c_uint32, [ctypes.c_wchar_p, ctypes.c_uint32, ctypes.c_void_p, ctypes.POINTER(FWPM_SESSION0), ctypes.POINTER(ctypes.c_void_p)])
        self.FwpmEngineClose0 = self._bind("FwpmEngineClose0", ctypes.c_uint32, [ctypes.c_void_p])
        self.FwpmSubLayerAdd0 = self._bind("FwpmSubLayerAdd0", ctypes.c_uint32, [ctypes.c_void_p, ctypes.POINTER(FWPM_SUBLAYER0), ctypes.c_void_p])
        self.FwpmGetAppIdFromFileName0 = self._bind("FwpmGetAppIdFromFileName0", ctypes.c_uint32, [ctypes.c_wchar_p, ctypes.POINTER(ctypes.POINTER(FWP_BYTE_BLOB))])
        self.FwpmTransactionBegin0 = self._bind("FwpmTransactionBegin0", ctypes.c_uint32, [ctypes.c_void_p, ctypes.c_uint32])
        self.FwpmTransactionCommit0 = self._bind("FwpmTransactionCommit0", ctypes.c_uint32, [ctypes.c_void_p])
        self.FwpmTransactionAbort0 = self._bind("FwpmTransactionAbort0", ctypes.c_uint32, [ctypes.c_void_p])
        self.FwpmFilterAdd0 = self._bind("FwpmFilterAdd0", ctypes.c_uint32, [ctypes.c_void_p, ctypes.POINTER(FWPM_FILTER0), ctypes.c_void_p, ctypes.POINTER(ctypes.c_uint64)])
        self.FwpmFreeMemory0 = self._bind("FwpmFreeMemory0", None, [ctypes.POINTER(ctypes.c_void_p)])

    def _bind(self, name, restype, argtypes):
        function = getattr(self._dll, name)
        function.argtypes = argtypes
        function.restype = restype
        return function
