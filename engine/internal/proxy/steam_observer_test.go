package proxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/Hypostasis-Cat/HypoMux/engine/internal/dns"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func observerFixture(t *testing.T) (*Server, *steamHTTPObserver) {
	t.Helper()
	s, _ := New(Config{DNS: dns.Config{Policy: dns.PolicyOff}, SteamCDNEnabled: true, Adapters: []Adapter{{Name: "a", SourceIP: "127.0.0.1"}}})
	s.ctx = context.Background()
	s.cdn = newSteamCDN(s.ctx, true)
	session := &connection{cdnKey: cdnKey{"a", testSteamHost, "80", "1.2.3.4"}, cdnGeneration: s.cdn.generation}
	o := s.newSteamObserver(session)
	s.cdn.probing = 2 // Prevent live DNS in parser tests.
	t.Cleanup(func() { o.close(); s.cdn.cancel() })
	return s, o
}
func TestSteamObserverPersistentFraming(t *testing.T) {
	for _, fragment := range []int{1, 7, 65536} {
		t.Run(fmt.Sprint(fragment), func(t *testing.T) {
			s, o := observerFixture(t)
			feed := func(up bool, data string) {
				for len(data) > 0 {
					n := min(fragment, len(data))
					o.feed(up, []byte(data[:n]))
					data = data[n:]
				}
			}
			// Body contains what looks like a request: it must never be parsed as one.
			fake := "GET " + testSteamChunk + " HTTP/1.1\r\nHost: " + testSteamHost + "\r\n\r\n"
			feed(true, fmt.Sprintf("POST /auth HTTP/1.1\r\nHost: %s\r\nContent-Length: %d\r\n\r\n%s", testSteamHost, len(fake), fake))
			feed(false, "HTTP/1.1 100 Continue\r\n\r\nHTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok")
			feed(true, "HEAD /metadata HTTP/1.1\r\nHost: "+testSteamHost+"\r\n\r\n"+fake+fake)
			feed(false, "HTTP/1.1 200 OK\r\nContent-Length: 99\r\n\r\nHTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n4\r\ntest\r\n0\r\nX-End: yes\r\n\r\nHTTP/1.1 200 OK\r\nContent-Length: 4\r\n\r\ndata")
			if o.stopped || len(o.pending) != 0 || o.current != nil {
				t.Fatalf("framing failed: stopped=%v pending=%d current=%v", o.stopped, len(o.pending), o.current != nil)
			}
			counts := s.cdn.snapshot().StageCounts
			if counts["http_eligible"] != 2 || counts["http_method"] != 2 {
				t.Fatal(counts)
			}
		})
	}
}
func TestSteamSignedClassificationAndRedaction(t *testing.T) {
	host := "xz.sycontroller.com"
	uri := testSteamChunk + "?reqhost=ctgslb&auth_key=1788931770-1234-0-FAKEsignature"
	request := func(target, headers string) *http.Request {
		r, e := http.ReadRequest(bufio.NewReader(strings.NewReader("GET " + target + " HTTP/1.1\r\nHost: " + host + "\r\n" + headers + "\r\n")))
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	got, reason := steamRequestURI(request(uri, ""))
	if got != uri || reason != "http_signed_eligible" {
		t.Fatal(reason)
	}
	for _, headers := range []string{"Cookie: secret\r\n", "Authorization: Bearer secret\r\n", "Content-Length: 5\r\n"} {
		if got, _ := steamRequestURI(request(uri, headers)); got != "" {
			t.Fatal("credentials allowed")
		}
	}
	if got, _ := steamRequestURI(request(uri+"&unexpected=secret", "")); got != "" {
		t.Fatal("unknown profile accepted")
	}
	s, o := observerFixture(t)
	o.session.cdnKey.domain = host
	o.feed(true, []byte("GET "+uri+" HTTP/1.1\r\nHost: "+host+"\r\n\r\n"))
	data, _ := json.Marshal(s.cdn.snapshot())
	if bytes.Contains(data, []byte("FAKEsignature")) || bytes.Contains(data, []byte("auth_key")) {
		t.Fatal("signature leaked into telemetry")
	}
	o.pending[0].at = time.Now().Add(-time.Minute)
	o.expire()
	if o.pending[0].uri != "" {
		t.Fatal("signature not expired")
	}
}
func TestSteamObserverFallbackPreservesBytes(t *testing.T) {
	for _, payload := range []string{"garbage\r\n\r\n", strings.Repeat("x", steamSniffLimit+1), "GET / HTTP/1.1\r\nHost: " + testSteamHost + "\r\nUpgrade: h2c\r\n\r\n"} {
		_, o := observerFixture(t)
		var forwarded bytes.Buffer
		w := steamObserverWriter{Writer: &forwarded, observer: o, up: true}
		_, e := w.Write([]byte(payload))
		if e != nil || forwarded.String() != payload || !o.stopped {
			t.Fatal("fallback changed forwarding")
		}
	}
}
func TestSteamObserverRequestLimitAndDisable(t *testing.T) {
	s, o := observerFixture(t)
	for i := 0; i < 17; i++ {
		o.feed(true, []byte("GET / HTTP/1.1\r\nHost: "+testSteamHost+"\r\n\r\n"))
	}
	if !o.stopped || s.cdn.observers != 0 {
		t.Fatal("observation queue unbounded")
	}
	o = s.newSteamObserver(o.session)
	defer o.close()
	s.cdn.configure(false, false)
	o.feed(true, []byte("GET"))
	if !o.stopped {
		t.Fatal("disable did not stop observer")
	}
}
func TestSteamObservedSignedProbeBindsAndKeepsURI(t *testing.T) {
	s, _ := New(Config{DNS: dns.Config{Policy: dns.PolicyOff}, SteamCDNEnabled: true, Adapters: []Adapter{{Name: "a", SourceIP: "127.0.0.1"}}})
	s.cdn = newSteamCDN(context.Background(), true)
	defer s.cdn.cancel()
	host := "xz.sycontroller.com"
	uri := testSteamChunk + "?reqhost=ctgslb&auth_key=1788931770-1234-0-Fake"
	s.dialTCP = func(_ context.Context, d *net.Dialer, target string) (net.Conn, error) {
		if target != "5.6.7.8:80" || d.LocalAddr.String() != "127.0.0.1:0" {
			t.Error("lost binding", target, d.LocalAddr)
		}
		a, b := net.Pipe()
		go func() {
			defer b.Close()
			_ = b.SetDeadline(time.Now().Add(time.Second))
			r, e := http.ReadRequest(bufio.NewReader(b))
			if e != nil {
				return
			}
			defer r.Body.Close()
			if r.RequestURI != uri || r.Host != host || r.Header.Get("Range") != "bytes=0-3" {
				t.Error("signed request changed")
			}
			_, _ = io.WriteString(b, "HTTP/1.1 206 Partial Content\r\nContent-Range: bytes 0-3/4\r\nContent-Length: 4\r\n\r\ndata")
		}()
		return a, nil
	}
	s.validateObservedSteam(context.Background(), s.cdn.generation, s.config.Adapters[0], host, "1.2.3.4", uri, []byte("data"), 4, []cdnCandidate{{"5.6.7.8", time.Now().Add(time.Minute)}}, map[string]string{"5.6.7.8": "session"})
	entries := s.cdn.snapshot().Entries
	if len(entries) != 1 || !entries[0].Validated || entries[0].Adapter != "a" || entries[0].Source != "session" {
		t.Fatal(entries)
	}
	// A different prefix must not be admitted.
	s.cdn.configure(true, true)
	s.validateObservedSteam(context.Background(), s.cdn.generation, s.config.Adapters[0], host, "1.2.3.4", uri, []byte("evil"), 4, []cdnCandidate{{"5.6.7.8", time.Now().Add(time.Minute)}}, map[string]string{"5.6.7.8": "session"})
	if len(s.cdn.snapshot().Entries) != 0 || s.cdn.snapshot().StageCounts["http_content_mismatch"] != 1 {
		t.Fatal("mismatched content admitted")
	}
}
func TestSteamTrialRequiresValidationAndReservesOne(t *testing.T) {
	c := newSteamCDN(context.Background(), true)
	defer c.cancel()
	seedCDN(c, "a", "80", "5.6.7.8")
	k := cdnKey{"a", testSteamHost, "80", "5.6.7.8"}
	c.entries[k].Validated = false
	for range 16 {
		if ip, _ := c.useTrial("a", testSteamHost, "80", "1.2.3.4"); ip != "" {
			t.Fatal("original-only node selected")
		}
	}
	c.entries[k].Validated = true
	c.decisions["a/"+testSteamHost+":80"] = 0
	for i := 1; i <= 16; i++ {
		ip, _ := c.useTrial("a", testSteamHost, "80", "1.2.3.4")
		if (ip != "") != (i == 8) {
			t.Fatalf("trial %d = %q", i, ip)
		}
	}
	c.releaseTrial(k, c.generation)
	c.decisions["a/"+testSteamHost+":80"] = 7
	if ip, _ := c.useTrial("a", testSteamHost, "80", "1.2.3.4"); ip == "" {
		t.Fatal("reservation not released")
	}
}
func TestSteamTrafficTotalsExpire(t *testing.T) {
	c := newSteamCDN(context.Background(), true)
	defer c.cancel()
	seedCDN(c, "a", "80", "5.6.7.8")
	k := cdnKey{"a", testSteamHost, "80", "5.6.7.8"}
	now := time.Now()
	c.now = func() time.Time { return now }
	c.trafficChange(k, c.generation, 1, 5000, false)
	c.trafficChange(k, c.generation, 1, 10000, false)
	e := c.snapshot().Entries[0]
	if e.ActiveConnections != 2 || e.TotalBPS != 3000 {
		t.Fatal(e)
	}
	now = now.Add(6 * time.Second)
	if c.snapshot().Entries[0].TotalBPS != 0 {
		t.Fatal("stale speed")
	}
	c.configure(true, true)
	c.trafficChange(k, c.generation-1, 1, 10000, false)
	if len(c.traffic) != 0 {
		t.Fatal("old traffic repopulated reset")
	}
}
func TestSteamProbeBudgetStopsBeforeNetwork(t *testing.T) {
	s, _ := New(Config{DNS: dns.Config{Policy: dns.PolicyOff}, SteamCDNEnabled: true, Adapters: []Adapter{{Name: "a", SourceIP: "127.0.0.1"}}})
	s.cdn = newSteamCDN(context.Background(), true)
	defer s.cdn.cancel()
	s.cdn.budgetAt = time.Now()
	s.cdn.budgetBytes = 256 * 1024
	s.dialTCP = func(context.Context, *net.Dialer, string) (net.Conn, error) {
		t.Fatal("budget ignored")
		return nil, nil
	}
	_, e := s.probeSteamHTTP(context.Background(), s.config.Adapters[0], testSteamHost, "5.6.7.8", testSteamChunk)
	if steamProbeReason(e) != "http_budget" {
		t.Fatal(e)
	}
}

func TestSteamPreferredRequiresFreshRepeatedEvidence(t *testing.T) {
	c := newSteamCDN(context.Background(), true)
	defer c.cancel()
	seedCDN(c, "a", "80", "5.6.7.8")
	seedCDN(c, "a", "80", "1.2.3.4")
	k := cdnKey{"a", testSteamHost, "80", "5.6.7.8"}
	base := cdnKey{"a", testSteamHost, "80", "1.2.3.4"}
	now := time.Now()
	c.now = func() time.Time { return now }
	c.decisions["a/"+testSteamHost+":80"] = 0
	c.traffic[k] = &cdnTraffic{trials: 1}
	for range 5 {
		c.observe(k, c.generation, 4*1024*1024, time.Second)
		c.observe(base, c.generation, 1024*1024, time.Second)
	}
	if ip, _ := c.useTrial("a", testSteamHost, "80", "1.2.3.4"); ip != "" {
		t.Fatal("promoted after one window")
	}
	now = now.Add(5 * time.Second)
	c.observe(k, c.generation, 4*1024*1024, time.Second)
	c.observe(base, c.generation, 1024*1024, time.Second)
	if ip, _ := c.useTrial("a", testSteamHost, "80", "1.2.3.4"); ip != "5.6.7.8" || !c.entries[k].Preferred {
		t.Fatal("fresh advantage not promoted")
	}
	if c.entries[k].SuccessfulConnections != 0 {
		t.Fatal("test must promote while connections remain open")
	}
	for range 2 {
		if ip, _ := c.useTrial("a", testSteamHost, "80", "1.2.3.4"); ip == "" {
			t.Fatal("preferred candidate not admitted below limit")
		}
	}
	if ip, _ := c.useTrial("a", testSteamHost, "80", "1.2.3.4"); ip != "" {
		t.Fatal("preferred candidate exceeded four concurrent trials")
	}
	c.releaseTrial(k, c.generation)
	now = now.Add(11 * time.Second)
	if ip, _ := c.useTrial("a", testSteamHost, "80", "1.2.3.4"); ip != "" || c.entries[k].Preferred {
		t.Fatal("stale advantage retained")
	}
}

func TestSteamObserverProxyAndTUNMetadataThenChunk(t *testing.T) {
	for _, mode := range []string{"http", "tun"} {
		t.Run(mode, func(t *testing.T) {
			s, _ := New(Config{DNS: dns.Config{Policy: dns.PolicyOff}, SteamCDNEnabled: true, Adapters: []Adapter{{Name: "a", SourceIP: "127.0.0.1"}}})
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Host != testSteamHost {
					t.Error("host changed")
				}
				if r.URL.Path == "/auth" {
					_, _ = io.Copy(io.Discard, r.Body)
					_, _ = io.WriteString(w, "ok")
					return
				}
				if r.URL.Path != testSteamChunk {
					t.Error("path changed")
				}
				w.Header().Set("Content-Length", "4")
				_, _ = io.WriteString(w, "data")
			}))
			defer origin.Close()
			s.dialTCP = func(ctx context.Context, _ *net.Dialer, _ string) (net.Conn, error) {
				conn, e := (&net.Dialer{}).DialContext(ctx, "tcp", origin.Listener.Addr().String())
				if e != nil {
					return nil, e
				}
				return cdnRemoteConn{conn, "1.2.3.4:80"}, nil
			}
			endpoints, e := s.Start()
			if e != nil {
				t.Fatal(e)
			}
			defer stopServer(t, s)
			s.cdn.mu.Lock()
			s.cdn.probing = 2
			s.cdn.mu.Unlock()
			resolver, e := dns.New(s.ctx, s.config.DNS, func(_ context.Context, network, _ string, _ dns.Binding) (net.Conn, error) {
				a, b := net.Pipe()
				go answerDNSAOnce(b, network, net.ParseIP("1.2.3.4").To4())
				return a, nil
			})
			if e != nil {
				t.Fatal(e)
			}
			s.resolver = resolver
			endpoint := endpoints.HTTP
			if mode == "tun" {
				endpoint = endpoints.SOCKS
			}
			client, e := net.Dial("tcp", endpoint)
			if e != nil {
				t.Fatal(e)
			}
			defer client.Close()
			_ = client.SetDeadline(time.Now().Add(3 * time.Second))
			reader := bufio.NewReader(client)
			if mode == "tun" {
				_, _ = client.Write([]byte{5, 1, 0})
				_, _ = io.ReadFull(reader, make([]byte, 2))
				_, _ = client.Write([]byte{5, 1, 0, 1, 1, 2, 3, 4, 0, 80})
				reply := make([]byte, 10)
				_, e = io.ReadFull(reader, reply)
				if e != nil || reply[1] != 0 {
					t.Fatal(e, reply)
				}
			}
			target := "/auth"
			if mode == "http" {
				target = "http://" + testSteamHost + target
			}
			_, _ = fmt.Fprintf(client, "POST %s HTTP/1.1\r\nHost: %s\r\nContent-Length: 3\r\n\r\nabc", target, testSteamHost)
			r, e := http.ReadResponse(reader, nil)
			if e != nil {
				t.Fatal(e)
			}
			data, _ := io.ReadAll(r.Body)
			r.Body.Close()
			if string(data) != "ok" {
				t.Fatal("metadata changed")
			}
			_, _ = fmt.Fprintf(client, "GET %s HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", testSteamChunk, testSteamHost)
			r, e = http.ReadResponse(reader, nil)
			if e != nil {
				t.Fatal(e)
			}
			data, _ = io.ReadAll(r.Body)
			r.Body.Close()
			if string(data) != "data" {
				t.Fatal("chunk changed")
			}
			// Downstream Write can complete just after the client receives the bytes.
			deadline := time.Now().Add(time.Second)
			for s.cdn.snapshot().StageCounts["http_reference_ready"] == 0 && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			status := s.cdn.snapshot()
			if status.StageCounts["http_method"] != 1 || status.StageCounts["http_eligible"] != 1 || status.StageCounts["http_reference_ready"] != 1 {
				t.Fatal(status.StageCounts)
			}
		})
	}
}

