package dns

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

const maxDoTPools = 64

type dotIdleConn struct {
	conn  *tls.Conn
	since time.Time
}

// At most two queries per binding/endpoint, each owning its connection.
// Idle connections are reused without mixing DNS IDs from concurrent queries.
type dotPoolEntry struct {
	mu        sync.Mutex
	slots     chan struct{}
	idle      []dotIdleConn
	tlsConfig *tls.Config
	closed    bool
	used      uint64
}

func (entry *dotPoolEntry) close() {
	entry.mu.Lock()
	defer entry.mu.Unlock()
	entry.closed = true
	for _, idle := range entry.idle {
		_ = idle.conn.Close()
	}
	entry.idle = nil
}

func (r *Resolver) doTPool(binding Binding, endpoint Endpoint) (*dotPoolEntry, error) {
	r.dotMu.Lock()
	defer r.dotMu.Unlock()
	if err := r.root.Err(); err != nil {
		return nil, err
	}
	key := dohPoolKey{binding: bindingKey(binding), endpoint: endpoint}
	r.dotSequence++
	if entry := r.dotPools[key]; entry != nil {
		entry.used = r.dotSequence
		return entry, nil
	}
	if r.dotPools == nil {
		r.dotPools = make(map[dohPoolKey]*dotPoolEntry)
	}
	if len(r.dotPools) >= maxDoTPools {
		var oldest dohPoolKey
		var sequence uint64
		for candidate, entry := range r.dotPools {
			if sequence == 0 || entry.used < sequence {
				oldest, sequence = candidate, entry.used
			}
		}
		r.dotPools[oldest].close()
		delete(r.dotPools, oldest)
	}
	entry := &dotPoolEntry{slots: make(chan struct{}, 2), used: r.dotSequence,
		tlsConfig: &tls.Config{MinVersion: tls.VersionTLS12, ServerName: endpoint.Host}}
	r.dotPools[key] = entry
	return entry, nil
}

func (r *Resolver) closeDoTPools() {
	r.dotMu.Lock()
	defer r.dotMu.Unlock()
	for _, entry := range r.dotPools {
		entry.close()
	}
	r.dotPools = nil
}

func (r *Resolver) resolveDoT(ctx context.Context, domain string, recordType uint16, binding Binding, configured []Endpoint) (Result, time.Duration, error) {
	var endpoints []Endpoint
	for _, endpoint := range configured {
		if endpoint.IP == "" || supportsEndpoint(binding, endpoint.IP) {
			endpoints = append(endpoints, endpoint)
		}
	}
	if len(endpoints) == 0 {
		return Result{}, 0, fmt.Errorf("no DoT endpoint matches adapter %q address families", binding.Name)
	}
	batchCount := (len(endpoints) + 1) / 2
	budget := remainingQueryBudget(ctx, r.config.QueryTimeout) / time.Duration(batchCount)
	var failures []error
	for start := 0; start < len(endpoints); start += 2 {
		batch := endpoints[start:min(start+2, len(endpoints))]
		batchCtx, cancel := context.WithTimeout(ctx, budget)
		type outcome struct {
			result Result
			ttl    time.Duration
			err    error
		}
		outcomes := make(chan outcome, len(batch))
		for _, endpoint := range batch {
			go func() {
				result, ttl, err := r.queryDoT(batchCtx, domain, recordType, binding, endpoint)
				outcomes <- outcome{result, ttl, err}
			}()
		}
		for range batch {
			select {
			case value := <-outcomes:
				if value.err == nil {
					cancel()
					return value.result, value.ttl, nil
				}
				failures = append(failures, value.err)
			case <-batchCtx.Done():
				failures = append(failures, batchCtx.Err())
			}
		}
		cancel()
		if ctx.Err() != nil {
			return Result{}, 0, ctx.Err()
		}
	}
	return Result{}, 0, errors.Join(failures...)
}

