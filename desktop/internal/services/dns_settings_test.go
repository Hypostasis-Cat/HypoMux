package services

import (
	"reflect"
	"testing"
)

func TestCustomDNSListsPersistAndRemainIsolated(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	service := NewSettingsService()
	next := service.Get()
	next.DNSServers = []string{" 1.1.1.1 ", "2001:4860:4860::8888", "1.1.1.1"}
	next.DoHServers = []string{"https://DNS.Example.com:8443/custom?key=hello", "https://dns.google"}
	next.DNSPolicy = "custom"
	saved, err := service.UpdateFields(next, []string{"dns_servers", "doh_servers", "dns_policy"})
	if err != nil {
		t.Fatal(err)
	}
	if saved.DNSServer != "1.1.1.1" || len(saved.DNSServers) != 2 || saved.DoHServers[1] != "https://dns.google/dns-query" {
		t.Fatalf("not normalized: %+v", saved)
	}
	saved.DNSServers[0] = "9.9.9.9"
	saved.DoHServers[0] = "https://wrong.example/dns-query"
	reloaded := NewSettingsService().Get()
	if !reflect.DeepEqual(service.Get(), reloaded) {
		t.Fatal("lists changed through Get or were not persisted")
	}
	next = service.Get()
	next.DNSServer = "8.8.8.8"
	updated, err := service.UpdateFields(next, []string{"dns_server"})
	if err != nil || updated.DNSServers[0] != "8.8.8.8" || len(updated.DNSServers) != 2 {
		t.Fatalf("legacy field edit lost list: %+v %v", updated, err)
	}
}

func TestCustomDNSSettingsRejectInvalidEntries(t *testing.T) {
	for _, address := range []string{"", "0.0.0.0", "224.0.0.1", "fe80::1", "example.com"} {
		settings := DefaultSettings()
		settings.DNSServers = []string{"1.1.1.1", address}
		if err := validateSettings(settings); err == nil {
			t.Errorf("accepted DNS %q", address)
		}
	}
	for _, address := range []string{"", "http://dns.example/query", "https://user:pass@dns.example/query", "https://dns.example/#fragment", "https://dns.example:99999/", "https://0.0.0.0/query"} {
		settings := DefaultSettings()
		settings.DoHServers = []string{address}
		if err := validateSettings(settings); err == nil {
			t.Errorf("accepted DoH %q", address)
		}
	}
	settings := DefaultSettings()
	settings.DNSPolicy = "custom"
	if err := validateSettings(settings); err == nil {
		t.Fatal("accepted empty custom DoH")
	}
}

func TestTUNCustomDoHPreservesPortAndPath(t *testing.T) {
	config := tunDNSConfiguration{Policy: "custom", DoHEndpoints: []tunDoHEndpoint{{IP: "1.1.1.1", Host: "dns.example.com", Port: 8443, Path: "/custom/query"}}}
	adapter := AdapterView{Name: "Ethernet", Address: "192.0.2.10"}
	result, err := configuredTUNDNS(config, adapter)
	if err != nil {
		t.Fatal(err)
	}
	upstream, err := buildDNSUpstreamForPolicy(adapter, result, "custom")
	if err != nil {
		t.Fatal(err)
	}
	if upstream["server_port"] != 8443 || upstream["path"] != "/custom/query" {
		t.Fatalf("custom endpoint lost: %+v", upstream)
	}
}

func TestTUNCustomDoHRelayPreservesSourceBindingInCore(t *testing.T) {
	result := dnsResolveResult{Transport: "doh", Server: "example.com@192.0.2.53:8443", DoHPath: "/custom%2Fquery?key=hello", RelayAddress: "127.0.0.1:19153"}
	upstream, err := buildDNSUpstreamForPolicy(AdapterView{Name: "Ethernet", Address: "192.0.2.10"}, result, "custom")
	if err != nil || upstream["type"] != "udp" || upstream["server"] != "127.0.0.1" || upstream["server_port"] != 19153 || upstream["bind_interface"] != nil || upstream["inet4_bind_address"] != nil {
		t.Fatalf("Core relay config = %+v, %v", upstream, err)
	}
	result.RelayAddress = "192.0.2.55:19153"
	if _, err := buildDNSUpstreamForPolicy(AdapterView{}, result, "custom"); err == nil {
		t.Fatal("accepted a non-loopback relay")
	}
}

func TestTUNDNSPoolKeepsDNS64PreferenceAndEncryptedFallback(t *testing.T) {
	adapter := AdapterView{SourceIPv6: "2001:db8::10", DNSServers: []string{"2001:db8::53"}}
	config := tunDNSConfiguration{Policy: "auto", LegacyServers: []string{"2001:db8::53", "2001:4860:4860::8888"}}
	encrypted := []dnsResolveResult{{Transport: "doh", Server: "dns.google@[2001:4860:4860::8888]:443"}}
	pool, err := orderTUNDNSPool(adapter, config, encrypted)
	if err != nil || len(pool) != 5 || pool[0].Transport != "udp" || pool[0].Server != "[2001:db8::53]:53" || pool[1].Transport != "tcp" || pool[2].Transport != "doh" || pool[3].Server != "[2001:4860:4860::8888]:53" {
		t.Fatalf("DNS64 order changed: %+v %v", pool, err)
	}
	config.Policy = "custom"
	pool, err = orderTUNDNSPool(adapter, config, encrypted)
	if err != nil || len(pool) != 1 || pool[0].Transport != "doh" {
		t.Fatal("strict policy acquired plaintext fallback")
	}
}