func TestSteamTrialHTTPFailureCoolsCandidate(t *testing.T) {
	s, o := observerFixture(t)
	seedCDN(s.cdn, "a", "80", "1.2.3.4")
	o.session.cdnTrial = true
	o.feed(true, []byte("GET "+testSteamChunk+" HTTP/1.1\r\nHost: "+testSteamHost+"\r\n\r\n"))
	o.feed(false, []byte("HTTP/1.1 403 Forbidden\r\nContent-Length: 0\r\n\r\n"))
	entry := s.cdn.snapshot().Entries[0]
	if !o.session.cdnResponseFailed || !entry.CooldownUntil.After(time.Now()) {
		t.Fatal("HTTP rejection did not cool trial")
	}
	if s.cdn.snapshot().StageCounts["http_signature_rejected"] != 1 {
		t.Fatal("missing rejection reason")
	}
}

func TestSteamAdditionalSignedCDNProfiles(t *testing.T) {
	expiry := strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)
	for _, tc := range []struct {
		host, query string
		allowed     bool
	}{
		{"dl1.steam.clngaa.com", "k=Fake%2BKey&t=" + expiry, true},
		{"dl.steam.clngaa.com", "t=" + expiry + "&k=Fake&rdkey=Fake%3D", true},
		{"gstore-y.bal.manlaxy.com", "token=Fake&expiration_time=" + expiry, true},
		{"xz.pphimalayanrt.com", "reqaidabccty=Fake&auth_key=1788931770-123-0-Fake&reqhost=ctgslb", true},
		{"dl1.steam.clngaa.com", "k=Fake&t=1", false},
		{"dl1.steam.clngaa.com", "k=Fake", false},
		{"dl1.steam.clngaa.com", "k=Fake&t=" + expiry + "&extra=Fake", false},
		{"dl1.steam.clngaa.com", "k=Fake&t=" + expiry + "&k=duplicate", false},
		{"gstore-y.bal.manlaxy.com", "token=Fake", false},
		{"dl1.steam.clngaa.com.evil.test", "k=Fake&t=" + expiry, false},
	} {
		uri := testSteamChunk + "?" + tc.query
		r, e := http.ReadRequest(bufio.NewReader(strings.NewReader("GET " + uri + " HTTP/1.1\r\nHost: " + tc.host + "\r\n\r\n")))
		if e != nil {
			t.Fatal(e)
		}
		got, _ := steamRequestURI(r)
		if (got != "") != tc.allowed {
			t.Fatalf("profile %s allowed=%v", tc.host, tc.allowed)
		}
		if tc.allowed && (!steamDownloadHost(tc.host) || got != uri || !validSteamProbeURI(tc.host, uri)) {
			t.Fatal("host or signed bytes changed")
		}
	}
}
func TestSteamIPRedirectIsExplicitObservationOnly(t *testing.T) {
	s, o := observerFixture(t)
	o.feed(true, []byte("GET "+testSteamChunk+" HTTP/1.1\r\nHost: "+testSteamHost+"\r\n\r\n"))
	o.feed(false, []byte("HTTP/1.1 302 Found\r\nContent-Length: 0\r\nLocation: http://58.19.174.176"+testSteamChunk+"?auth_key=FakeSecret\r\n\r\n"))
	status := s.cdn.snapshot()
	if status.StageCounts["http_redirect_ip"] != 1 || status.StageCounts["http_reference_ready"] != 0 {
		t.Fatal(status.StageCounts)
	}
	raw, _ := json.Marshal(status)
	if bytes.Contains(raw, []byte("FakeSecret")) {
		t.Fatal("redirect signature leaked")
	}
	if steamDownloadHost("58.19.174.176") {
		t.Fatal("literal IP accepted as CDN domain")
	}
}

