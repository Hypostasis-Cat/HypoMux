//go:build windows

package services

import (
	"encoding/json"
	"os"
	"os/exec"
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
