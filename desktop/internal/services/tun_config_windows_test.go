//go:build windows

package services

import (
	"encoding/json"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

func TestGeneratedTunConfigPassesBundledSingBoxCheck(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	endpoints := map[string]string{
		"nic_ethernet": "127.0.0.1:19101",
		"nic_wifi":     "127.0.0.1:19102",
		"aggregation":  "127.0.0.1:19103",
		"direct":       "127.0.0.1:19104",
	}
	executable, configPath, clashAPI, err := writeSingBoxConfig(
		endpoints,
		AdapterView{Name: "Ethernet", Address: "192.0.2.10"},
		dnsResolveResult{Transport: "udp", Server: "1.1.1.1"},
		nil,
		normalizedCompatibilityPlan(nil, nil),
		true,
	)
	if err != nil {
		t.Fatalf("writeSingBoxConfig() failed: %v", err)
	}
	if clashAPI.Endpoint == "" || clashAPI.Secret == "" {
		t.Fatalf("clash API config = %#v", clashAPI)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("generated config is invalid JSON: %v", err)
	}
	command := exec.Command(executable, "check", "--disable-color", "-c", configPath)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("sing-box check failed: %v\n%s", err, output)
	}
}

func TestTunRouteResolvesFakeIPBeforeUserCIDRRules(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	endpoints := map[string]string{
		"nic_ethernet": "127.0.0.1:19201",
		"nic_wifi":     "127.0.0.1:19202",
		"aggregation":  "127.0.0.1:19203",
	}
	_, configPath, _, err := writeSingBoxConfig(
		endpoints,
		AdapterView{Name: "Ethernet", Address: "192.0.2.10"},
		dnsResolveResult{Transport: "udp", Server: "1.1.1.1"},
		[]RoutingRule{{MatchType: MatchIP, Value: "203.0.113.0/24", Outbound: "direct"}},
		compatibilityPlan{ProcessNames: []string{"mihomo.exe"}},
		true,
	)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Route struct {
			Rules []map[string]any `json:"rules"`
		} `json:"route"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	resolveAt, cidrAt, compatibilityAt := -1, -1, -1
	for index, rule := range config.Route.Rules {
		if rule["action"] == "resolve" {
			resolveAt = index
		}
		if _, ok := rule["ip_cidr"]; ok {
			cidrAt = index
		}
		if names, ok := rule["process_name"].([]any); ok {
			for _, name := range names {
				if name == "mihomo.exe" {
					compatibilityAt = index
				}
			}
		}
	}
	if compatibilityAt < 0 || resolveAt <= compatibilityAt || cidrAt <= resolveAt {
		t.Fatalf("unexpected compatibility/resolve/user order: compatibility=%d resolve=%d cidr=%d", compatibilityAt, resolveAt, cidrAt)
	}
}

func TestGeneratedTunConfigProtectsLiteralDNSBootstrapAndOmitsIPv6WhenUnavailable(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	endpoints := map[string]string{
		"nic_ethernet": "127.0.0.1:19301",
		"nic_wifi":     "127.0.0.1:19302",
		"aggregation":  "127.0.0.1:19303",
	}
	_, configPath, _, err := writeSingBoxConfigWithOptions(
		endpoints,
		AdapterView{Name: "以太网", Address: "192.0.2.10"},
		dnsResolveResult{Transport: "doh", Server: "dns.example@223.5.5.5:443"},
		nil,
		normalizedCompatibilityPlan(nil, nil),
		true,
		tunConfigOptions{DNSPolicy: "auto", IPv6Available: false},
	)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		DNS struct {
			Servers []map[string]any `json:"servers"`
		} `json:"dns"`
		Inbounds []struct {
			Address             []string `json:"address"`
			RouteExcludeAddress []string `json:"route_exclude_address"`
		} `json:"inbounds"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	if len(config.Inbounds) != 1 {
		t.Fatalf("inbounds = %#v", config.Inbounds)
	}
	if !reflect.DeepEqual(config.Inbounds[0].Address, []string{"172.19.0.1/30"}) {
		t.Fatalf("IPv4-only TUN address = %#v", config.Inbounds[0].Address)
	}
	if !reflect.DeepEqual(config.Inbounds[0].RouteExcludeAddress, []string{"223.5.5.5/32"}) {
		t.Fatalf("DNS route exclusions = %#v", config.Inbounds[0].RouteExcludeAddress)
	}
	if len(config.DNS.Servers) != 2 || config.DNS.Servers[1]["type"] != "fakeip" {
		t.Fatalf("auto policy fakeip server = %#v", config.DNS.Servers)
	}
	if strings.Contains(string(data), "fdfe:dcba:9876") {
		t.Fatal("IPv6 TUN address was emitted for an IPv4-only host")
	}
}

