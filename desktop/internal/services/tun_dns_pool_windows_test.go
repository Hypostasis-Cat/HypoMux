//go:build windows

package services

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestMultipleTUNDNSUpstreamsPassBundledCheck(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	pool := []dnsResolveResult{
		{Transport: "doh", Server: "dns.example@192.0.2.53:8443", DoHPath: "/custom/query?profile=test", RelayAddress: "127.0.0.1:19153"},
		{Transport: "doh", Server: "dns.google@8.8.8.8:443"},
		{Transport: "udp", Server: "1.1.1.1:53"},
		{Transport: "tcp", Server: "1.1.1.1:53"},
		{Transport: "udp", Server: "9.9.9.9:53"},
	}
	for _, policy := range []string{"auto", "custom", "off"} {
		t.Run(policy, func(t *testing.T) {
			upstreams := pool
			if policy == "custom" {
				upstreams = pool[:2]
			}
			if policy == "off" {
				upstreams = pool[2:]
			}
			executable, path, _, err := writeSingBoxConfigWithOptions(
				map[string]string{"nic_ethernet": "127.0.0.1:19001", "nic_wifi": "127.0.0.1:19002", "aggregation": "127.0.0.1:19003"},
				AdapterView{Address: "192.0.2.10"}, upstreams[0], nil, normalizedCompatibilityPlan(nil, nil), true,
				tunConfigOptions{DNSPolicy: policy, DNSUpstreams: upstreams, ConfigName: "pool-" + policy + ".json"},
			)
			if err != nil {
				t.Fatal(err)
			}
			checkSingBoxConfig(t, executable, path)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var config struct {
				DNS struct {
					Servers []map[string]any
					Rules   []map[string]any
				}
				Route    struct{ Rules []map[string]any }
				Inbounds []struct {
					RouteExcludeAddress []string `json:"route_exclude_address"`
				}
			}
			if err := json.Unmarshal(data, &config); err != nil {
				t.Fatal(err)
			}
			count := len(upstreams)
			if policy != "off" {
				count++
				if !reflect.DeepEqual(config.DNS.Rules[0]["protocol"], []any{"dns"}) {
					t.Fatal("FakeIP must apply only to intercepted DNS, not route resolution")
				}
			}
			if len(config.DNS.Servers) != count {
				t.Fatalf("upstreams = %d, want %d", len(config.DNS.Servers), count)
			}
			for _, rule := range config.Route.Rules {
				if rule["action"] == "resolve" && rule["server"] != nil {
					t.Fatal("route resolve bypasses DNS fallback rules")
				}
			}
			if !strings.Contains(string(data), `"rcode": "SERVFAIL"`) {
				t.Fatal("pool must fail closed after exhausting upstreams")
			}
			for _, result := range upstreams {
				for _, exclusion := range dnsBootstrapRouteExclusions(result) {
					found := false
					for _, actual := range config.Inbounds[0].RouteExcludeAddress {
						if actual == exclusion {
							found = true
						}
					}
					if !found {
						t.Fatalf("missing DNS route exclusion %s", exclusion)
					}
				}
			}
		})
	}
}
