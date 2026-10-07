package dns

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func TestCustomDoHConfiguration(t *testing.T) {
	config, err := NormalizeConfig(Config{Policy: PolicyCustom, DoHServers: []string{" https://DNS.Example.com:8443/custom?key=hello ", "https://dns.example.com:8443/custom?key=hello", "https://[2001:4860:4860::8888]"}})
	if err != nil {
		t.Fatal(err)
	}
	endpoints := ConfigEndpoints(config)
	if len(endpoints) != 2 || endpoints[0].Host != "dns.example.com" || endpoints[0].Port != 8443 || endpoints[0].Path != "/custom?key=hello" || endpoints[1].IP != "2001:4860:4860::8888" {
		t.Fatalf("invalid custom endpoints: %+v", endpoints)
	}
	config.Policy = PolicyAuto
	if len(ConfigEndpoints(config)) != 2 {
		t.Fatal("auto did not select custom list")
	}
	for _, value := range []string{"", "http://dns.example/query", "https://user:pass@dns.example/query", "https://dns.example/#fragment", "https://dns.example:99999/", "https://0.0.0.0/query"} {
		if _, err := NormalizeConfig(Config{Policy: PolicyCustom, DoHServers: []string{value}}); err == nil {
			t.Errorf("accepted %q", value)
		}
	}
	if _, err := NormalizeConfig(Config{Policy: PolicyCustom}); err == nil {
		t.Fatal("accepted empty custom DoH")
	}
}

func TestCustomDoHBootstrapsBoundCachesAndPreservesURL(t *testing.T) {
	f := newDoHFixture(t, false, func(w http.ResponseWriter, request *http.Request) {
		if request.Host != "example.com:8443" || request.URL.RequestURI() != "/custom/query?key=hello" {
			t.Errorf("URL changed: %s %s", request.Host, request.URL.RequestURI())
		}
		serveDoHAnswer(t, w, request)
	})
	config, err := NormalizeConfig(Config{Policy: PolicyCustom, DoHServers: []string{"https://example.com:8443/custom/query?key=hello"}, QueryTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	f.resolver.config = config
	endpoint := ConfigEndpoints(config)[0]
	endpoint.IP = f.endpoint.IP
	f.trust(t, loopbackBinding, endpoint)
	originalDial := f.resolver.dial
	var bootstrapDials atomic.Int64
	f.resolver.dial = func(ctx context.Context, network, address string, binding Binding) (net.Conn, error) {
		if binding.SourceIP != loopbackBinding.SourceIP || binding.Name != loopbackBinding.Name {
			t.Errorf("source binding changed: %+v", binding)
		}
		if network == "udp4" {
			bootstrapDials.Add(1)
			return answeringConnection(t, f.endpoint.IP, 60, 0, network), nil
		}
		if address != "192.0.2.53:8443" {
			t.Errorf("dial address changed: %s", address)
		}
		return originalDial(ctx, network, address, binding)
	}
	for _, domain := range []string{"first.example", "second.example"} {
		result, err := f.resolver.Resolve(context.Background(), Query{Domain: domain, Binding: loopbackBinding})
		if err != nil || result.Transport != "doh" || result.DoHPath != "/custom/query?key=hello" || result.Server != "example.com@192.0.2.53:8443" {
			t.Fatalf("result=%+v err=%v", result, err)
		}
	}
	if bootstrapDials.Load() != 1 {
		t.Fatalf("hostname bootstrap was not cached: %d", bootstrapDials.Load())
	}
}

func TestCustomDoHNeverDowngradesButAutoFallsBack(t *testing.T) {
	for _, policy := range []string{PolicyCustom, PolicyAuto} {
		var plaintext atomic.Int64
		resolver, err := New(context.Background(), Config{Policy: policy, DoHServers: []string{"https://192.0.2.1/query", "https://192.0.2.2/query"}, FailureThreshold: 1}, func(ctx context.Context, network, address string, binding Binding) (net.Conn, error) {
			if network == "udp4" {
				plaintext.Add(1)
				return answeringConnection(t, "192.0.2.44", 60, 0, network), nil
			}
			return nil, errors.New("HTTPS unavailable")
		})
		if err != nil {
			t.Fatal(err)
		}
		var restart atomic.Int64
		resolver.SetFallbackHandler(func(FallbackEvent) { restart.Add(1) })
		result, err := resolver.Resolve(context.Background(), Query{Domain: "target.example", Binding: loopbackBinding})
		if policy == PolicyCustom && (err == nil || plaintext.Load() != 0 || restart.Load() != 0) {
			t.Fatalf("custom policy downgraded: %+v %v DNS=%d restart=%d", result, err, plaintext.Load(), restart.Load())
		}
		if policy == PolicyAuto && (err != nil || result.Transport != "udp" || plaintext.Load() == 0) {
			t.Fatalf("auto did not fall back: %+v %v", result, err)
		}
	}
}
