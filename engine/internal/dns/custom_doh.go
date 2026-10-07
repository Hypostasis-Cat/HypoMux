package dns

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Bootstrap uses the same adapter-scoped cache and coalescing as ordinary
// lookups, but forces traditional DNS to avoid recursively calling DoH.
func (r *Resolver) queryBootstrappedDoH(ctx context.Context, domain string, recordType uint16, binding Binding, endpoint Endpoint) (Result, time.Duration, error) {
	bootstrapCtx, cancel := context.WithTimeout(ctx, remainingQueryBudget(ctx, r.config.QueryTimeout)/2)
	types := []RecordType{}
	if binding.SourceIP != "" {
		types = append(types, RecordA)
	}
	if binding.SourceIPv6 != "" {
		types = append(types, RecordAAAA)
	}
	type outcome struct {
		result Result
		err    error
	}
	outcomes := make(chan outcome, len(types))
	for _, kind := range types {
		go func(kind RecordType) {
			result, err := r.Resolve(bootstrapCtx, Query{Domain: endpoint.Host, RecordType: kind, Binding: binding, LegacyDNS: true})
			outcomes <- outcome{result, err}
		}(kind)
	}
	var endpoints []Endpoint
	var failures []error
	for range types {
		value := <-outcomes
		if value.err != nil {
			failures = append(failures, value.err)
			continue
		}
		for _, address := range value.result.Addresses {
			if supportsEndpoint(binding, address) {
				next := endpoint
				next.IP = address
				endpoints = append(endpoints, next)
			}
		}
	}
	cancel()
	if len(endpoints) == 0 {
		return Result{}, 0, fmt.Errorf("DoH hostname bootstrap failed: %w", errors.Join(failures...))
	}
	// Keep at most two dials per endpoint; the query's deadline bounds both
	// bootstrap and HTTPS and the transport retains the original TLS hostname.
	return r.resolveDoHEndpoints(ctx, domain, recordType, binding, endpoints)
}
