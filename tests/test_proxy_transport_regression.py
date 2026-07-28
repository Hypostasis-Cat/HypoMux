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
    TCP_RELAY_DRAIN_THRESHOLD,
    _SocksUdpRelayProtocol,
    _write_with_bounded_backpressure,
)
from utils.config_manager import _coerce_config, default_config
from utils.network_utils import find_shared_ipv4_gateway_risks
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
    async def test_tcp_relay_batches_drain_until_backpressure_threshold(self):
        class Transport:
            def __init__(self):
                self.size = 0

            def get_write_buffer_size(self):
                return self.size

        class Writer:
            def __init__(self):
                self.transport = Transport()
                self.drains = 0

            def write(self, data):
                self.transport.size += len(data)

            async def drain(self):
                self.drains += 1
                self.transport.size = 0

        writer = Writer()
        await _write_with_bounded_backpressure(writer, b"x" * 1024)
        self.assertEqual(writer.drains, 0)

        writer.transport.size = TCP_RELAY_DRAIN_THRESHOLD - 512
        await _write_with_bounded_backpressure(writer, b"x" * 1024)
        self.assertEqual(writer.drains, 1)

    async def test_doh_off_skips_https_endpoints_without_system_dns_fallback(self):
        worker = MultiPortProxyWorker([NIC], allow_degraded_start=True)
        worker.set_doh_provider("off")
        self.assertEqual(worker._doh_endpoints, ())
        self.assertEqual(default_config()["doh_provider"], "auto")
        self.assertEqual(_coerce_config({"doh_provider": "off"})["doh_provider"], "off")

    async def test_doh_strict_never_falls_back_to_port_53(self):
        worker = MultiPortProxyWorker([NIC], allow_degraded_start=True)
        worker._dns_mode = worker.DNS_MODE_DOH_STRICT
        calls = []

        async def fail_doh(*_args, **_kwargs):
            raise OSError("DoH unavailable")

        async def legacy_dns(*_args, **_kwargs):
            calls.append(True)
            return "192.0.2.53"

        worker._query_dns_doh = fail_doh
        worker._query_dns_udp = legacy_dns
        worker._query_dns_tcp = legacy_dns

        with self.assertRaises(RuntimeError):
            await worker._resolve_domain_uncached(
                "strict.example", NIC, asyncio.get_running_loop(),
            )
        self.assertEqual(calls, [])

    async def test_one_verified_doh_egress_is_enough_for_singbox_owned_dns(self):
        second_nic = {"name": "backup", "ip": "192.0.2.11", "if_index": 8}
        worker = MultiPortProxyWorker([NIC, second_nic])

        async def fake_preflight(nic, _loop):
            if nic["name"] == "test-nic":
                return True, True, "test-nic: DoH=ok; legacy=ok", {
                    "doh": {
                        "mode": "doh",
                        "server": "223.5.5.5",
                        "tls_server_name": "dns.alidns.com",
                        "path": "/dns-query",
                        "bind_interface": "test-nic",
                        "bind_ip": "192.0.2.10",
                    }
                }
            return False, True, "backup: DoH=failed; legacy=ok"

        worker._preflight_dns_for_nic = fake_preflight
        ready, details = await worker._preflight_selected_nics_dns()

        self.assertTrue(ready)
        self.assertEqual(worker.dns_mode(), worker.DNS_MODE_DOH_STRICT)
        self.assertTrue(worker._legacy_dns_verified)
        self.assertEqual(worker.singbox_dns_plan()["bind_interface"], "test-nic")
        self.assertIn("sing-box DoH", details[-1])

    async def test_tun_pool_rejects_unresolved_socks_domain_without_python_dns(self):
        worker = MultiPortProxyWorker(
            [NIC],
            allow_degraded_start=True,
            resolve_socks_domains=False,
        )
        calls = []

        async def forbidden_resolver(*_args, **_kwargs):
            calls.append(True)
            return "192.0.2.53"

        worker._resolve_domain_via_nic = forbidden_resolver
        reader = asyncio.StreamReader()
        reader.feed_data(
            b"\x05\x01\x00"
            b"\x05\x01\x00\x03\x0bexample.test\x01\xbb"
        )
        reader.feed_eof()
        writer = _Writer()

        await worker._handle_socks(
            reader, writer, RoundRobinBalancer([NIC]), "test",
        )

        self.assertEqual(calls, [])
        self.assertIn(b"\x05\x04\x00\x01\x00\x00\x00\x00\x00\x00", writer.writes)

    async def test_singbox_doh_health_monitor_requests_legacy_restart(self):
        worker = MultiPortProxyWorker([NIC], allow_degraded_start=True)
        worker.DNS_HEALTH_INTERVAL = 0.001
        worker.DNS_HEALTH_FAILURE_LIMIT = 2
        worker._dns_egress_plan = {
            "mode": "doh",
            "server": "223.5.5.5",
            "tls_server_name": "dns.alidns.com",
            "path": "/dns-query",
            "bind_interface": "test-nic",
            "bind_ip": "192.0.2.10",
        }

        def fail_doh(*_args, **_kwargs):
            raise OSError("DoH unavailable")

        worker._query_dns_doh_sync = fail_doh
        await asyncio.wait_for(
            worker._monitor_singbox_doh_upstream(),
            timeout=0.2,
        )

        self.assertEqual(worker._doh_failure_count, 2)
        self.assertTrue(worker._doh_compatibility_signal_emitted)

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
        worker.set_doh_provider("off")
        worker._dns_mode = worker.DNS_MODE_LEGACY_COMPAT
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

    async def test_udp_packets_reuse_one_stable_physical_flow(self):
        class EchoProtocol(asyncio.DatagramProtocol):
            def connection_made(self, transport):
                self.transport = transport

            def datagram_received(self, data, addr):
                self.transport.sendto(data, addr)

        loop = asyncio.get_running_loop()
        echo_transport, _ = await loop.create_datagram_endpoint(
            EchoProtocol,
            local_addr=("127.0.0.1", 0),
        )
        echo_port = echo_transport.get_extra_info("sockname")[1]

        worker = MultiPortProxyWorker(
            [NIC],
            allow_degraded_start=True,
            resolve_socks_domains=False,
        )
        socket_creations = []

        def create_udp_socket(_nic, family=socket.AF_INET):
            socket_creations.append(family)
            sock = socket.socket(family, socket.SOCK_DGRAM)
            sock.setblocking(False)
            return sock

        worker._create_bound_udp_socket = create_udp_socket

        class RelayTransport:
            def __init__(self):
                self.packets = []

            def sendto(self, data, addr):
                self.packets.append((data, addr))

        balancer = RoundRobinBalancer([NIC])
        protocol = _SocksUdpRelayProtocol(
            worker, balancer, "test", "127.0.0.1",
        )
        protocol.transport = RelayTransport()
        client_addr = ("127.0.0.1", 30001)

        def request(payload):
            return (
                b"\x00\x00\x00\x01"
                + socket.inet_aton("127.0.0.1")
                + struct.pack("!H", echo_port)
                + payload
            )

        try:
            await worker._handle_udp_packet(
                request(b"one"), client_addr, balancer, "test", protocol,
            )
            await worker._handle_udp_packet(
                request(b"two"), client_addr, balancer, "test", protocol,
            )
            for _ in range(20):
                if len(protocol.transport.packets) >= 2:
                    break
                await asyncio.sleep(0.01)

            self.assertEqual(len(socket_creations), 1)
            self.assertEqual(len(protocol.flows), 1)
            self.assertEqual(
                [packet[-3:] for packet, _addr in protocol.transport.packets],
                [b"one", b"two"],
            )
            self.assertEqual(balancer.active_connections()["test-nic"], 1)
        finally:
            protocol.close_flows()
            echo_transport.close()
            await asyncio.sleep(0)

        self.assertEqual(balancer.active_connections()["test-nic"], 0)


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


class AdapterTopologyRiskTests(unittest.TestCase):
    def test_same_subnet_and_gateway_is_advisory_risk(self):
        risks = find_shared_ipv4_gateway_risks([
            {"name": "Ethernet", "ip": "192.168.1.118", "prefix_length": 24, "gateway": "192.168.1.1"},
            {"name": "WLAN", "ip": "192.168.1.105", "prefix_length": 24, "gateway": "192.168.1.1"},
        ])
        self.assertEqual(len(risks), 1)
        self.assertIn("192.168.1.0/24", risks[0])

    def test_different_gateway_or_subnet_is_not_reported(self):
        risks = find_shared_ipv4_gateway_risks([
            {"name": "Ethernet", "ip": "192.168.1.118", "prefix_length": 24, "gateway": "192.168.1.1"},
            {"name": "WLAN", "ip": "192.168.9.105", "prefix_length": 24, "gateway": "192.168.9.1"},
        ])
        self.assertEqual(risks, [])


if __name__ == "__main__":
    unittest.main()
