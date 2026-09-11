package dns

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"time"
)

const maxDoHTransports = 64

type dohPoolKey struct {
	binding  string
	endpoint Endpoint
}

type dohPoolEntry struct {
	transport *http.Transport
	used      uint64
}

// Each transport has exactly one source binding and one configured endpoint.
// Never inherit HTTP_PROXY/HTTPS_PROXY or the system's default DNS/dialer.
func (r *Resolver) newDoHTransport(binding Binding, endpoint Endpoint) *http.Transport {
	binding.DNSServers = append([]string(nil), binding.DNSServers...)
	transport := &http.Transport{
		TLSClientConfig:        &tls.Config{MinVersion: tls.VersionTLS12, ServerName: endpoint.Host},
		ForceAttemptHTTP2:      true,
		MaxIdleConns:           2,
		MaxIdleConnsPerHost:    2,
		MaxConnsPerHost:        8,
		IdleConnTimeout:        30 * time.Second,
		MaxResponseHeaderBytes: 16 * 1024,
		DisableCompression:     true,
	}
	transport.DialTLSContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
		// net/http may keep a dial alive for another waiting request after the
		// initiating request is cancelled. Bound setup independently, and stop
		// both TCP and TLS immediately when this resolver's root is cancelled.
		ctx, cancel := context.WithTimeout(ctx, r.config.QueryTimeout)
		stop := context.AfterFunc(r.root, cancel)
		defer stop()
		defer cancel()
		if err := r.root.Err(); err != nil {
			return nil, err
		}
		connection, err := r.dial(ctx, "tcp4", net.JoinHostPort(endpoint.IP, "443"), binding)
		if err != nil {
			return nil, err
		}
		secured := tls.Client(connection, transport.TLSClientConfig.Clone())
		if err := secured.HandshakeContext(ctx); err != nil {
			connection.Close()
			return nil, err
		}
		return secured, nil
	}
	return transport
}

func (r *Resolver) doHTransport(binding Binding, endpoint Endpoint) (*http.Transport, error) {
	r.dohMu.Lock()
	defer r.dohMu.Unlock()
	if err := r.root.Err(); err != nil {
		return nil, err
	}
	if r.dohClosed {
		return nil, context.Canceled
	}
	key := dohPoolKey{binding: bindingKey(binding), endpoint: endpoint}
	r.dohSequence++
	if entry := r.dohTransports[key]; entry != nil {
		entry.used = r.dohSequence
		return entry.transport, nil
	}
	if r.dohTransports == nil {
		r.dohTransports = make(map[dohPoolKey]*dohPoolEntry)
	}
	if len(r.dohTransports) >= maxDoHTransports {
		var oldest dohPoolKey
		var sequence uint64
		for candidate, entry := range r.dohTransports {
			if sequence == 0 || entry.used < sequence {
				oldest, sequence = candidate, entry.used
			}
		}
		r.dohTransports[oldest].transport.CloseIdleConnections()
		delete(r.dohTransports, oldest)
	}
	transport := r.newDoHTransport(binding, endpoint)
	r.dohTransports[key] = &dohPoolEntry{transport: transport, used: r.dohSequence}
	return transport, nil
}

func (r *Resolver) closeDoHTransports() {
	r.dohMu.Lock()
	defer r.dohMu.Unlock()
	r.dohClosed = true
	for _, entry := range r.dohTransports {
		entry.transport.CloseIdleConnections()
	}
	r.dohTransports = nil
}
