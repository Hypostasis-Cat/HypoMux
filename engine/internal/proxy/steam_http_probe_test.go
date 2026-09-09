package proxy

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Hypostasis-Cat/HypoMux/engine/internal/dns"
)

func TestSteamPublicChunkScope(t *testing.T) {
	for _, host := range []string{"xz.pphimalayanrt.com", "st.dl.eccdnx.com", "dl.steam.clngaa.com"} {
		if !steamDownloadHost(host) || steamDownloadHost(host+".evil.test") {
			t.Fatal(host)
		}
	}
	for _, tc := range []struct {
		method, path, headers string
		allowed               bool
	}{
		{"GET", testSteamChunk, "", true}, {"GET", testSteamChunk + "?token=secret", "", false},
		{"POST", testSteamChunk, "", false}, {"GET", "/login", "", false},
		{"GET", testSteamChunk, "Cookie: token=secret\r\n", false},
		{"GET", testSteamChunk, "Authorization: Bearer secret\r\n", false},
		{"GET", testSteamChunk, "Content-Length: 5\r\n", false},
	} {
		request := tc.method + " " + tc.path + " HTTP/1.1\r\nHost: " + testSteamHost + "\r\n" + tc.headers + "\r\n"
		if got := steamChunkPath([]byte(request)); (got != "") != tc.allowed {
			t.Fatalf("scope %q: %q", request, got)
		}
		reader := bufio.NewReader(strings.NewReader(request))
		_, _ = reader.Peek(len(request))
		_ = peekSteamChunkPath(reader)
		rest, _ := io.ReadAll(reader)
		if string(rest) != request {
			t.Fatal("consumed request")
		}
	}
}

func TestSteamHTTPProbeValidation(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		status                 int
		contentRange, encoding string
		size                   int
		valid                  bool
	}{
		{"valid", 206, "bytes 0-4095/8192", "", 4096, true},
		{"range ignored", 200, "", "", 4096, false},
		{"redirect", 302, "", "", 0, false},
		{"short", 206, "bytes 0-4095/8192", "", 50, false},
		{"oversize", 206, "bytes 0-4095/8192", "", 4097, false},
		{"wrong range", 206, "bytes 1-4096/8192", "", 4096, false},
		{"invalid total", 206, "bytes 0-4095/1", "", 4096, false},
		{"encoded", 206, "bytes 0-4095/8192", "gzip", 4096, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Host != testSteamHost || r.URL.Path != testSteamChunk || r.Header.Get("Range") != "bytes=0-4095" || r.Header.Get("Cookie") != "" {
					t.Error("invalid probe request")
				}
				w.Header().Set("Content-Range", tc.contentRange)
				w.Header().Set("Content-Encoding", tc.encoding)
				w.Header().Set("Location", "http://127.0.0.1/private")
				w.WriteHeader(tc.status)
				_, _ = w.Write(bytes.Repeat([]byte("x"), tc.size))
			}))
			defer origin.Close()
			s, _ := New(Config{Adapters: []Adapter{{Name: "a", SourceIP: "127.0.0.1"}}})
			calls := 0
			s.dialTCP = func(ctx context.Context, d *net.Dialer, target string) (net.Conn, error) {
				calls++
				if target != "1.2.3.4:80" || d.LocalAddr.String() != "127.0.0.1:0" {
					t.Error("lost target/binding", target, d.LocalAddr)
				}
				return (&net.Dialer{}).DialContext(ctx, "tcp", origin.Listener.Addr().String())
			}
			data, err := s.probeSteamHTTP(context.Background(), s.config.Adapters[0], testSteamHost, "1.2.3.4", testSteamChunk)
			if (err == nil) != tc.valid || (tc.valid && len(data) != 4096) {
				t.Fatal(len(data), err)
			}
			if calls != 1 {
				t.Fatal("redirect followed", calls)
			}
		})
	}
}

func TestSteamHTTPDiscoveryComparesContentPerNIC(t *testing.T) {
	s, _ := New(Config{SteamCDNEnabled: true, Adapters: []Adapter{{Name: "a", SourceIP: "127.0.0.1"}, {Name: "b", SourceIP: "127.0.0.2"}}, DNS: dns.Config{Policy: dns.PolicyOff}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.ctx = ctx
	s.cdn = newSteamCDN(ctx, true)
	defer s.cdn.cancel()
	resolver, err := dns.New(ctx, s.config.DNS, func(_ context.Context, network, _ string, binding dns.Binding) (net.Conn, error) {
		ip := "1.2.3.4"
		if binding.Name == "b" {
			ip = "5.6.7.8"
		}
		a, b := net.Pipe()
		go answerDNSAOnce(b, network, net.ParseIP(ip).To4())
		return a, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	s.dialTCP = func(_ context.Context, d *net.Dialer, target string) (net.Conn, error) {
		a, b := net.Pipe()
		go func() {
			defer b.Close()
			_ = b.SetDeadline(time.Now().Add(time.Second))
			req, err := http.ReadRequest(bufio.NewReader(b))
			if err != nil {
				return
			}
			defer req.Body.Close()
			value := "x"
			if target == "5.6.7.8:80" && d.LocalAddr.String() == "127.0.0.1:0" {
				value = "y"
			}
			_, _ = fmt.Fprintf(b, "HTTP/1.1 206 Partial Content\r\nContent-Length: 4096\r\nContent-Range: bytes 0-4095/8192\r\nConnection: close\r\n\r\n%s", strings.Repeat(value, 4096))
		}()
		return a, nil
	}
	// A missing safe path must not reserve a cooldown that blocks a later chunk.
	s.discoverSteamCDN(testSteamHost, "80")
	if len(s.cdn.discovery) != 0 {
		t.Fatal("missing path reserved discovery cooldown")
	}
	s.probeSteamCandidates(ctx, s.cdn.generation, testSteamHost, "80", testSteamChunk, resolver)
	deadline := time.Now().Add(3 * time.Second)
	for s.cdn.snapshot().Probing > 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	status := s.cdn.snapshot()
	if status.Probing != 0 || len(status.Entries) != 3 {
		t.Fatalf("status: %+v", status)
	}
	mismatch := false
	for _, d := range status.Diagnostics {
		if d.Stage == "http_content_mismatch" && d.Adapter == "a" && d.IP == "5.6.7.8" {
			mismatch = true
		}
	}
	if !mismatch {
		t.Fatal("missing mismatch diagnostic", status.Diagnostics)
	}
	for _, e := range status.Entries {
		if e.Adapter == "a" && e.IP == "5.6.7.8" {
			t.Fatal("accepted different content")
		}
	}
}

func TestSteamDiagnosticsBoundedAndReset(t *testing.T) {
	c := newSteamCDN(context.Background(), true)
	defer c.cancel()
	generation := c.generation
	for i := 0; i < 100; i++ {
		c.note(generation, testSteamHost, "a", fmt.Sprint(i), "verified")
	}
	c.note(generation, testSteamHost, "a", "99", "verified")
	if len(c.snapshot().Diagnostics) != 64 {
		t.Fatal("not bounded or deduplicated")
	}
	c.configure(true, true)
	c.note(generation, testSteamHost, "a", "late", "verified")
	if len(c.snapshot().Diagnostics) != 0 {
		t.Fatal("stale diagnostic survived reset")
	}
}
