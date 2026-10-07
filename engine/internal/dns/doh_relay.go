package dns

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
)

// StartDoHRelay adapts DNS wire queries for sidecars whose HTTPS URL field
// cannot preserve an escaped path or query string. HTTPS still uses the
// resolver's source-bound transport and certificate verification.
func (r *Resolver) StartDoHRelay(binding Binding, endpoint Endpoint) (string, error) {
	var err error
	binding, err = NormalizeBinding(binding)
	if err != nil {
		return "", err
	}
	parsed, _, err := parseDoHURL("https://" + endpoint.authority() + endpoint.Path)
	if err != nil {
		return "", err
	}
	parsed.IP = endpoint.IP
	ip := net.ParseIP(parsed.IP)
	if ip == nil || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() {
		return "", fmt.Errorf("invalid DoH relay bootstrap IP")
	}
	if !supportsEndpoint(binding, parsed.IP) {
		return "", fmt.Errorf("DoH relay has no compatible bootstrap IP")
	}
	allowed := false
	for _, configured := range ConfigEndpoints(r.config) {
		if configured.Host == parsed.Host && configured.Path == parsed.Path && configured.port() == parsed.port() && (configured.IP == "" || configured.IP == parsed.IP) {
			allowed = true
			break
		}
	}
	if !allowed {
		return "", fmt.Errorf("DoH relay endpoint is not configured")
	}
	transport, err := r.doHTransport(binding, parsed)
	if err != nil {
		return "", err
	}
	key := dohPoolKey{binding: bindingKey(binding), endpoint: parsed}
	r.dohMu.Lock()
	defer r.dohMu.Unlock()
	if r.dohClosed || r.root.Err() != nil {
		return "", context.Canceled
	}
	if relay := r.dohRelays[key]; relay != nil {
		return relay.LocalAddr().String(), nil
	}
	if len(r.dohRelays) >= maxDoHTransports {
		return "", fmt.Errorf("too many DoH relays")
	}
	relay, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	if r.dohRelays == nil {
		r.dohRelays = make(map[dohPoolKey]net.PacketConn)
	}
	r.dohRelays[key] = relay
	go func() {
		capacity := make(chan struct{}, 64)
		buffer := make([]byte, maxDNSMessageBytes)
		for {
			n, remote, err := relay.ReadFrom(buffer)
			if err != nil {
				return
			}
			if n < 12 || buffer[2]&0x80 != 0 {
				continue
			}
			select {
			case capacity <- struct{}{}:
			default:
				continue
			}
			query := append([]byte(nil), buffer[:n]...)
			go func() {
				defer func() { <-capacity }()
				ctx, cancel := context.WithTimeout(r.root, r.config.QueryTimeout)
				defer cancel()
				request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://"+parsed.authority()+parsed.Path, bytes.NewReader(query))
				if err != nil {
					return
				}
				request.Header.Set("Content-Type", "application/dns-message")
				request.Header.Set("Accept", "application/dns-message")
				response, err := transport.RoundTrip(request)
				var answer []byte
				if err == nil {
					answer, err = io.ReadAll(io.LimitReader(response.Body, 65508))
					_ = response.Body.Close()
					if response.StatusCode != http.StatusOK || len(answer) < 12 || len(answer) > 65507 || answer[2]&0x80 == 0 || !bytes.Equal(answer[:2], query[:2]) {
						err = fmt.Errorf("invalid DoH wire response")
					}
				}
				if err != nil {
					answer = append([]byte(nil), query...)
					answer[2] |= 0x80
					answer[3] = (answer[3] & 0xf0) | 2
					for offset := 6; offset < 12; offset += 2 {
						binary.BigEndian.PutUint16(answer[offset:offset+2], 0)
					}
				}
				_, _ = relay.WriteTo(answer, remote)
			}()
		}
	}()
	return relay.LocalAddr().String(), nil
}
