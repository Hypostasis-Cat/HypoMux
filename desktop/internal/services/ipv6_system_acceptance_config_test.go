package services

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Explicit offline preparation for the separately elevated system test.
// This uses the production config writer and never starts TUN or changes proxy.
func TestPrepareIPv6SystemAcceptanceConfig(t *testing.T) {
	inputPath := os.Getenv("HYPOMUX_IPV6_TUN_PREPARE_INPUT")
	if inputPath == "" {
		t.Skip("provide an explicit IPv6 system acceptance preparation input")
	}
	data, err := os.ReadFile(inputPath)
	if err != nil {
		t.Fatal(err)
	}
	var input struct {
		Adapter   AdapterView       `json:"adapter"`
		Endpoints map[string]string `json:"endpoints"`
		Core      string            `json:"core"`
		Output    string            `json:"output"`
	}
	// Windows PowerShell 5.1 writes a UTF-8 BOM with Set-Content -Encoding utf8.
	if err := json.Unmarshal(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf}), &input); err != nil {
		t.Fatal(err)
	}
	if input.Adapter.SourceIPv6 == "" || input.Adapter.Address != "" || input.Output == "" || input.Core == "" {
		t.Fatal("prepare requires an explicit IPv6-only binding, output directory and real Core")
	}
	if err := os.MkdirAll(input.Output, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HYPOMUX_DATA_DIR", input.Output)
	t.Setenv("HYPOMUX_ENGINE_PATH", input.Core)
	var digest string
	executable, configPath, _, err := writeSingBoxConfigWithOptions(
		input.Endpoints, input.Adapter,
		dnsResolveResult{Adapter: input.Adapter.Name, Domain: "dns.alidns.com", RecordType: "AAAA", Address: "2400:3200::1", Transport: "doh", Server: "dns.alidns.com@[2400:3200::1]:443"},
		nil, detectCompatibilityPlan(), true,
		tunConfigOptions{DNSPolicy: "alidns", IPv6Available: true, IPv4Unavailable: true, Stack: "system", ConfigSHA256: &digest},
	)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := json.MarshalIndent(map[string]any{"executable": executable, "config_path": configPath, "config_sha256": digest, "strict_route": true, "startup_timeout_ms": 20000}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(input.Output, "activation.json"), manifest, 0600); err != nil {
		t.Fatal(err)
	}
	t.Log("prepared production IPv6-only source configuration; system TUN has not started")
}
