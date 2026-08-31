//go:build darwin

package proxy

import (
	"net"
	"testing"
	"time"
)

func TestDarwinBoundDialerPinsSourceAndInterface(t *testing.T) {
	dialer, err := boundNetworkDialer(Adapter{
		Name: "Wi-Fi", SourceIP: "192.0.2.10", IfIndex: 7,
	}, time.Second, "tcp4")
	if err != nil {
		t.Fatal(err)
	}
	local, ok := dialer.LocalAddr.(*net.TCPAddr)
	if !ok || !local.IP.Equal(net.ParseIP("192.0.2.10")) {
		t.Fatalf("local address = %#v", dialer.LocalAddr)
	}
	if dialer.Control == nil {
		t.Fatal("Darwin dialer did not install IP_BOUND_IF control")
	}
}

func TestDarwinBoundDialerRejectsMissingIPv6Source(t *testing.T) {
	if _, err := boundNetworkDialer(Adapter{Name: "Wi-Fi", SourceIP: "192.0.2.10"}, time.Second, "udp6"); err == nil {
		t.Fatal("expected missing IPv6 source to be rejected")
	}
}
