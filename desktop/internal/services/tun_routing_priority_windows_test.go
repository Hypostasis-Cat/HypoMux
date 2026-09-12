//go:build windows

package services

import (
	"encoding/json"
	"net"
	"os"
	"strings"
	"testing"
)

func nestedRouteValues(rule map[string]any, key string) []any {
	values, _ := rule[key].([]any)
	result := append([]any{}, values...)
	children, _ := rule["rules"].([]any)
	for _, raw := range children {
		result = append(result, nestedRouteValues(raw.(map[string]any), key)...)
	}
	return result
}

type priorityFlow struct {
	process, path, domain, ip, protocol string
	port                                int
}

// Evaluate the emitted predicates for known connection metadata. This does not
// emulate sniffing, DNS resolution or OS process lookup; the bundled binary's
// check command independently validates the configuration and rule-set schema.
func priorityRuleMatches(t *testing.T, rule map[string]any, sets map[string][]any, flow priorityFlow) bool {
	t.Helper()
	matched := true
	if rule["type"] == "logical" {
		matched = rule["mode"] == "and"
		for _, raw := range rule["rules"].([]any) {
			child := priorityRuleMatches(t, raw.(map[string]any), sets, flow)
			if rule["mode"] == "and" {
				matched = matched && child
			} else {
				matched = matched || child
			}
		}
	} else {
		for key, raw := range rule {
			switch key {
			case "outbound", "action", "timeout", "server", "strategy", "invert":
				continue
			case "domain_suffix":
				continue // Evaluated as an OR with domain below.
			case "domain":
				found := false
				for _, value := range raw.([]any) {
					found = found || strings.EqualFold(flow.domain, value.(string))
				}
				for _, value := range nestedRouteValues(rule, "domain_suffix") {
					found = found || strings.HasSuffix(flow.domain, value.(string))
				}
				matched = matched && found
			case "process_name", "process_path", "protocol":
				actual := map[string]string{"process_name": flow.process, "process_path": flow.path, "protocol": flow.protocol}[key]
				found := false
				for _, value := range raw.([]any) {
					found = found || strings.EqualFold(actual, value.(string))
				}
				matched = matched && found
			case "port":
				found := false
				for _, value := range raw.([]any) {
					found = found || flow.port == int(value.(float64))
				}
				matched = matched && found
			case "ip_cidr":
				found := false
				for _, value := range raw.([]any) {
					_, network, err := net.ParseCIDR(value.(string))
					if err != nil {
						t.Fatal(err)
					}
					found = found || network.Contains(net.ParseIP(flow.ip))
				}
				matched = matched && found
			case "rule_set":
				found := false
				for _, tag := range raw.([]any) {
					entries, ok := sets[tag.(string)]
					if !ok {
						t.Fatalf("missing rule-set %s", tag)
					}
					for _, entry := range entries {
						found = priorityRuleMatches(t, entry.(map[string]any), sets, flow) || found
					}
				}
				matched = matched && found
			default:
				t.Fatalf("unsupported predicate %s", key)
			}
		}
	}
	if rule["invert"] == true {
		return !matched
	}
	return matched
}