func (r *Resolver) queryDoT(ctx context.Context, domain string, recordType uint16, binding Binding, endpoint Endpoint) (Result, time.Duration, error) {
	if endpoint.IP == "" {
		return r.queryBootstrappedEncrypted(ctx, domain, recordType, binding, endpoint, "DoT", r.resolveDoT)
	}
	packet, queryID, err := buildQuery(domain, recordType)
	if err != nil {
		return Result{}, 0, err
	}
	pool, err := r.doTPool(binding, endpoint)
	if err != nil {
		return Result{}, 0, err
	}
	select {
	case pool.slots <- struct{}{}:
		defer func() { <-pool.slots }()
	case <-ctx.Done():
		return Result{}, 0, ctx.Err()
	}
	// One fresh retry handles servers that closed an idle connection.
	for attempt := 0; attempt < 2; attempt++ {
		pool.mu.Lock()
		if pool.closed {
			pool.mu.Unlock()
			return Result{}, 0, context.Canceled
		}
		var connection *tls.Conn
		for len(pool.idle) > 0 {
			last := len(pool.idle) - 1
			idle := pool.idle[last]
			pool.idle = pool.idle[:last]
			if time.Since(idle.since) < 30*time.Second {
				connection = idle.conn
				break
			}
			_ = idle.conn.Close()
		}
		pool.mu.Unlock()
		reused := connection != nil
		if !reused {
			if !supportsEndpoint(binding, endpoint.IP) {
				return Result{}, 0, fmt.Errorf("DoT endpoint has no matching source address")
			}
			raw, err := r.dial(ctx, endpointNetwork("tcp", endpoint.IP), net.JoinHostPort(endpoint.IP, endpoint.port()), binding)
			if err != nil {
				return Result{}, 0, err
			}
			connection = tls.Client(raw, pool.tlsConfig.Clone())
		}
		stop := context.AfterFunc(ctx, func() { _ = connection.Close() })
		setContextDeadline(connection, ctx)
		answer, err := exchangeDoT(ctx, connection, packet, queryID, recordType)
		canReuse := stop() && ctx.Err() == nil && err == nil
		if canReuse {
			canReuse = connection.SetDeadline(time.Time{}) == nil
		}
		pool.mu.Lock()
		if canReuse && !pool.closed {
			pool.idle = append(pool.idle, dotIdleConn{connection, time.Now()})
		} else {
			_ = connection.Close()
		}
		pool.mu.Unlock()
		if err == nil {
			return Result{Address: answer.Address, Addresses: append([]string(nil), answer.Addresses...), Transport: "dot",
				Server: endpoint.Host + "@" + net.JoinHostPort(endpoint.IP, endpoint.port())}, answer.TTL, nil
		}
		if !reused || ctx.Err() != nil || attempt == 1 {
			return Result{}, 0, err
		}
	}
	return Result{}, 0, fmt.Errorf("DoT retry budget exhausted")
}

func exchangeDoT(ctx context.Context, connection *tls.Conn, packet []byte, queryID, recordType uint16) (wireAnswer, error) {
	if err := connection.HandshakeContext(ctx); err != nil {
		return wireAnswer{}, err
	}
	frame := make([]byte, len(packet)+2)
	binary.BigEndian.PutUint16(frame, uint16(len(packet)))
	copy(frame[2:], packet)
	if n, err := connection.Write(frame); err != nil {
		return wireAnswer{}, err
	} else if n != len(frame) {
		return wireAnswer{}, io.ErrShortWrite
	}
	var length uint16
	if err := binary.Read(connection, binary.BigEndian, &length); err != nil {
		return wireAnswer{}, err
	}
	if length < 12 {
		return wireAnswer{}, fmt.Errorf("invalid DoT response length %d", length)
	}
	response := make([]byte, int(length))
	if _, err := io.ReadFull(connection, response); err != nil {
		return wireAnswer{}, err
	}
	return parseResponse(response, queryID, recordType)
}