func TestSteamActiveExpiryRetainsStatisticsWithoutAdmission(t *testing.T) {
	c := newSteamCDN(context.Background(), true)
	defer c.cancel()
	seedCDN(c, "a", "80", "5.6.7.8")
	key := cdnKey{"a", testSteamHost, "80", "5.6.7.8"}
	c.traffic[key] = &cdnTraffic{active: 1, trials: 1}
	c.entries[key].Samples = 9
	c.entries[key].EffectiveBytes = 20 * 1024 * 1024
	c.entries[key].Preferred = true
	c.entries[key].ExpiresAt = time.Now().Add(-time.Second)
	status := c.snapshot()
	if len(status.Entries) != 1 || status.Entries[0].Samples != 9 || status.Entries[0].Validated || status.Entries[0].Preferred || status.Entries[0].DecisionReason != "validation_expired" {
		t.Fatal("active expiry lost data or retained eligibility", status.Entries)
	}
	if ip, _ := c.useTrial("a", testSteamHost, "80", "1.2.3.4"); ip != "" {
		t.Fatal("expired candidate selected", ip)
	}
	if ip, _ := c.choose("a", testSteamHost, "80"); ip != "" {
		t.Fatal("legacy selection reused expired candidate", ip)
	}
	c.traffic[key].active = 0
	c.traffic[key].trials = 0
	if len(c.snapshot().Entries) != 0 {
		t.Fatal("idle expired record retained")
	}
}

