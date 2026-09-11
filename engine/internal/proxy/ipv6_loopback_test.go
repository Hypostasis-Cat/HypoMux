package proxy

import (
	"net"
	"testing"
	"time"
)

// Check TCP connection establishment and bidirectional UDP I/O independently
// of the proxy. Windows may allow listening on ::1 while blocking connections.
func requireIPv6Loopback(t *testing.T, network string) {
	t.Helper()
	if network == "tcp6" {
		listener, err := net.Listen("tcp6", "[::1]:0")
		if err != nil {
			t.Skipf("IPv6 loopback unavailable: %v", err)
		}
		defer listener.Close()
		dialer := net.Dialer{Timeout: time.Second, LocalAddr: &net.TCPAddr{IP: net.IPv6loopback}}
		client, err := dialer.Dial("tcp6", listener.Addr().String())
		if err != nil {
			t.Skipf("IPv6 loopback connect unavailable: %v", err)
		}
		defer client.Close()
		server, err := listener.Accept()
		if err != nil {
			t.Fatal(err)
		}
		defer server.Close()
		return
	}
	server, err := net.ListenUDP("udp6", &net.UDPAddr{IP: net.IPv6loopback})
	if err != nil {
		t.Skipf("IPv6 UDP loopback unavailable: %v", err)
	}
	defer server.Close()
	client, err := net.DialUDP("udp6", &net.UDPAddr{IP: net.IPv6loopback}, server.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Skipf("IPv6 UDP loopback connect unavailable: %v", err)
	}
	defer client.Close()
	_ = client.SetDeadline(time.Now().Add(time.Second))
	_ = server.SetDeadline(time.Now().Add(time.Second))
	if _, err := client.Write([]byte{1}); err != nil {
		t.Skipf("IPv6 UDP loopback send unavailable: %v", err)
	}
	buffer := make([]byte, 1)
	_, peer, err := server.ReadFromUDP(buffer)
	if err != nil {
		t.Skipf("IPv6 UDP loopback receive unavailable: %v", err)
	}
	if _, err := server.WriteToUDP(buffer, peer); err != nil {
		t.Skipf("IPv6 UDP loopback reply unavailable: %v", err)
	}
	if _, err := client.Read(buffer); err != nil {
		t.Skipf("IPv6 UDP loopback reply unavailable: %v", err)
	}
}
