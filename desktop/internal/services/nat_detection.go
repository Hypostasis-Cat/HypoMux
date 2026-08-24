package services

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/pion/stun/v3"
)

const (
	natMappingDirect           = "direct"
	natEndpointIndependent     = "endpoint_independent"
	natAddressDependent        = "address_dependent"
	natAddressPortDependent    = "address_port_dependent"
	natBehaviorInconclusive    = "inconclusive"
	natProbeResponseTimeout    = 1250 * time.Millisecond
	natDetectionOverallTimeout = 12 * time.Second
)

var (
	errNATProbeTimeout      = errors.New("STUN response timeout")
	errNATServerUnsupported = errors.New("STUN server does not support RFC 5780 OTHER-ADDRESS")
	natBehaviorSTUNServers  = []string{"stun.voipgate.com:3478", "stun.voipinfocenter.com:3478"}
)

type NATDetectionResult struct {
	State             string    `json:"state"`
	AdapterID         string    `json:"adapter_id,omitempty"`
	Name              string    `json:"name,omitempty"`
	Address           string    `json:"address,omitempty"`
	NATType           string    `json:"nat_type,omitempty"`
	MappingBehavior   string    `json:"mapping_behavior,omitempty"`
	FilteringBehavior string    `json:"filtering_behavior,omitempty"`
	PublicEndpoint    string    `json:"public_endpoint,omitempty"`
	Server            string    `json:"server,omitempty"`
	Detail            string    `json:"detail,omitempty"`
	DurationMS        int64     `json:"duration_ms,omitempty"`
	StartedAt         time.Time `json:"started_at,omitempty"`
	CompletedAt       time.Time `json:"completed_at,omitempty"`
}

type natSTUNSession struct {
	connection *net.UDPConn
	primary    *net.UDPAddr
}

type natSTUNResponse struct {
	message *stun.Message
	source  *net.UDPAddr
}

func classifyNAT(mapping string, filtering string) string {
	switch {
	case mapping == natMappingDirect:
		return "direct"
	case mapping == natEndpointIndependent && filtering == natEndpointIndependent:
		return "full_cone"
	case mapping == natEndpointIndependent && filtering == natAddressDependent:
		return "restricted_cone"
	case mapping == natEndpointIndependent && filtering == natAddressPortDependent:
		return "port_restricted_cone"
	case mapping == natAddressPortDependent:
		return "symmetric"
	case mapping == natBehaviorInconclusive || filtering == natBehaviorInconclusive:
		return "inconclusive"
	default:
		return "unknown"
	}
}

func detectAdapterNAT(parent context.Context, adapter AdapterView) NATDetectionResult {
	started := time.Now()
	result := NATDetectionResult{
		State: "running", AdapterID: adapter.ID, Name: adapter.Name, Address: adapter.Address,
		MappingBehavior: natBehaviorInconclusive, FilteringBehavior: natBehaviorInconclusive,
		StartedAt: started,
	}
	ctx, cancel := context.WithTimeout(parent, natDetectionOverallTimeout)
	defer cancel()

	failures := make([]string, 0, len(natBehaviorSTUNServers))
	for _, server := range natBehaviorSTUNServers {
		probe, err := probeNATBehavior(ctx, adapter, server)
		if err == nil {
			probe.StartedAt = started
			probe.CompletedAt = time.Now()
			probe.DurationMS = probe.CompletedAt.Sub(started).Milliseconds()
			return probe
		}
		if ctx.Err() != nil {
			result.State = "cancelled"
			result.Detail = "NAT detection cancelled"
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				result.State = "inconclusive"
				result.Detail = "NAT detection timed out"
			}
			result.CompletedAt = time.Now()
			result.DurationMS = result.CompletedAt.Sub(started).Milliseconds()
			return result
		}
		failures = append(failures, fmt.Sprintf("%s: %v", server, err))
	}

	result.State = "inconclusive"
	result.NATType = "inconclusive"
	result.Detail = strings.Join(failures, "; ")
	result.CompletedAt = time.Now()
	result.DurationMS = result.CompletedAt.Sub(started).Milliseconds()
	return result
}

