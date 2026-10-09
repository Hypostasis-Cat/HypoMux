package services

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestDiagnosticTLSRequiresVerifiedHandshake(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	target := diagnosticTLSTarget{Address: server.Listener.Addr().String(), Host: "127.0.0.1"}
	dial := func(ctx context.Context, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", address)
	}
	for _, tc := range []struct {
		name   string
		config *tls.Config
		host   string
		want   bool
	}{
		{"trusted", &tls.Config{RootCAs: roots}, "127.0.0.1", true},
		{"untrusted", &tls.Config{RootCAs: x509.NewCertPool()}, "127.0.0.1", false},
		{"wrong hostname", &tls.Config{RootCAs: roots}, "wrong.invalid", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			target.Host = tc.host
			r := probeDiagnosticTLS(ctx, target, dial, tc.config)
			if !r.TCP || r.TLS != tc.want {
				t.Fatalf("%+v", r)
			}
		})
	}
}

func TestDiagnosticTCPAcceptWithoutTLSIsNotSuccess(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	r := probeDiagnosticTLS(ctx, diagnosticTLSTarget{Host: "example.com"},
		func(context.Context, string) (net.Conn, error) { return client, nil }, nil)
	if !r.TCP || r.TLS {
		t.Fatalf("%+v", r)
	}
}

func TestDiagnosticTargetsRetryAndRespectCancellation(t *testing.T) {
	targets := []diagnosticTLSTarget{{Address: "one"}, {Address: "two"}, {Address: "three"}}
	var calls atomic.Int32
	r := probeDiagnosticTargets(context.Background(), targets, func(_ context.Context, target diagnosticTLSTarget) diagnosticEgressResult {
		calls.Add(1)
		return diagnosticEgressResult{Detail: target.Address + " timeout"}
	})
	if calls.Load() != 6 || r.TCP || r.TLS {
		t.Fatalf("%+v calls=%d", r, calls.Load())
	}
	calls.Store(0)
	r = probeDiagnosticTargets(context.Background(), targets, func(_ context.Context, target diagnosticTLSTarget) diagnosticEgressResult {
		calls.Add(1)
		ok := target.Address == "two"
		return diagnosticEgressResult{TCP: ok, TLS: ok, Detail: target.Address}
	})
	if calls.Load() != 3 || !r.TLS {
		t.Fatalf("one restricted target vetoed healthy target: %+v", r)
	}
	calls.Store(0)
	r = probeDiagnosticTargets(context.Background(), targets, func(_ context.Context, target diagnosticTLSTarget) diagnosticEgressResult {
		ok := calls.Add(1) > 3 && target.Address == "two"
		return diagnosticEgressResult{TCP: ok, TLS: ok}
	})
	if calls.Load() != 6 || !r.TLS {
		t.Fatalf("retry did not recover transient failure: %+v", r)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	probeDiagnosticTargets(ctx, targets, func(context.Context, diagnosticTLSTarget) diagnosticEgressResult {
		t.Error("cancelled run started a probe")
		return diagnosticEgressResult{}
	})
}
