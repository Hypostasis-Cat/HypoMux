package dns

import (
	"context"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAutoFallbackSurvivesSilentDoHAndFirstLegacyServer(t *testing.T) {
	var dohDials, legacyDials atomic.Int64
	resolver, err := New(context.Background(), Config{
		Policy: PolicyAuto, QueryTimeout: time.Second,
	}, func(ctx context.Context, network, address string, binding Binding) (net.Conn, error) {
		if binding.Name != loopbackBinding.Name || binding.SourceIP != loopbackBinding.SourceIP {
			t.Errorf("lost source binding: %#v", binding)
		}
		if strings.HasSuffix(address, ":443") {
			dohDials.Add(1)
			<-ctx.Done()
			return nil, ctx.Err()
		}
		legacyDials.Add(1)
		if address == "192.0.2.53:53" {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		if ctx.Err() != nil {
			t.Errorf("fallback received expired context: %v", ctx.Err())
		}
		return answeringConnection(t, "192.0.2.46", 30, 0, network), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	binding := loopbackBinding
	binding.DNSServers = []string{"192.0.2.53"}
	result, err := resolver.Resolve(context.Background(), Query{Domain: "fallback.example", Binding: binding})
	if err != nil {
		t.Fatal(err)
	}
	if result.Transport != "udp" || result.Server != "223.5.5.5:53" || dohDials.Load() < 3 || legacyDials.Load() != 3 {
		t.Fatalf("result=%#v DoH=%d legacy=%d", result, dohDials.Load(), legacyDials.Load())
	}
}

func TestExplicitDoHTimeoutNeverUsesPlainDNS(t *testing.T) {
	var legacy atomic.Int64
	resolver, err := New(context.Background(), Config{Policy: PolicyAliDNS, QueryTimeout: 50 * time.Millisecond},
		func(ctx context.Context, network, address string, binding Binding) (net.Conn, error) {
			if !strings.HasSuffix(address, ":443") {
				legacy.Add(1)
			}
			<-ctx.Done()
			return nil, ctx.Err()
		})
	if err != nil {
		t.Fatal(err)
	}
	_, err = resolver.Resolve(context.Background(), Query{Domain: "strict.example", Binding: loopbackBinding})
	if err == nil || legacy.Load() != 0 {
		t.Fatalf("err=%v plain DNS attempts=%d", err, legacy.Load())
	}
}
