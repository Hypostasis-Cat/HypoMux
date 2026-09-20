//go:build windows

package services

import (
	"encoding/json"
	"os"
	"testing"
)

func TestRejectRoutingSaveAndBackup(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	settings := NewSettingsService()
	service := NewRoutingRuleService(settings, NewAdapterService(settings), nil)
	rules := []RoutingRule{
		{MatchType: MatchProcess, Value: "blocked.exe", Outbound: OutboundReject},
		{MatchType: MatchDomain, Value: "example.com", Outbound: OutboundReject},
		{MatchType: MatchIP, Value: "203.0.113.0/24", Outbound: OutboundReject},
	}
	snapshot, err := service.Save(rules)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, outbound := range snapshot.Outbounds {
		found = found || outbound.ID == OutboundReject
	}
	if !found {
		t.Fatal("reject missing from available choices")
	}
	data, err := json.Marshal(map[string]any{
		"format": RoutingBackupFormat, "version": RoutingBackupVersion,
		"rules": NewSettingsService().Get().RoutingRules,
	})
	if err != nil {
		t.Fatal(err)
	}
	restored, err := parseRoutingBackup(data)
	if err != nil || len(restored) != 3 {
		t.Fatalf("restore = %v, %v", restored, err)
	}
	for _, rule := range restored {
		if rule.Outbound != OutboundReject {
			t.Fatalf("lost reject: %+v", rule)
		}
	}
}

func TestRejectRoutingOldManifestRequiresRestart(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	if _, err := writeSingBoxRuleSetPlan(nil, []string{"aggregation", "direct"}, true); err != nil {
		t.Fatal(err)
	}
	rules := []RoutingRule{{MatchType: MatchProcess, Value: "app.exe", Outbound: OutboundReject}}
	if err := refreshSingBoxRuleSets(rules); err != nil {
		t.Fatal(err)
	}
	if restart, reason := singBoxRuleSetRestartRequirement(rules); !restart || reason != "outbound_changed" {
		t.Fatalf("old configuration must require restart: %v %s", restart, reason)
	}
}

func TestRejectRoutingPriorityAndHotReload(t *testing.T) {
	for _, policy := range []string{"auto", "off", "system"} {
		t.Run(policy, func(t *testing.T) {
			t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
			exe, path, _, err := writeSingBoxConfigWithOptions(
				map[string]string{"nic_ethernet": "127.0.0.1:19101", "nic_wifi": "127.0.0.1:19102", "aggregation": "127.0.0.1:19103"},
				AdapterView{}, dnsResolveResult{Transport: "udp", Server: "1.1.1.1"}, nil,
				compatibilityPlan{ProcessNames: []string{"proxy.exe"}}, true, tunConfigOptions{DNSPolicy: policy},
			)
			if err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			rules := []RoutingRule{
				{MatchType: MatchProcess, Value: "blocked.exe", Outbound: OutboundReject},
				{MatchType: MatchProcess, Value: "allowed.exe", Outbound: "direct"},
				{MatchType: MatchDomain, Value: "example.com", Outbound: OutboundReject},
				{MatchType: MatchDomain, Value: "allowed.example.com", Outbound: "direct"},
				{MatchType: MatchIP, Value: "203.0.113.0/24", Outbound: OutboundReject, Priority: 10},
				{MatchType: MatchIP, Value: "2001:db8::/32", Outbound: OutboundReject, Priority: 10},
				{MatchType: MatchIP, Value: "203.0.113.7/32", Outbound: "direct", Priority: 20},
			}
			if err := refreshSingBoxRuleSets(rules); err != nil {
				t.Fatal(err)
			}
			checkSingBoxConfig(t, exe, path)
			// Domain additions can still require enabling FakeIP under DNS off;
			// adding the reserved reject action itself never requires a restart.
			if restart, reason := singBoxRuleSetRestartRequirement(rules); restart && reason != "enable_fakeip" {
				t.Fatalf("unexpected restart: %s", reason)
			}
			cases := []struct {
				flow priorityFlow
				want string
			}{
				{priorityFlow{process: "blocked.exe", ip: "192.0.2.1"}, OutboundReject},
				{priorityFlow{domain: "sub.example.com", ip: "192.0.2.1"}, OutboundReject},
				{priorityFlow{process: "allowed.exe", domain: "example.com", ip: "192.0.2.1"}, "direct"},
				{priorityFlow{domain: "allowed.example.com", ip: "192.0.2.1"}, "direct"},
				{priorityFlow{process: "proxy.exe", domain: "example.com", ip: "192.0.2.1"}, OutboundReject},
				{priorityFlow{process: "proxy.exe", ip: "203.0.113.8"}, OutboundReject},
				{priorityFlow{ip: "2001:db8::1"}, OutboundReject},
				{priorityFlow{ip: "203.0.113.7"}, "direct"},
				{priorityFlow{ip: "192.0.2.1"}, "aggregation"},
			}
			for _, tc := range cases {
				if got := priorityRouteFor(t, path, tc.flow); got != tc.want {
					t.Errorf("%+v: got %s, want %s", tc.flow, got, tc.want)
				}
			}
			rules[0].Disabled = true
			if err := refreshSingBoxRuleSets(rules); err != nil {
				t.Fatal(err)
			}
			if got := priorityRouteFor(t, path, cases[0].flow); got != "aggregation" {
				t.Fatalf("disabled reject: %s", got)
			}
			if err := refreshSingBoxRuleSets(nil); err != nil {
				t.Fatal(err)
			}
			if got := priorityRouteFor(t, path, cases[4].flow); got != "system-direct" {
				t.Fatalf("removed reject: %s", got)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(before) != string(after) {
				t.Fatal("hot reload rewrote main config")
			}
			checkSingBoxConfig(t, exe, path)
		})
	}
}
