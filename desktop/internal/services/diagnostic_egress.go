package services

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

type diagnosticEgressResult struct {
	TCP, TLS bool
	Detail   string
}

type diagnosticTLSTarget struct{ Address, Host string }

// Literal destinations avoid system DNS/FakeIP. Certificate-verified TLS
// distinguishes a working data path from a locally accepted TCP handshake.
func probeDiagnosticTLS(ctx context.Context, target diagnosticTLSTarget,
	dial func(context.Context, string) (net.Conn, error), config *tls.Config,
) diagnosticEgressResult {
	conn, err := dial(ctx, target.Address)
	if err != nil {
		return diagnosticEgressResult{Detail: fmt.Sprintf("%s TCP: %v", target.Address, err)}
	}
	defer conn.Close()
	result := diagnosticEgressResult{TCP: true}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if config != nil {
		tlsConfig = config.Clone()
	}
	tlsConfig.ServerName = target.Host
	secure := tls.Client(conn, tlsConfig)
	if err := secure.HandshakeContext(ctx); err != nil {
		result.Detail = fmt.Sprintf("%s via %s TCP OK; TLS %s: %v", target.Address, conn.LocalAddr(), target.Host, err)
		return result
	}
	result.TLS = true
	result.Detail = fmt.Sprintf("%s via %s TCP + TLS %s OK", target.Address, conn.LocalAddr(), target.Host)
	return result
}

// Probe independent providers concurrently. If none verifies, retry once;
// a finite set of failures is inconclusive, never proof of total outage.
func probeDiagnosticTargets(ctx context.Context, targets []diagnosticTLSTarget,
	probe func(context.Context, diagnosticTLSTarget) diagnosticEgressResult,
) diagnosticEgressResult {
	result := diagnosticEgressResult{}
	var evidence []string
	for round := 0; round < 2 && ctx.Err() == nil; round++ {
		outcomes := make([]diagnosticEgressResult, len(targets))
		var wg sync.WaitGroup
		for i, target := range targets {
			wg.Add(1)
			go func(i int, target diagnosticTLSTarget) {
				defer wg.Done()
				attempt, cancel := context.WithTimeout(ctx, 3*time.Second)
				defer cancel()
				outcomes[i] = probe(attempt, target)
			}(i, target)
		}
		wg.Wait()
		for _, outcome := range outcomes {
			result.TCP = result.TCP || outcome.TCP
			result.TLS = result.TLS || outcome.TLS
			evidence = append(evidence, fmt.Sprintf("#%d %s", round+1, outcome.Detail))
		}
		if result.TLS {
			break
		}
	}
	result.Detail = strings.Join(evidence, "; ")
	if result.Detail == "" {
		result.Detail = "探测未完成"
	}
	return result
}
