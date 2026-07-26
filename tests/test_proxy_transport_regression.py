"""Regression coverage for SOCKS transport isolation, DNS and IPv6 support."""

import asyncio
import socket
import struct
import unittest

from proxy_worker import (
    DNS_TYPE_A,
    DNS_TYPE_AAAA,
    MultiPortProxyWorker,
    ProxyWorker,
    RoundRobinBalancer,
    _SocksUdpRelayProtocol,
)
from utils.config_manager import _coerce_config, default_config
from utils.socket_binding import IPV6_UNICAST_IF, configure_bound_ipv6_socket


NIC = {"name": "test-nic", "ip": "192.0.2.10", "if_index": 7}


class _Writer:
    def __init__(self, peer=("127.0.0.1", 50000)):
        self.peer = peer
        self.writes = []
        self.closed = False

    def write(self, value):
        self.writes.append(value)

    async def drain(self):
        return None

    def close(self):
        self.closed = True

    def get_extra_info(self, name):
        return self.peer if name == "peername" else None


class ProxyTransportRegressionTests(unittest.IsolatedAsyncioTestCase):
    async def test_doh_off_skips_https_endpoints_without_system_dns_fallback(self):
        worker = MultiPortProxyWorker([NIC], allow_degraded_start=True)
        worker.set_doh_provider("off")
        self.assertEqual(worker._doh_endpoints, ())
        self.assertEqual(default_config()["doh_provider"], "auto")
        self.assertEqual(_coerce_config({"doh_provider": "off"})["doh_provider"], "off")

    async def test_dns_failure_does_not_decrement_an_existing_connection(self):
        worker = MultiPortProxyWorker([NIC], allow_degraded_start=True)
        balancer = RoundRobinBalancer([NIC])
        balancer.on_connect("test-nic")

        async def fail_dns(*_args, **_kwargs):
            raise RuntimeError("selected-interface DNS failed")

        worker._resolve_domain_via_nic = fail_dns
        reader = asyncio.StreamReader()
        reader.feed_data(
            b"\x05\x01\x00"  # greeting
            b"\x05\x01\x00\x03\x0bexample.test\x01\xbb"  # CONNECT example.test:443
        )
        reader.feed_eof()

        await worker._handle_socks(reader, _Writer(), balancer, "test")

        self.assertEqual(balancer.active_connections()["test-nic"], 1)

    async def test_udp_domain_resolution_uses_selected_nic_and_can_return_aaaa(self):
        worker = MultiPortProxyWorker([NIC], allow_degraded_start=True)
        worker._doh_endpoints = ()
        worker._dns_servers_for_nic = lambda _nic: ("192.0.2.53",)
        requested_types = []

        async def fake_dns(_domain, _nic, _loop, _server, record_type=DNS_TYPE_A):
            requested_types.append(record_type)
            if record_type == DNS_TYPE_A:
                raise ValueError("no A record")
            return "2001:db8::53"

        worker._query_dns_udp = fake_dns
        worker._query_dns_tcp = fake_dns
        value = await worker._resolve_domain_via_nic(
            "ipv6-only.example", NIC, asyncio.get_running_loop(),
        )

        self.assertEqual(value, "2001:db8::53")
        self.assertEqual(requested_types, [DNS_TYPE_A, DNS_TYPE_A, DNS_TYPE_AAAA])

    async def test_udp_relay_rejects_a_different_local_client(self):
        calls = []

        class Owner:
            _client_tasks = set()

            async def _handle_udp_packet(self, *_args):
                calls.append(_args)

        protocol = _SocksUdpRelayProtocol(
            Owner(), RoundRobinBalancer([NIC]), "test", "127.0.0.1",
        )
        protocol.datagram_received(b"ignored", ("127.0.0.2", 30000))
        await asyncio.sleep(0)
        self.assertEqual(calls, [])

        protocol.datagram_received(b"accepted", ("127.0.0.1", 30001))
        await asyncio.sleep(0)
        self.assertEqual(len(calls), 1)


class IPv6TransportTests(unittest.TestCase):
    def test_socks_udp_ipv6_header_and_http_authority_parsing(self):
        packet = MultiPortProxyWorker._pack_socks5_udp_header(
            "2001:db8::1", 443, b"payload",
        )
        self.assertEqual(packet[:4], b"\x00\x00\x00\x04")
        self.assertEqual(socket.inet_ntop(socket.AF_INET6, packet[4:20]), "2001:db8::1")
        self.assertEqual(struct.unpack("!H", packet[20:22])[0], 443)
        self.assertEqual(ProxyWorker._split_host_port("[2001:db8::1]:8443", 443), ("2001:db8::1", 8443))

    def test_ipv6_interface_binding_uses_host_order_ifindex(self):
        class Socket:
            def __init__(self):
                self.value = None

            def setsockopt(self, _level, _option, value):
                self.value = value

            def getsockopt(self, _level, _option):
                return self.value

        sock = Socket()
        configure_bound_ipv6_socket(sock, {"name": "v6", "if_index": 19}, "test")
        self.assertEqual(IPV6_UNICAST_IF, 31)
        self.assertEqual(sock.value, 19)


if __name__ == "__main__":
    unittest.main()