func probeNATBehavior(ctx context.Context, adapter AdapterView, server string) (NATDetectionResult, error) {
	result := NATDetectionResult{
		State: "inconclusive", AdapterID: adapter.ID, Name: adapter.Name, Address: adapter.Address,
		NATType: "inconclusive", MappingBehavior: natBehaviorInconclusive,
		FilteringBehavior: natBehaviorInconclusive, Server: server,
	}
	primary, err := net.ResolveUDPAddr("udp4", server)
	if err != nil {
		return result, fmt.Errorf("resolve STUN server: %w", err)
	}
	localIP := net.ParseIP(adapter.Address).To4()
	if localIP == nil {
		return result, errors.New("adapter does not have a valid IPv4 address")
	}
	connection, err := net.ListenUDP("udp4", &net.UDPAddr{IP: localIP})
	if err != nil {
		return result, fmt.Errorf("bind adapter source %s: %w", adapter.Address, err)
	}
	session := &natSTUNSession{connection: connection, primary: primary}
	defer connection.Close()

	initial, err := session.roundTrip(ctx, primary, 0)
	if err != nil {
		return result, fmt.Errorf("binding request: %w", err)
	}
	mapped, other, err := parseNATAddresses(initial.message)
	if err != nil {
		return result, err
	}
	result.PublicEndpoint = mapped.String()
	primaryOrigin := initial.source

	// Filtering tests run before the mapping tests so an expected timeout
	// does not inherit state created by probes to the alternate endpoint.
	changedBoth, changedBothErr := session.roundTrip(ctx, primary, 0x06)
	switch {
	case changedBothErr == nil:
		if !sameUDPEndpoint(changedBoth.source, other) {
			return result, errors.New("STUN server replied from an unexpected change-address endpoint")
		}
		result.FilteringBehavior = natEndpointIndependent
	case changedBothErr != nil && !errors.Is(changedBothErr, errNATProbeTimeout):
		return result, fmt.Errorf("filtering change-address test: %w", changedBothErr)
	case errors.Is(changedBothErr, errNATProbeTimeout):
		changedPort, changedPortErr := session.roundTrip(ctx, primary, 0x02)
		expectedPortOrigin := &net.UDPAddr{IP: primaryOrigin.IP, Port: other.Port}
		switch {
		case changedPortErr == nil && sameUDPEndpoint(changedPort.source, expectedPortOrigin):
			result.FilteringBehavior = natAddressDependent
		case errors.Is(changedPortErr, errNATProbeTimeout):
			result.FilteringBehavior = natAddressPortDependent
		case changedPortErr != nil:
			return result, fmt.Errorf("filtering change-port test: %w", changedPortErr)
		default:
			return result, errors.New("STUN server replied from an unexpected filtering endpoint")
		}
	}

	local := connection.LocalAddr().(*net.UDPAddr)
	if sameUDPEndpoint(mapped, local) {
		result.MappingBehavior = natMappingDirect
	} else {
		alternateIPPrimaryPort := &net.UDPAddr{IP: other.IP, Port: primaryOrigin.Port}
		second, secondErr := session.roundTrip(ctx, alternateIPPrimaryPort, 0)
		if secondErr != nil {
			return result, fmt.Errorf("mapping alternate-address test: %w", secondErr)
		}
		mappedSecond, _, secondParseErr := parseNATAddresses(second.message)
		if secondParseErr != nil && !errors.Is(secondParseErr, errNATServerUnsupported) {
			return result, secondParseErr
		}
		if sameUDPEndpoint(mapped, mappedSecond) {
			result.MappingBehavior = natEndpointIndependent
		} else {
			third, thirdErr := session.roundTrip(ctx, other, 0)
			if thirdErr != nil {
				return result, fmt.Errorf("mapping alternate-port test: %w", thirdErr)
			}
			mappedThird, _, thirdParseErr := parseNATAddresses(third.message)
			if thirdParseErr != nil && !errors.Is(thirdParseErr, errNATServerUnsupported) {
				return result, thirdParseErr
			}
			if sameUDPEndpoint(mappedSecond, mappedThird) {
				result.MappingBehavior = natAddressDependent
			} else {
				result.MappingBehavior = natAddressPortDependent
			}
		}
	}

	result.NATType = classifyNAT(result.MappingBehavior, result.FilteringBehavior)
	if result.NATType == "inconclusive" {
		result.Detail = "The STUN behavior tests did not produce a conclusive classification"
		return result, errors.New(result.Detail)
	}
	result.State = "completed"
	result.Detail = "RFC 5780 UDP mapping and filtering behavior detected"
	return result, nil
}

func (s *natSTUNSession) roundTrip(ctx context.Context, target *net.UDPAddr, changeRequest byte) (*natSTUNResponse, error) {
	request := stun.MustBuild(stun.TransactionID, stun.BindingRequest)
	if changeRequest != 0 {
		request.Add(stun.AttrChangeRequest, []byte{0x00, 0x00, 0x00, changeRequest})
	}
	if _, err := s.connection.WriteToUDP(request.Raw, target); err != nil {
		return nil, err
	}

	responseDeadline := time.Now().Add(natProbeResponseTimeout)
	buffer := make([]byte, 2048)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !time.Now().Before(responseDeadline) {
			return nil, errNATProbeTimeout
		}
		readDeadline := time.Now().Add(250 * time.Millisecond)
		if readDeadline.After(responseDeadline) {
			readDeadline = responseDeadline
		}
		_ = s.connection.SetReadDeadline(readDeadline)
		count, source, err := s.connection.ReadFromUDP(buffer)
		if err != nil {
			if networkErr, ok := err.(net.Error); ok && networkErr.Timeout() {
				continue
			}
			return nil, err
		}
		message := &stun.Message{Raw: append([]byte(nil), buffer[:count]...)}
		if err := message.Decode(); err != nil || message.TransactionID != request.TransactionID {
			continue
		}
		if message.Type != stun.BindingSuccess {
			return nil, fmt.Errorf("unexpected STUN response type %s", message.Type)
		}
		return &natSTUNResponse{message: message, source: source}, nil
	}
}

func parseNATAddresses(message *stun.Message) (*net.UDPAddr, *net.UDPAddr, error) {
	mapped := &stun.XORMappedAddress{}
	if err := mapped.GetFrom(message); err != nil {
		return nil, nil, fmt.Errorf("STUN response has no XOR-MAPPED-ADDRESS: %w", err)
	}
	other := &stun.OtherAddress{}
	if err := other.GetFrom(message); err != nil {
		return &net.UDPAddr{IP: mapped.IP, Port: mapped.Port}, nil, errNATServerUnsupported
	}
	return &net.UDPAddr{IP: mapped.IP, Port: mapped.Port}, &net.UDPAddr{IP: other.IP, Port: other.Port}, nil
}

func sameUDPEndpoint(left *net.UDPAddr, right *net.UDPAddr) bool {
	return left != nil && right != nil && left.Port == right.Port && left.IP.Equal(right.IP)
}
