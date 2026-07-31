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