func priorityRouteFor(t *testing.T, path string, flow priorityFlow) string {
	t.Helper()
	var config struct {
		Route struct {
			Rules []map[string]any `json:"rules"`
			Final string           `json:"final"`
			Sets  []struct {
				Tag  string `json:"tag"`
				Path string `json:"path"`
			} `json:"rule_set"`
		} `json:"route"`
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	sets := map[string][]any{}
	for _, definition := range config.Route.Sets {
		data, err := os.ReadFile(definition.Path)
		if err != nil {
			t.Fatal(err)
		}
		var source struct {
			Rules []any `json:"rules"`
		}
		if err := json.Unmarshal(data, &source); err != nil {
			t.Fatal(err)
		}
		sets[definition.Tag] = source.Rules
	}
	for _, rule := range config.Route.Rules {
		if rule["action"] == "sniff" || rule["action"] == "resolve" {
			continue
		}
		if priorityRuleMatches(t, rule, sets, flow) {
			if rule["action"] == "hijack-dns" {
				return "hijack-dns"
			}
			return rule["outbound"].(string)
		}
	}
	return config.Route.Final
}

func TestTUNRoutingPriorityAndHotReload(t *testing.T) {
	for _, policy := range []string{"auto", "off", "system"} {
		for _, compat := range []bool{false, true} {
			name := policy + "/plain"
			if compat {
				name = policy + "/compatibility"
			}
			t.Run(name, func(t *testing.T) {
				t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
				plan := compatibilityPlan{}
				if compat {
					plan = compatibilityPlan{ProcessNames: []string{"mihomo.exe"}, ProcessPaths: []string{`C:\Proxy\renamed.exe`}}
				}
				executable, path, _, err := writeSingBoxConfigWithOptions(
					map[string]string{"nic_ethernet": "127.0.0.1:19101", "nic_wifi": "127.0.0.1:19102", "aggregation": "127.0.0.1:19103"},
					AdapterView{}, dnsResolveResult{Transport: "udp", Server: "1.1.1.1"}, nil, plan, true,
					tunConfigOptions{DNSPolicy: policy},
				)
				if err != nil {
					t.Fatal(err)
				}
				before, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				rules, err := normalizeRulesStrict([]RoutingRule{
					{MatchType: MatchIP, Value: "0.0.0.0/0", Outbound: "nic_wifi"},
					{MatchType: MatchIP, Value: "::/0", Outbound: "nic_wifi"},
					{MatchType: MatchIP, Value: "202.118.0.0/16", Outbound: "nic_ethernet"},
					{MatchType: MatchIP, Value: "203.0.113.7/32", Outbound: "direct"},
					{MatchType: MatchProcess, Value: "DeltaForceClient-Win64-Shipping.exe", Outbound: "nic_ethernet"},
					{MatchType: MatchDomain, Value: "example.com", Outbound: "direct"},
				})
				if err != nil {
					t.Fatal(err)
				}
				if err := refreshSingBoxRuleSets(rules); err != nil {
					t.Fatal(err)
				}
				checkSingBoxConfig(t, executable, path)
				cases := []struct {
					name string
					flow priorityFlow
					want string
				}{
					{"game", priorityFlow{process: "DeltaForceClient-Win64-Shipping.exe", ip: "109.244.227.99"}, "nic_ethernet"},
					{"game-v6", priorityFlow{process: "DeltaForceClient-Win64-Shipping.exe", ip: "2001:db8::1"}, "nic_ethernet"},
					{"process-over-domain", priorityFlow{process: "DeltaForceClient-Win64-Shipping.exe", domain: "example.com", ip: "109.244.227.99"}, "nic_ethernet"},
					{"domain", priorityFlow{domain: "www.example.com", ip: "109.244.227.99"}, "direct"},
					{"campus", priorityFlow{ip: "202.118.66.6", port: 443}, "nic_ethernet"},
					{"specific-direct", priorityFlow{ip: "203.0.113.7"}, "direct"},
					{"other", priorityFlow{ip: "109.244.227.99"}, "nic_wifi"},
					{"other-v6", priorityFlow{ip: "2001:db8::1"}, "nic_wifi"},
					{"proxy-pin", priorityFlow{process: "mihomo.exe", ip: "109.244.227.99"}, "nic_wifi"},
					{"proxy-path-pin", priorityFlow{path: `C:\Proxy\renamed.exe`, ip: "109.244.227.99"}, "nic_wifi"},
					{"proxy-domain", priorityFlow{process: "mihomo.exe", domain: "example.com", ip: "109.244.227.99"}, "direct"},
				}
				proxySpecificWant := "direct"
				if compat {
					// Preserve the compatibility bypass when the adapter catch-all
					// is excluded by a more specific non-adapter rule.
					proxySpecificWant = "system-direct"
				}
				cases = append(cases, struct {
					name string
					flow priorityFlow
					want string
				}{"proxy-specific-exclusion", priorityFlow{process: "mihomo.exe", ip: "203.0.113.7"}, proxySpecificWant})
				if policy != "system" {
					for _, flow := range []priorityFlow{
						{process: "DeltaForceClient-Win64-Shipping.exe", ip: "1.1.1.1", port: 53},
						{process: "mihomo.exe", ip: "2001:db8::53", port: 53},
						{path: `C:\Proxy\renamed.exe`, ip: "1.1.1.1", protocol: "dns", port: 5353},
					} {
						cases = append(cases, struct {
							name string
							flow priorityFlow
							want string
						}{"dns", flow, "hijack-dns"})
					}
				} else {
					cases = append(cases, struct {
						name string
						flow priorityFlow
						want string
					}{"system-dns", priorityFlow{ip: "1.1.1.1", port: 53}, "nic_wifi"})
				}
				for _, test := range cases {
					if got := priorityRouteFor(t, path, test.flow); got != test.want {
						t.Errorf("%s: got %s, want %s", test.name, got, test.want)
					}
				}
				// A subnet rule must obey the same priority as a /0 catch-all.
				for i := range rules {
					if rules[i].Value == "0.0.0.0/0" {
						rules[i].Value = "109.244.0.0/16"
					}
				}
				if err := refreshSingBoxRuleSets(rules); err != nil {
					t.Fatal(err)
				}
				if got := priorityRouteFor(t, path, priorityFlow{process: "DeltaForceClient-Win64-Shipping.exe", ip: "109.244.227.99"}); got != "nic_ethernet" {
					t.Fatalf("subnet overrode game process: %s", got)
				}
				// Add a user rule for a compatibility process without rewriting the main config.
				rules = append(rules, RoutingRule{MatchType: MatchProcess, Value: "mihomo.exe", Outbound: "nic_ethernet"})
				if err := refreshSingBoxRuleSets(rules); err != nil {
					t.Fatal(err)
				}
				if got := priorityRouteFor(t, path, priorityFlow{process: "mihomo.exe", domain: "example.com", ip: "109.244.227.99"}); got != "nic_ethernet" {
					t.Fatalf("hot process override = %s", got)
				}
				// Removing user rules restores the compatibility bypass or normal final route.
				if err := refreshSingBoxRuleSets(nil); err != nil {
					t.Fatal(err)
				}
				want := "aggregation"
				if compat {
					want = "system-direct"
				}
				if got := priorityRouteFor(t, path, priorityFlow{process: "mihomo.exe", ip: "109.244.227.99"}); got != want {
					t.Fatalf("removed rules = %s, want %s", got, want)
				}
				after, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if string(before) != string(after) {
					t.Fatal("hot reload rewrote the main configuration")
				}
			})
		}
	}
}

func TestCustomRoutingPriorityAndDisabledRulesHotReload(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	rules := []RoutingRule{
		{MatchType: MatchProcess, Value: "app.exe", Outbound: "nic_wifi"},
		{MatchType: MatchDomain, Value: "example.com", Outbound: "direct", Priority: 20},
		{MatchType: MatchIP, Value: "203.0.113.0/24", Outbound: "aggregation", Priority: 30},
		{MatchType: MatchDomain, Value: "disabled.example", Outbound: "nic_missing", Disabled: true, Priority: 999},
	}
	executable, path, _, err := writeSingBoxConfigWithOptions(
		map[string]string{"nic_ethernet": "127.0.0.1:19101", "nic_wifi": "127.0.0.1:19102", "aggregation": "127.0.0.1:19103"},
		AdapterView{}, dnsResolveResult{Transport: "udp", Server: "1.1.1.1"}, rules,
		compatibilityPlan{ProcessNames: []string{"app.exe"}}, true, tunConfigOptions{DNSPolicy: "auto"},
	)
	if err != nil {
		t.Fatal(err)
	}
	checkSingBoxConfig(t, executable, path)
	flow := priorityFlow{process: "app.exe", domain: "example.com", ip: "203.0.113.7", port: 443}
	if got := priorityRouteFor(t, path, flow); got != "aggregation" {
		t.Fatalf("IP priority got %s", got)
	}
	rules[2].Disabled = true
	if err := refreshSingBoxRuleSets(rules); err != nil {
		t.Fatal(err)
	}
	if got := priorityRouteFor(t, path, flow); got != "direct" {
		t.Fatalf("disabled IP still matches: %s", got)
	}
	rules[0].Priority = 50
	if err := refreshSingBoxRuleSets(rules); err != nil {
		t.Fatal(err)
	}
	if got := priorityRouteFor(t, path, flow); got != "nic_wifi" {
		t.Fatalf("process priority got %s", got)
	}
	rules[0].Disabled = true
	rules[1].Disabled = true
	if err := refreshSingBoxRuleSets(rules); err != nil {
		t.Fatal(err)
	}
	if got := priorityRouteFor(t, path, flow); got != "system-direct" {
		t.Fatalf("disabled rules block compatibility fallback: %s", got)
	}
	if tunDNSNeedsFakeIP("off", rules) {
		t.Fatal("disabled domains still require FakeIP")
	}
	checkSingBoxConfig(t, executable, path)
}

func TestAllTypeOrdersHotReload(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	rules := []RoutingRule{{MatchType: MatchProcess, Value: "app.exe", Outbound: "nic_wifi"}, {MatchType: MatchDomain, Value: "example.com", Outbound: "direct"}, {MatchType: MatchIP, Value: "203.0.113.0/24", Outbound: "aggregation"}}
	exe, path, _, err := writeSingBoxConfigWithOptions(map[string]string{"nic_ethernet": "127.0.0.1:19101", "nic_wifi": "127.0.0.1:19102", "aggregation": "127.0.0.1:19103"}, AdapterView{}, dnsResolveResult{Transport: "udp", Server: "1.1.1.1"}, rules, compatibilityPlan{}, true, tunConfigOptions{DNSPolicy: "auto"})
	if err != nil {
		t.Fatal(err)
	}
	orders := [][]string{{MatchProcess, MatchDomain, MatchIP}, {MatchProcess, MatchIP, MatchDomain}, {MatchDomain, MatchProcess, MatchIP}, {MatchDomain, MatchIP, MatchProcess}, {MatchIP, MatchProcess, MatchDomain}, {MatchIP, MatchDomain, MatchProcess}}
	outbounds := map[string]string{MatchProcess: "nic_wifi", MatchDomain: "direct", MatchIP: "aggregation"}
	for _, order := range orders {
		if err := refreshSingBoxRuleSets(rulesWithMatchOrder(rules, order)); err != nil {
			t.Fatal(err)
		}
		flow := priorityFlow{process: "app.exe", domain: "example.com", ip: "203.0.113.7", port: 443}
		if got := priorityRouteFor(t, path, flow); got != outbounds[order[0]] {
			t.Fatalf("order %v: %s", order, got)
		}
		switch order[0] {
		case MatchProcess:
			flow.process = "other.exe"
		case MatchDomain:
			flow.domain = "other.test"
		case MatchIP:
			flow.ip = "192.0.2.1"
		}
		if got := priorityRouteFor(t, path, flow); got != outbounds[order[1]] {
			t.Fatalf("second type in %v: %s", order, got)
		}
	}
	checkSingBoxConfig(t, exe, path)
}
