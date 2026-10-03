package proxy

import (
	"context"
	"errors"
	"fmt"
	"net"

	"github.com/Hypostasis-Cat/HypoMux/engine/internal/dns"
)

func (s *Server) dialUpstream(
	ctx context.Context,
	target string,
	channelScheduler *scheduler,
	literalIPOnly bool,
	tuneTCP bool,
) (net.Conn, Adapter, error) {
	host, port, splitErr := net.SplitHostPort(target)
	if splitErr != nil {
		return nil, Adapter{}, fmt.Errorf("target: %w", splitErr)
	}
	targetIP := net.ParseIP(host)
	if literalIPOnly && targetIP == nil {
		return nil, Adapter{}, errors.New("TUN TCP pool requires a literal IP target")
	}

	adapters := channelScheduler.snapshot().Adapters
	excluded := make(map[string]struct{}, len(adapters))
	if targetIP != nil {
		network := networkForIP("tcp", targetIP)
		for _, adapter := range adapters {
			if !adapterSupportsNetwork(adapter, network) && !(targetIP.To4() != nil && adapter.SourceIPv6 != "") {
				excluded[adapter.Name] = struct{}{}
			}
		}
		excludeCoolingFamilies(adapters, excluded, channelScheduler.health, network)
	}

	attempts := len(adapters) - len(excluded)
	if attempts > 2 {
		attempts = 2
	}
	var failures []error
	domain := ""
	if targetIP == nil {
		domain = normalizeDomain(host)
	}
	var comparativeFailures []string
	var pending *performanceLease
	defer func() {
		if pending != nil {
			pending.finish()
		}
	}()
	for range attempts {
		if pending != nil {
			pending.finish()
			pending = nil
		}
		adapter, lease, ok := channelScheduler.acquireTCP(excluded, domain, host)
		pending = lease
		if !ok {
			break
		}
		excluded[adapter.Name] = struct{}{}
		connection, err := s.dialAdapterTargets(ctx, adapter, host, port, targetIP, channelScheduler, tuneTCP)
		if err == nil {
			channelScheduler.MarkSuccess(adapter.Name, domain)
			for _, failedAdapter := range comparativeFailures {
				channelScheduler.health.recordComparativeDomainFailure(failedAdapter, domain)
			}
			if pending != nil {
				pending.attach()
				connection = &leasedConn{Conn: connection, lease: pending}
				pending = nil
			}
			return connection, adapter, nil
		}
		if isLocalConnectFailure(err) && (adapter.SourceIP == "" || adapter.SourceIPv6 == "") {
			channelScheduler.MarkFailure(adapter.Name)
		} else if domain != "" {
			comparativeFailures = append(comparativeFailures, adapter.Name)
		}
		failures = append(failures, fmt.Errorf("%s connect: %w", adapter.Name, err))
	}
	if len(failures) == 0 {
		return nil, Adapter{}, errors.New("no adapter available")
	}
	return nil, Adapter{}, errors.Join(failures...)
}

func networkForIP(transport string, ip net.IP) string {
	if ip.To4() != nil {
		return transport + "4"
	}
	return transport + "6"
}

func adapterSupportsNetwork(adapter Adapter, network string) bool {
	if network == "tcp6" || network == "udp6" {
		return adapter.SourceIPv6 != ""
	}
	return adapter.SourceIP != ""
}

// Prefer a working family on another selected NIC, but retain recovery attempts
// when every matching NIC is cooling down. IPv4 targets may use NAT64 on v6-only.
func excludeCoolingFamilies(adapters []Adapter, excluded map[string]struct{}, health *healthTable, network string) {
	actualNetwork := func(a Adapter) string {
		if (network == "tcp4" || network == "udp4") && a.SourceIP == "" && a.SourceIPv6 != "" {
			return network[:len(network)-1] + "6"
		}
		return network
	}
	ready := false
	for _, a := range adapters {
		if _, skip := excluded[a.Name]; !skip && health.familyAvailable(a, actualNetwork(a)) {
			ready = true
			break
		}
	}
	if ready {
		for _, a := range adapters {
			if !health.familyAvailable(a, actualNetwork(a)) {
				excluded[a.Name] = struct{}{}
			}
		}
	}
}

func adapterDNSBinding(adapter Adapter) dns.Binding {
	return dns.Binding{
		Name:        adapter.Name,
		SourceIP:    adapter.SourceIP,
		IfIndex:     adapter.IfIndex,
		SourceIPv6:  adapter.SourceIPv6,
		IPv6IfIndex: adapter.IPv6IfIndex,
		DNSServers:  append([]string(nil), adapter.DNSServers...),
	}
}
