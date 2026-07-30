package proxy

import (
	"context"
	"errors"
	"fmt"
	"net"

	"github.com/Hypostasis-Cat/HypoMux/engine/internal/dns"
)

func (s *Server) dialUpstream(ctx context.Context, target string) (net.Conn, Adapter, error) {
	attempts := len(s.config.Adapters)
	if attempts > 2 {
		attempts = 2
	}
	excluded := make(map[string]struct{}, attempts)
	var failures []error
	for range attempts {
		adapter, ok := s.scheduler.Select(excluded)
		if !ok {
			break
		}
		excluded[adapter.Name] = struct{}{}
		dialTarget := target
		host, port, splitErr := net.SplitHostPort(target)
		if splitErr != nil {
			s.scheduler.MarkFailure(adapter.Name)
			failures = append(failures, fmt.Errorf("%s target: %w", adapter.Name, splitErr))
			continue
		}
		if net.ParseIP(host) == nil {
			if s.resolver == nil {
				s.scheduler.MarkFailure(adapter.Name)
				failures = append(failures, fmt.Errorf("%s DNS resolver is unavailable", adapter.Name))
				continue
			}
			resolved, resolveErr := s.resolver.Resolve(ctx, dns.Query{
				Domain:     host,
				RecordType: dns.RecordA,
				Binding:    adapterDNSBinding(adapter),
			})
			if resolveErr != nil {
				s.scheduler.MarkFailure(adapter.Name)
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
			s.scheduler.MarkFailure(adapter.Name)
			failures = append(failures, fmt.Errorf("%s bind: %w", adapter.Name, err))
			continue
		}
		connection, err := s.dialTCP(ctx, dialer, dialTarget)
		if err == nil {
			s.scheduler.MarkSuccess(adapter.Name)
			return connection, adapter, nil
		}
		s.scheduler.MarkFailure(adapter.Name)
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