func TestSteamPromotionDiagnosticReasons(t *testing.T) {
	now := time.Now()
	base := SteamCDNEntry{Samples: 5, DownloadBPS: 100, lastSample: now}
	good := SteamCDNEntry{Samples: 5, EffectiveBytes: 8 * 1024 * 1024, DownloadBPS: 116, lastSample: now}
	for _, test := range []struct {
		name     string
		modify   func(*SteamCDNEntry)
		baseline *SteamCDNEntry
		reason   string
	}{
		{"fresh", func(*SteamCDNEntry) {}, &base, "advantage_window"},
		{"small", func(e *SteamCDNEntry) { e.EffectiveBytes-- }, &base, "insufficient_samples"},
		{"stale", func(e *SteamCDNEntry) { e.lastSample = now.Add(-11 * time.Second) }, &base, "stale_samples"},
		{"unchanged", func(e *SteamCDNEntry) { e.scoredSamples = e.Samples }, &base, "stale_samples"},
		{"no baseline", func(*SteamCDNEntry) {}, nil, "baseline_missing"},
		{"slow", func(e *SteamCDNEntry) { e.DownloadBPS = 110 }, &base, "advantage_insufficient"},
	} {
		t.Run(test.name, func(t *testing.T) {
			e := good
			test.modify(&e)
			if got := steamPromotionReason(now, &e, test.baseline); got != test.reason {
				t.Fatalf("%s != %s", got, test.reason)
			}
		})
	}
}
