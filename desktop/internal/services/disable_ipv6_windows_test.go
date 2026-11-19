//go:build windows

package services

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"
)

func TestDisableIPv6GeneratedConfig(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	for _, policy := range []string{"auto", "off", "system"} {
		t.Run(policy, func(t *testing.T) {
			executable, path, _, err := writeSingBoxConfigWithOptions(
				map[string]string{"nic_ethernet": "127.0.0.1:19101", "nic_wifi": "127.0.0.1:19102", "aggregation": "127.0.0.1:19103"},
				AdapterView{Name: "test", Address: "192.0.2.2", SourceIPv6: "2001:db8::2"},
				dnsResolveResult{Transport: "udp", Server: "223.5.5.5:53"}, nil, compatibilityPlan{}, false,
				tunConfigOptions{DisableIPv6: true, IPv6Available: true, DNSPolicy: policy},
			)
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var doc struct {
				DNS struct {
					Strategy string
					Rules    []struct {
						Action    string
						QueryType []string `json:"query_type"`
						RCode     string   `json:"rcode"`
					}
				}
				Route    struct{ Rules []map[string]any }
				Inbounds []struct {
					Address []string
					DNSMode string `json:"dns_mode"`
				}
				Outbounds []map[string]any
			}
			if err = json.Unmarshal(data, &doc); err != nil {
				t.Fatal(err)
			}
			if doc.DNS.Strategy != "ipv4_only" || len(doc.DNS.Rules) == 0 || doc.DNS.Rules[0].Action != "predefined" || doc.DNS.Rules[0].RCode != "NOERROR" || len(doc.DNS.Rules[0].QueryType) != 1 || doc.DNS.Rules[0].QueryType[0] != "AAAA" {
				t.Fatalf("AAAA was not suppressed before other DNS rules: %s", data)
			}
			if doc.Route.Rules[0]["action"] != "reject" || doc.Route.Rules[0]["ip_version"] != float64(6) {
				t.Fatal("IPv6 literal bypass remains")
			}
			if len(doc.Inbounds[0].Address) != 2 {
				t.Fatal("native IPv6 must still enter TUN for explicit rejection")
			}
			for _, out := range doc.Outbounds {
				if out["inet6_bind_address"] != nil {
					t.Fatal("IPv6 source retained")
				}
			}
			checkSingBoxConfig(t, executable, path)
		})
	}
}

// Exercise the actual bundled DNS engine on a loopback UDP listener. No TUN,
// host DNS setting, external resolver or network route is changed by this test.
func TestDisableIPv6ActualDNSReturnsNODATABeforeFakeIP(t *testing.T) {
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	executable, path, _, err := writeSingBoxConfigWithOptions(
		map[string]string{"nic_ethernet": "127.0.0.1:19101", "nic_wifi": "127.0.0.1:19102", "aggregation": "127.0.0.1:19103"},
		AdapterView{}, dnsResolveResult{Transport: "udp", Server: "192.0.2.53:53"}, nil, compatibilityPlan{}, false,
		tunConfigOptions{DisableIPv6: true, DNSPolicy: "auto"},
	)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	var doc map[string]any
	if err = json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	listener, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	endpoint := listener.LocalAddr().String()
	_, portText, _ := net.SplitHostPort(endpoint)
	port, _ := strconv.Atoi(portText)
	listener.Close()
	doc["inbounds"] = []any{map[string]any{"type": "direct", "listen": "127.0.0.1", "listen_port": port, "network": "udp"}}
	doc["outbounds"] = []any{map[string]any{"type": "direct", "tag": "direct"}}
	doc["route"] = map[string]any{"rules": []any{map[string]any{"action": "hijack-dns"}}, "final": "direct", "default_domain_resolver": "dns-local"}
	delete(doc, "experimental")
	data, _ = json.Marshal(doc)
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, "run", "--disable-color", "-c", path)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	query := func(kind byte) ([]byte, error) {
		packet := []byte{0x12, 0x34, 1, 0, 0, 1, 0, 0, 0, 0, 0, 0, 4, 't', 'e', 's', 't', 7, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 0, 0, kind, 0, 1}
		conn, e := net.DialTimeout("udp4", endpoint, 100*time.Millisecond)
		if e != nil {
			return nil, e
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(150 * time.Millisecond))
		if _, e = conn.Write(packet); e != nil {
			return nil, e
		}
		response := make([]byte, 2048)
		n, e := conn.Read(response)
		return response[:n], e
	}
	var response []byte
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		response, err = query(28)
		if err == nil {
			break
		}
	}
	if err != nil {
		t.Fatalf("DNS listener not ready: %v", err)
	}
	if len(response) < 12 || response[3]&15 != 0 || binary.BigEndian.Uint16(response[6:8]) != 0 {
		t.Fatalf("AAAA was not NOERROR/NODATA: %x", response)
	}
	response, err = query(1)
	if err != nil || len(response) < 12 || response[3]&15 != 0 || binary.BigEndian.Uint16(response[6:8]) == 0 {
		t.Fatalf("IPv4 FakeIP response missing: %x %v", response, err)
	}
}
