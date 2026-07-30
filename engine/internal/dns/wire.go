package dns

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"net"
	"strings"
	"time"

	"golang.org/x/net/idna"
)

type RecordType string

const (
	RecordA    RecordType = "A"
	RecordAAAA RecordType = "AAAA"

	dnsTypeA    uint16 = 1
	dnsTypeAAAA uint16 = 28
)

type wireAnswer struct {
	Address string
	TTL     time.Duration
}

type truncatedResponseError struct{}

func (truncatedResponseError) Error() string { return "DNS response is truncated" }

func normalizeRecordType(value RecordType) (RecordType, uint16, error) {
	switch RecordType(strings.ToUpper(strings.TrimSpace(string(value)))) {
	case "", RecordA:
		return RecordA, dnsTypeA, nil
	case RecordAAAA:
		return RecordAAAA, dnsTypeAAAA, nil
	default:
		return "", 0, fmt.Errorf("unsupported DNS record type %q", value)
	}
}

func normalizeDomain(value string) (string, error) {
	raw := strings.TrimSuffix(strings.TrimSpace(value), ".")
	ascii, err := idna.Lookup.ToASCII(raw)
	if err != nil {
		return "", fmt.Errorf("convert domain to IDNA: %w", err)
	}
	name := strings.ToLower(ascii)
	if name == "" || len(name) > 253 {
		return "", fmt.Errorf("invalid domain length")
	}
	for _, label := range strings.Split(name, ".") {
		if len(label) == 0 || len(label) > 63 {
			return "", fmt.Errorf("invalid DNS label length")
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return "", fmt.Errorf("DNS label cannot start or end with a hyphen")
		}
		for _, character := range []byte(label) {
			if character >= 'a' && character <= 'z' ||
				character >= '0' && character <= '9' ||
				character == '-' {
				continue
			}
			return "", fmt.Errorf("domain must use ASCII or IDNA form")
		}
	}
	return name, nil
}

func buildQuery(domain string, recordType uint16) ([]byte, uint16, error) {
	name, err := normalizeDomain(domain)
	if err != nil {
		return nil, 0, err
	}
	var idBytes [2]byte
	if _, err := rand.Read(idBytes[:]); err != nil {
		return nil, 0, fmt.Errorf("generate DNS query ID: %w", err)
	}
	queryID := binary.BigEndian.Uint16(idBytes[:])
	packet := make([]byte, 12, 12+len(name)+6)
	binary.BigEndian.PutUint16(packet[0:2], queryID)
	binary.BigEndian.PutUint16(packet[2:4], 0x0100)
	binary.BigEndian.PutUint16(packet[4:6], 1)
	for _, label := range strings.Split(name, ".") {
		packet = append(packet, byte(len(label)))
		packet = append(packet, label...)
	}
	packet = append(packet, 0)
	packet = binary.BigEndian.AppendUint16(packet, recordType)
	packet = binary.BigEndian.AppendUint16(packet, 1)
	return packet, queryID, nil
}

func parseResponse(packet []byte, queryID uint16, recordType uint16) (wireAnswer, error) {
	if len(packet) < 12 {
		return wireAnswer{}, fmt.Errorf("short DNS response")
	}
	if binary.BigEndian.Uint16(packet[0:2]) != queryID {
		return wireAnswer{}, fmt.Errorf("mismatched DNS response ID")
	}
	flags := binary.BigEndian.Uint16(packet[2:4])
	if flags&0x8000 == 0 {
		return wireAnswer{}, fmt.Errorf("DNS packet is not a response")
	}
	if flags&0x0200 != 0 {
		return wireAnswer{}, truncatedResponseError{}
	}
	if code := flags & 0x000f; code != 0 {
		return wireAnswer{}, fmt.Errorf("DNS response code %d", code)
	}

	questionCount := int(binary.BigEndian.Uint16(packet[4:6]))
	answerCount := int(binary.BigEndian.Uint16(packet[6:8]))
	offset := 12
	var err error
	for range questionCount {
		offset, err = skipName(packet, offset)
		if err != nil || offset+4 > len(packet) {
			return wireAnswer{}, fmt.Errorf("malformed DNS question")
		}
		offset += 4
	}

	var best wireAnswer
	for range answerCount {
		offset, err = skipName(packet, offset)
		if err != nil || offset+10 > len(packet) {
			return wireAnswer{}, fmt.Errorf("malformed DNS answer")
		}
		answerType := binary.BigEndian.Uint16(packet[offset : offset+2])
		class := binary.BigEndian.Uint16(packet[offset+2 : offset+4])
		ttlSeconds := binary.BigEndian.Uint32(packet[offset+4 : offset+8])
		length := int(binary.BigEndian.Uint16(packet[offset+8 : offset+10]))
		offset += 10
		if length < 0 || offset+length > len(packet) {
			return wireAnswer{}, fmt.Errorf("truncated DNS answer data")
		}
		data := packet[offset : offset+length]
		offset += length
		if class != 1 || answerType != recordType {
			continue
		}

		var ip net.IP
		switch {
		case recordType == dnsTypeA && length == net.IPv4len:
			ip = net.IP(data).To4()
			if isFakeIPv4(ip) {
				continue
			}
		case recordType == dnsTypeAAAA && length == net.IPv6len:
			ip = net.IP(data)
		default:
			continue
		}
		if ip == nil {
			continue
		}
		ttl := time.Duration(ttlSeconds) * time.Second
		if best.Address == "" || ttl < best.TTL {
			best = wireAnswer{Address: ip.String(), TTL: ttl}
		}
	}
	if best.Address == "" {
		return wireAnswer{}, fmt.Errorf("DNS response has no requested address record")
	}
	return best, nil
}

func skipName(packet []byte, offset int) (int, error) {
	for labels := 0; labels <= 128; labels++ {
		if offset >= len(packet) {
			return 0, fmt.Errorf("truncated DNS name")
		}
		length := int(packet[offset])
		switch {
		case length == 0:
			return offset + 1, nil
		case length&0xc0 == 0xc0:
			if offset+1 >= len(packet) {
				return 0, fmt.Errorf("truncated DNS compression pointer")
			}
			pointer := (length&0x3f)<<8 | int(packet[offset+1])
			if pointer >= len(packet) {
				return 0, fmt.Errorf("invalid DNS compression pointer")
			}
			return offset + 2, nil
		case length&0xc0 != 0 || length > 63:
			return 0, fmt.Errorf("invalid DNS label")
		default:
			offset++
			if offset+length > len(packet) {
				return 0, fmt.Errorf("truncated DNS label")
			}
			offset += length
		}
	}
	return 0, fmt.Errorf("too many DNS labels")
}

func isFakeIPv4(ip net.IP) bool {
	value := ip.To4()
	return value != nil && value[0] == 198 && (value[1] == 18 || value[1] == 19)
}
