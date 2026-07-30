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
	literalIPv4Only bool,
) (net.Conn, Adapter, error) {
	host, _, splitErr := net.SplitHostPort(target)
	if splitErr != nil {
		return nil, Adapter{}, fmt.Errorf("target: %w", splitErr)
	}
	if literalIPv4Only {
		ip := net.ParseIP(host)
		if ip == nil || ip.To4() == nil {
			return nil, Adapter{}, errors.New("TUN TCP pool requires a literal IPv4 target")
		}
	}
	attempts := len(channelScheduler.adapters)
	if attempts > 2 {
		attempts = 2
	}
	excluded := make(map[string]struct{}, attempts)
	var failures []error
	for range attempts {
		adapter, ok := channelScheduler.Select(excluded)
		if !ok {
			break
		}
		excluded[adapter.Name] = struct{}{}
		dialTarget := target
		host, port, splitErr := net.SplitHostPort(target)
		if splitErr != nil {
			channelScheduler.MarkFailure(adapter.Name)
			failures = append(failures, fmt.Errorf("%s target: %w", adapter.Name, splitErr))
			continue
		}
		if net.ParseIP(host) == nil {
			if s.resolver == nil {
				channelScheduler.MarkFailure(adapter.Name)
				failures = append(failures, fmt.Errorf("%s DNS resolver is unavailable", adapter.Name))
				continue
			}
			resolved, resolveErr := s.resolver.Resolve(ctx, dns.Query{
				Domain:     host,
				RecordType: dns.RecordA,
				Binding:    adapterDNSBinding(adapter),
			})
			if resolveErr != nil {
				channelScheduler.MarkFailure(adapter.Name)
				failures = append(
					failures,
					fmt.Errorf("%s resolve %s: %w", adapter.Name, host, resolveErr),
				)
				continue
			}
			dialTarget = net.JoinHostPort(resolved.Address, port)
		}
		dialer, err := boundDialer(adapter, s.config.ConnectTimeout)
		if err != nil {
			channelScheduler.MarkFailure(adapter.Name)
			failures = append(failures, fmt.Errorf("%s bind: %w", adapter.Name, err))
			continue
		}
		connection, err := s.dialTCP(ctx, dialer, dialTarget)
		if err == nil {
			channelScheduler.MarkSuccess(adapter.Name)
			return connection, adapter, nil
		}
		channelScheduler.MarkFailure(adapter.Name)
		failures = append(failures, fmt.Errorf("%s connect: %w", adapter.Name, err))
	}
	if len(failures) == 0 {
		return nil, Adapter{}, errors.New("no adapter available")
	}
	return nil, Adapter{}, errors.Join(failures...)
}

func adapterDNSBinding(adapter Adapter) dns.Binding {
	return dns.Binding{
		Name:       adapter.Name,
		SourceIP:   adapter.SourceIP,
		IfIndex:    adapter.IfIndex,
		DNSServers: append([]string(nil), adapter.DNSServers...),
	}
}
