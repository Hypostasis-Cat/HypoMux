from utils.network_utils import (
    _is_usable_ipv6_source,
    _parse_adapter_from_json,
)


def test_ipv6_source_filter_rejects_unbound_or_link_local_addresses():
    assert _is_usable_ipv6_source("2001:db8::10")
    assert _is_usable_ipv6_source("fd00::10")
    assert not _is_usable_ipv6_source("")
    assert not _is_usable_ipv6_source("::")
    assert not _is_usable_ipv6_source("::1")
    assert not _is_usable_ipv6_source("fe80::1%12")
    assert not _is_usable_ipv6_source("ff02::1")
    assert not _is_usable_ipv6_source("::ffff:192.0.2.1")


def test_powershell_adapter_metadata_preserves_ipv6_source_and_index():
    adapter = _parse_adapter_from_json(
        {
            "InterfaceIndex": 11,
            "IPv6InterfaceIndex": 12,
            "InterfaceAlias": "Ethernet",
            "IPv4Address": "192.0.2.10",
            "IPv6Address": "2001:db8::10",
            "IfType": 6,
        }
    )

    assert adapter is not None
    assert adapter["ipv6"] == "2001:db8::10"
    assert adapter["ipv6_if_index"] == 12
