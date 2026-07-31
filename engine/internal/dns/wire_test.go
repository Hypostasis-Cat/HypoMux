package dns

import (
	"encoding/binary"
	"fmt"
	"net"
	"testing"
	"time"
)

func TestBuildAndParseAddressResponses(t *testing.T) {
	tests := []struct {
		name       string
		recordType uint16
		address    string
	}{
		{name: "A", recordType: dnsTypeA, address: "192.0.2.25"},
		{name: "AAAA", recordType: dnsTypeAAAA, address: "2001:db8::25"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			query, queryID, err := buildQuery("Example.COM.", test.recordType)
			if err != nil {
				t.Fatal(err)
			}
			response := answerForQuery(t, query, test.recordType, test.address, 90)
			answer, err := parseResponse(response, queryID, test.recordType)
			if err != nil {
				t.Fatal(err)
			}
			if answer.Address != test.address {
				t.Fatalf("address = %q, want %q", answer.Address, test.address)
			}
			if answer.TTL != 90*time.Second {
				t.Fatalf("TTL = %v", answer.TTL)
			}
		})
	}
}

func TestParseRejectsFakeIPTruncationAndMismatchedID(t *testing.T) {
	query, queryID, err := buildQuery("example.test", dnsTypeA)
	if err != nil {
		t.Fatal(err)
	}
	fake := answerForQuery(t, query, dnsTypeA, "198.18.1.2", 60)
	if _, err := parseResponse(fake, queryID, dnsTypeA); err == nil {
		t.Fatal("FakeIP response unexpectedly accepted")
	}

	truncated := append([]byte(nil), fake...)
	binary.BigEndian.PutUint16(truncated[2:4], 0x8380)
	if _, err := parseResponse(truncated, queryID, dnsTypeA); err == nil {
		t.Fatal("truncated response unexpectedly accepted")
	}

	valid := answerForQuery(t, query, dnsTypeA, "192.0.2.1", 60)
	if _, err := parseResponse(valid, queryID+1, dnsTypeA); err == nil {
		t.Fatal("mismatched response unexpectedly accepted")
	}
}

func TestNormalizeDomainRequiresWireSafeIDNAForm(t *testing.T) {
	if normalized, err := normalizeDomain("WWW.Example.COM."); err != nil || normalized != "www.example.com" {
		t.Fatalf("normalized = %q, %v", normalized, err)
	}
	if normalized, err := normalizeDomain("例子.测试"); err != nil ||
		normalized != "xn--fsqu00a.xn--0zwm56d" {
		t.Fatalf("IDNA normalized = %q, %v", normalized, err)
	}
	for _, invalid := range []string{"", "-bad.example", "bad_.example"} {
		if _, err := normalizeDomain(invalid); err == nil {
			t.Errorf("invalid domain %q unexpectedly accepted", invalid)
		}
	}
}

func answerForQuery(
	t *testing.T,
	query []byte,
	recordType uint16,
	address string,
	ttl uint32,
) []byte {
	t.Helper()
	response, err := answerForQueryRaw(query, recordType, address, ttl)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func answerForQueryRaw(
	query []byte,
	recordType uint16,
	address string,
	ttl uint32,
) ([]byte, error) {
	if len(query) < 12 {
		return nil, fmt.Errorf("short test query")
	}
	response := make([]byte, 12, len(query)+32)
	copy(response[0:2], query[0:2])
	binary.BigEndian.PutUint16(response[2:4], 0x8180)
	binary.BigEndian.PutUint16(response[4:6], 1)
	binary.BigEndian.PutUint16(response[6:8], 1)
	response = append(response, query[12:]...)
	response = append(response, 0xc0, 0x0c)
	response = binary.BigEndian.AppendUint16(response, recordType)
	response = binary.BigEndian.AppendUint16(response, 1)
	response = binary.BigEndian.AppendUint32(response, ttl)
	ip := net.ParseIP(address)
	if recordType == dnsTypeA {
		ip = ip.To4()
	} else {
		ip = ip.To16()
	}
	if ip == nil {
		return nil, fmt.Errorf("invalid test address %q", address)
	}
	response = binary.BigEndian.AppendUint16(response, uint16(len(ip)))
	response = append(response, ip...)
	return response, nil
}
