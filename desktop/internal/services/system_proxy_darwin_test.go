//go:build darwin

package services

import (
	"reflect"
	"testing"
)

func TestParseDarwinNetworkServicesSkipsDisabled(t *testing.T) {
	output := "An asterisk (*) denotes that a network service is disabled.\nWi-Fi\n*Old Ethernet\niPhone USB\n"
	want := []string{"Wi-Fi", "iPhone USB"}
	if got := parseDarwinNetworkServices(output); !reflect.DeepEqual(got, want) {
		t.Fatalf("parse services = %#v, want %#v", got, want)
	}
}

func TestParseDarwinProxyEndpoint(t *testing.T) {
	got := parseDarwinProxyEndpoint("Enabled: Yes\nServer: 127.0.0.1\nPort: 10801\nAuthenticated Proxy Enabled: 0\n")
	want := darwinProxyEndpoint{Enabled: true, Server: "127.0.0.1", Port: 10801}
	if got != want {
		t.Fatalf("parse endpoint = %#v, want %#v", got, want)
	}
}

func TestOwnedDarwinProxyRequiresEveryEndpoint(t *testing.T) {
	service := darwinServiceProxy{
		Web:       darwinProxyEndpoint{Enabled: true, Server: "127.0.0.1", Port: 10801},
		SecureWeb: darwinProxyEndpoint{Enabled: true, Server: "127.0.0.1", Port: 10801},
		SOCKS:     darwinProxyEndpoint{Enabled: true, Server: "127.0.0.1", Port: 10800},
	}
	if !isOwnedDarwinProxy(service, 10801, 10800) {
		t.Fatal("expected exact HypoMux proxy ownership")
	}
	service.SOCKS.Enabled = false
	if isOwnedDarwinProxy(service, 10801, 10800) {
		t.Fatal("a user-modified proxy must not be considered owned")
	}
}

func TestShellQuote(t *testing.T) {
	if got, want := shellQuote("Bob's Wi-Fi"), `'Bob'\''s Wi-Fi'`; got != want {
		t.Fatalf("shellQuote = %q, want %q", got, want)
	}
}
