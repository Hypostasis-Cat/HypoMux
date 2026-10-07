package dns

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestCustomDoHRelayPreservesURLBindingAndLifecycle(t *testing.T) {
	path := "/custom%2Fquery?profile=test&key=a%2Bb"
	f := newDoHFixture(t, false, func(w http.ResponseWriter, req *http.Request) {
		if req.Host != "example.com:8443" || req.URL.RequestURI() != path {
			t.Errorf("URL changed: %s %s", req.Host, req.URL.RequestURI())
		}
		packet, err := io.ReadAll(req.Body)
		if err != nil {
			t.Error(err)
			return
		}
		packet[2] |= 0x80
		w.Header().Set("Content-Type", "application/dns-message")
		_, _ = w.Write(packet)
	})
	config, err := NormalizeConfig(Config{Policy: PolicyCustom, DoHServers: []string{"https://example.com:8443" + path}, QueryTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	f.resolver.config = config
	endpoint := ConfigEndpoints(config)[0]
	endpoint.IP = f.endpoint.IP
	f.trust(t, loopbackBinding, endpoint)
	address, err := f.resolver.StartDoHRelay(loopbackBinding, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	again, err := f.resolver.StartDoHRelay(loopbackBinding, endpoint)
	if err != nil || again != address {
		t.Fatal("relay was not reused")
	}
	conn, err := net.Dial("udp4", address)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	query, _, err := buildQuery("target.example", 16)
	if err != nil {
		t.Fatal(err)
	} // TXT must also pass through intact.
	if _, err := conn.Write(query); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 4096)
	n, err := conn.Read(buffer)
	if err != nil {
		t.Fatal(err)
	}
	want := append([]byte(nil), query...)
	want[2] |= 0x80
	if !bytes.Equal(buffer[:n], want) {
		t.Fatal("DNS wire query changed")
	}
	f.mu.Lock()
	dials := append([]dohTestDial(nil), f.dials...)
	f.mu.Unlock()
	if len(dials) != 1 || dials[0].binding.SourceIP != loopbackBinding.SourceIP || dials[0].address != "192.0.2.53:8443" {
		t.Fatalf("binding lost: %+v", dials)
	}
	endpoint.Host = "unconfigured.example"
	if _, err := f.resolver.StartDoHRelay(loopbackBinding, endpoint); err == nil {
		t.Fatal("accepted an unconfigured relay target")
	}
	f.cancel()
	f.resolver.closeDoHTransports()
	if _, err := f.resolver.StartDoHRelay(loopbackBinding, ConfigEndpoints(config)[0]); err == nil {
		t.Fatal("started relay after cancellation")
	}
}
