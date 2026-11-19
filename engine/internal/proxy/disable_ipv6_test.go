package proxy

import (
	"context"
	"net"
	"strings"
	"testing"
)

func TestDisableIPv6PolicyRemovesAdapterFamily(t *testing.T) {
	adapters := []Adapter{{Name: "test", SourceIP: "127.0.0.1", SourceIPv6: "2001:db8::1", IPv6IfIndex: 2}}
	c, err := normalizeConfig(Config{DisableIPv6: true, Adapters: adapters})
	if err != nil {
		t.Fatal(err)
	}
	if c.Adapters[0].SourceIPv6 != "" || c.Adapters[0].IPv6IfIndex != 0 || !c.DNS.DisableIPv6 {
		t.Fatal("IPv6 survived policy normalization")
	}
	if adapters[0].SourceIPv6 == "" {
		t.Fatal("caller metadata modified")
	}
	_, err = normalizeConfig(Config{DisableIPv6: true, Adapters: []Adapter{{Name: "v6-only", SourceIPv6: "2001:db8::1"}}})
	if err == nil {
		t.Fatal("IPv6-only egress accepted with IPv6 disabled")
	}
}

func TestDisableIPv6RejectsLiteralBeforeAnySocket(t *testing.T) {
	s, err := New(Config{DisableIPv6: true, Adapters: []Adapter{{Name: "test", SourceIP: "127.0.0.1"}}})
	if err != nil {
		t.Fatal(err)
	}
	s.dialTCP = func(context.Context, *net.Dialer, string) (net.Conn, error) {
		t.Fatal("IPv6 socket attempted")
		return nil, nil
	}
	_, _, err = s.dialUpstream(context.Background(), "[2001:db8::1]:443", s.scheduler, true, false)
	if err == nil || !strings.Contains(err.Error(), "IPv6 is disabled") {
		t.Fatal(err)
	}
	if _, err = s.dialDirectTCP(context.Background(), "[2001:db8::1]:443"); err == nil {
		t.Fatal("direct bypass accepted IPv6")
	}
}