func TestIPv4FallbackConfigReusesClashAPIIdentity(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	endpoints := map[string]string{
		"nic_ethernet": "127.0.0.1:19311",
		"nic_wifi":     "127.0.0.1:19312",
		"aggregation":  "127.0.0.1:19313",
	}
	adapter := AdapterView{Name: "Ethernet", Address: "192.0.2.10", SourceIPv6: "2001:db8::10"}
	result := dnsResolveResult{Transport: "udp", Server: "192.0.2.53:53"}
	_, _, primaryAPI, err := writeSingBoxConfigWithOptions(
		endpoints, adapter, result, nil, normalizedCompatibilityPlan(nil, nil), true,
		tunConfigOptions{DNSPolicy: "auto", IPv6Available: true, ConfigName: "sing-box.json"},
	)
	if err != nil {
		t.Fatal(err)
	}
	_, fallbackPath, fallbackAPI, err := writeSingBoxConfigWithOptions(
		endpoints, adapter, result, nil, normalizedCompatibilityPlan(nil, nil), true,
		tunConfigOptions{
			DNSPolicy: "auto", IPv6Available: false, ConfigName: "sing-box-ipv4.json",
			ClashAPI: &primaryAPI,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if fallbackAPI != primaryAPI {
		t.Fatalf("fallback API identity = %#v, primary = %#v", fallbackAPI, primaryAPI)
	}
	data, err := os.ReadFile(fallbackPath)
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Experimental struct {
			ClashAPI struct {
				ExternalController string `json:"external_controller"`
				Secret             string `json:"secret"`
			} `json:"clash_api"`
		} `json:"experimental"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	if config.Experimental.ClashAPI.ExternalController != primaryAPI.Endpoint ||
		config.Experimental.ClashAPI.Secret != primaryAPI.Secret {
		t.Fatalf("fallback config changed Clash API identity: %#v vs %#v", config.Experimental.ClashAPI, primaryAPI)
	}
}

func TestGeneratedTunConfigOffPolicyUsesNativeDNSWithoutUnconditionalFakeIP(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	endpoints := map[string]string{
		"nic_ethernet": "127.0.0.1:19401",
		"nic_wifi":     "127.0.0.1:19402",
		"aggregation":  "127.0.0.1:19403",
	}
	executable, configPath, _, err := writeSingBoxConfigWithOptions(
		endpoints,
		AdapterView{Name: "Ethernet", Address: "192.0.2.10", DNSServers: []string{"192.0.2.53"}},
		dnsResolveResult{Transport: "udp", Server: "192.0.2.53:53"},
		nil,
		normalizedCompatibilityPlan(nil, nil),
		true,
		tunConfigOptions{DNSPolicy: "off", IPv6Available: false},
	)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		DNS struct {
			Servers []map[string]any `json:"servers"`
		} `json:"dns"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	if len(config.DNS.Servers) != 1 || config.DNS.Servers[0]["type"] != "udp" {
		t.Fatalf("off policy DNS servers = %#v", config.DNS.Servers)
	}
	if strings.Contains(string(data), "fakeip") || strings.Contains(string(data), "https") {
		t.Fatal("off policy generated encrypted DNS or unconditional FakeIP")
	}
	command := exec.Command(executable, "check", "--disable-color", "-c", configPath)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("off-policy sing-box check failed: %v\n%s", err, output)
	}
}

func TestDNSBootstrapRouteExclusionsNormalizeAllLiteralIPFamilies(t *testing.T) {
	result := dnsBootstrapRouteExclusions(dnsResolveResult{
		Transport: "dot", Server: "resolver@[2001:db8::53]:853",
	})
	if !reflect.DeepEqual(result, []string{"2001:db8::53/128"}) {
		t.Fatalf("IPv6 route exclusion = %#v", result)
	}
}

func TestGeneratedTunConfigSystemPolicyDoesNotInstallDNSHijack(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	endpoints := map[string]string{
		"nic_ethernet": "127.0.0.1:19501",
		"nic_wifi":     "127.0.0.1:19502",
		"aggregation":  "127.0.0.1:19503",
	}
	executable, configPath, _, err := writeSingBoxConfigWithOptions(
		endpoints,
		AdapterView{Name: "Ethernet", Address: "192.0.2.10"},
		dnsResolveResult{},
		nil,
		normalizedCompatibilityPlan(nil, nil),
		true,
		tunConfigOptions{DNSPolicy: "system", IPv6Available: false},
	)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "hijack-dns") || strings.Contains(string(data), "fakeip") {
		t.Fatal("system policy installed DNS interception")
	}
	command := exec.Command(executable, "check", "--disable-color", "-c", configPath)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("system-policy sing-box check failed: %v\n%s", err, output)
	}
}
