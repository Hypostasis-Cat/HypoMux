package proxy

import (
	"context"
	"errors"
	"fmt"
	"net"
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
		dialer, err := boundDialer(adapter, s.config.ConnectTimeout)
		if err != nil {
			s.scheduler.MarkFailure(adapter.Name)
			failures = append(failures, fmt.Errorf("%s bind: %w", adapter.Name, err))
			continue
		}
		connection, err := dialer.DialContext(ctx, "tcp4", target)
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
