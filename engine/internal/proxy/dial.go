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
) (net.Conn, Adapter, error) {
	host, port, splitErr := net.SplitHostPort(target)
	if splitErr != nil {
		return nil, Adapter{}, fmt.Errorf("target: %w", splitErr)
	}
	targetIP := net.ParseIP(host)
	if literalIPOnly && targetIP == nil {
		return nil, Adapter{}, errors.New("TUN TCP pool requires a literal IP target")
	}

	excluded := make(map[string]struct{}, len(channelScheduler.adapters))
	if targetIP != nil {
		network := networkForIP("tcp", targetIP)
		for _, adapter := range channelScheduler.adapters {
			if !adapterSupportsNetwork(adapter, network) {
				excluded[adapter.Name] = struct{}{}
			}
		}
	}

	attempts := len(channelScheduler.adapters) - len(excluded)
	if attempts > 2 {
		attempts = 2
	}
	var failures []error
	domain := ""
	if targetIP == nil {
		domain = normalizeDomain(host)
	}
	var comparativeFailures []string
	for range attempts {
		adapter, ok := channelScheduler.SelectForDomain(excluded, domain)
		if !ok {
			break
		}
		excluded[adapter.Name] = struct{}{}
		resolvedIP := targetIP
		if resolvedIP == nil {
			if s.resolver == nil {
				failures = append(failures, fmt.Errorf("%s DNS resolver is unavailable", adapter.Name))
				comparativeFailures = append(comparativeFailures, adapter.Name)
				continue
			}
			resolved, resolveErr := s.resolver.Resolve(ctx, dns.Query{
				Domain:     host,
				RecordType: dns.RecordA,
				Binding:    adapterDNSBinding(adapter),
			})
			if resolveErr != nil {
				if adapter.SourceIPv6 != "" {
					resolved, resolveErr = s.resolver.Resolve(ctx, dns.Query{
						Domain:     host,
						RecordType: dns.RecordAAAA,
						Binding:    adapterDNSBinding(adapter),
					})
				}
				if resolveErr != nil {
					failures = append(
						failures,
						fmt.Errorf("%s resolve %s: %w", adapter.Name, host, resolveErr),
					)
					comparativeFailures = append(comparativeFailures, adapter.Name)
					continue
				}
			}
			resolvedIP = net.ParseIP(resolved.Address)
			if resolvedIP == nil {
				failures = append(
					failures,
					fmt.Errorf("%s resolver returned invalid address", adapter.Name),
				)
				comparativeFailures = append(comparativeFailures, adapter.Name)
				continue
			}
		}
		network := networkForIP("tcp", resolvedIP)
		if !adapterSupportsNetwork(adapter, network) {
			failures = append(
				failures,
				fmt.Errorf("%s has no %s source address", adapter.Name, network),
			)
			continue
		}
		dialTarget := net.JoinHostPort(resolvedIP.String(), port)
		dialer, err := boundNetworkDialer(adapter, s.config.ConnectTimeout, network)
		if err != nil {
			channelScheduler.MarkFailure(adapter.Name)
			failures = append(failures, fmt.Errorf("%s bind: %w", adapter.Name, err))
			continue
		}
		connection, err := s.dialTCP(ctx, dialer, dialTarget)
		if err == nil {
			channelScheduler.MarkSuccess(adapter.Name, domain)
			for _, failedAdapter := range comparativeFailures {
				channelScheduler.health.recordComparativeDomainFailure(
					failedAdapter,
					domain,
				)
			}
			return connection, adapter, nil
		}
		if isLocalConnectFailure(err) {
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

func adapterDNSBinding(adapter Adapter) dns.Binding {
	return dns.Binding{
		Name:       adapter.Name,
		SourceIP:   adapter.SourceIP,
		IfIndex:    adapter.IfIndex,
		DNSServers: append([]string(nil), adapter.DNSServers...),
	}
}
