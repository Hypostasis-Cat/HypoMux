package proxy

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Hypostasis-Cat/HypoMux/engine/internal/dns"
)

func TestSteamObserverTracksKeepAliveIdle(t *testing.T) {
	_, o := observerFixture(t)
	request := "GET " + testSteamChunk + " HTTP/1.1\r\nHost: " + testSteamHost + "\r\n\r\n"
	o.feed(true, []byte(request))
	waiting := o.idleTime(time.Now())
	if later := o.idleTime(time.Now().Add(time.Hour)); later != waiting {
		t.Fatalf("time waiting for a response counted as idle: %v -> %v", waiting, later)
	}
	o.feed(false, []byte("HTTP/1.1 200 OK\r\nContent-Length: 4\r\n\r\ndata"))
	now := time.Now()
	if gap := o.idleTime(now.Add(8*time.Second)) - o.idleTime(now); gap < 7*time.Second {
		t.Fatalf("keep-alive gap not idle: %v", gap)
	}
	o.mu.Lock()
	o.idleFrom = o.idleFrom.Add(-8 * time.Second)
	o.mu.Unlock()
	o.feed(true, []byte(request))
	frozen := o.idleTime(time.Now())
	if frozen < 8*time.Second || o.idleTime(time.Now().Add(time.Hour)) != frozen {
		t.Fatalf("next request did not end the idle gap: %v", frozen)
	}
	o.close()
	if o.idleTime(time.Now().Add(time.Hour)) != frozen {
		t.Fatal("stopped observer kept classifying gaps")
	}
	var missing *steamHTTPObserver
	if missing.idleTime(time.Now()) != 0 {
		t.Fatal("unobserved connection reported idle time")
	}
}

func TestSteamActiveElapsedExcludesOnlyIdle(t *testing.T) {
	start := time.Unix(100, 0)
	if got := steamActiveElapsed(start, start.Add(8500*time.Millisecond), time.Second, 9*time.Second); got != 500*time.Millisecond {
		t.Fatal(got)
	}
	if got := steamActiveElapsed(start, start.Add(time.Second), 0, 2*time.Second); got != 0 {
		t.Fatal("negative active time", got)
	}
}

// Two fast responses separated by client think time must score as fast. The
// former wall-clock sample divided both bodies by the idle gap as well.
func TestSteamThroughputExcludesKeepAliveIdle(t *testing.T) {
	s, _ := New(Config{DNS: dns.Config{Policy: dns.PolicyOff}, SteamCDNEnabled: true, Adapters: []Adapter{{Name: "a", SourceIP: "127.0.0.1"}}})
	s.ctx = context.Background()
	s.cdn = newSteamCDN(s.ctx, true)
	t.Cleanup(s.cdn.cancel)
	key := cdnKey{"a", testSteamHost, "80", "1.2.3.4"}
	s.cdn.mu.Lock()
	s.cdn.probing = 2 // The 4 KiB reference prefix must not start live discovery.
	s.cdn.entries[key] = &SteamCDNEntry{Adapter: "a", Domain: testSteamHost, Port: "80", IP: key.ip, ExpiresAt: time.Now().Add(time.Minute)}
	s.cdn.mu.Unlock()
	app, client := net.Pipe()
	upstream, origin := net.Pipe()
	session := s.registry.Begin("socks5", "", client)
	session.cdnKey, session.cdnGeneration = key, s.cdn.generation
	relayed := make(chan struct{})
	go func() { s.relay(client, client, upstream, session); close(relayed) }()
	const body = 1 << 20
	go func() {
		defer origin.Close()
		reader := bufio.NewReader(origin)
		chunk := strings.Repeat("x", 64<<10)
		for range 2 {
			if _, err := http.ReadRequest(reader); err != nil {
				return
			}
			_, _ = fmt.Fprintf(origin, "HTTP/1.1 200 OK\r\nContent-Length: %d\r\n\r\n", body)
			for range body / len(chunk) {
				time.Sleep(10 * time.Millisecond)
				_, _ = io.WriteString(origin, chunk)
			}
		}
	}()
	reader := bufio.NewReader(app)
	for i := range 2 {
		if i == 1 {
			time.Sleep(1500 * time.Millisecond) // No request outstanding.
		}
		_, _ = io.WriteString(app, "GET "+testSteamChunk+" HTTP/1.1\r\nHost: "+testSteamHost+"\r\n\r\n")
		response, err := http.ReadResponse(reader, nil)
		if err != nil {
			t.Fatal(err)
		}
		if n, _ := io.Copy(io.Discard, response.Body); n != body {
			t.Fatalf("short body: %d", n)
		}
	}
	_ = app.Close()
	<-relayed
	s.cdn.mu.Lock()
	entry := *s.cdn.entries[key]
	s.cdn.mu.Unlock()
	// About 2 MiB over roughly 330 ms of transfer; wall time is about 1.8 s.
	t.Logf("samples=%d bps=%.0f", entry.Samples, entry.DownloadBPS)
	if entry.Samples == 0 || entry.DownloadBPS < 3<<20 {
		t.Fatalf("idle gap deflated throughput: samples=%d bps=%.0f", entry.Samples, entry.DownloadBPS)
	}
}
