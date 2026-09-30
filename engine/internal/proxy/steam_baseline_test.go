package proxy

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestSteamNewOriginalConnectionRecoversExpiredBaseline(t *testing.T) {
	for _, validated := range []bool{false, true} {
		name := "original"
		if validated {
			name = "expired candidate"
		}
		t.Run(name, func(t *testing.T) {
			s, err := New(Config{SteamCDNEnabled: true, Adapters: []Adapter{{Name: "a", SourceIP: "127.0.0.1"}}})
			if err != nil {
				t.Fatal(err)
			}
			s.ctx = context.Background()
			s.cdn = newSteamCDN(s.ctx, true)
			defer s.cdn.cancel()
			now := time.Now()
			s.cdn.mu.Lock()
			s.cdn.now = func() time.Time { return now }
			k := cdnKey{"a", testSteamHost, "80", "1.2.3.4"}
			cooldown := now.Add(time.Minute)
			s.cdn.entries[k] = &SteamCDNEntry{
				Adapter: k.adapter, Domain: k.domain, Port: k.port, IP: k.ip,
				ExpiresAt: now.Add(-time.Second), Validated: validated, Preferred: validated,
				Samples: 9, EffectiveBytes: 20 << 20, DownloadBPS: 1 << 20, lastSample: now,
				ProbeBPS: 2 << 20, ProbedAt: now, probeAttemptAt: now,
				CooldownUntil: cooldown, SuccessfulConnections: 3, Selections: 4,
			}
			s.cdn.traffic[k] = &cdnTraffic{active: 1, originalBytes: 1234, switchedBytes: 5678}
			s.cdn.mu.Unlock()
			client, peer := net.Pipe()
			defer client.Close()
			defer peer.Close()
			upstream, remote := net.Pipe()
			defer upstream.Close()
			defer remote.Close()
			original := cdnRemoteConn{upstream, k.ip + ":80"}
			session := s.registry.Begin("socks5", ChannelAggregation, client)
			defer s.registry.Finish(session)
			s.registry.Attach(session, original, k.ip+":80", s.config.Adapters[0])
			if got := s.prepareSteamCDN(session, original, s.config.Adapters[0], k.domain, k.port, ""); got != original {
				t.Fatal("unexpected replacement")
			}
			entry := s.cdn.entries[k]
			if entry.Samples != 0 || entry.EffectiveBytes != 0 || entry.DownloadBPS != 0 || entry.Preferred || entry.Validated || entry.ProbeBPS != 0 || !entry.ProbedAt.IsZero() || !entry.probeAttemptAt.IsZero() {
				t.Fatal("expired performance or replacement eligibility retained")
			}
			if !entry.ExpiresAt.Equal(now.Add(cdnLifetime)) || !entry.CooldownUntil.Equal(cooldown) || entry.SuccessfulConnections != 3 || entry.Selections != 4 {
				t.Fatal("baseline renewal lost lifetime, cooldown or history")
			}
			s.cdn.observe(k, session.cdnGeneration, 256*1024, time.Second)
			status := s.cdn.snapshot()
			if len(status.Entries) != 1 || status.Entries[0].Samples != 1 || status.Entries[0].DownloadBPS != 256*1024 || status.Entries[0].OriginalBytes != 1234 || status.Entries[0].SwitchedBytes != 5678 {
				t.Fatalf("renewed baseline cannot learn or lost attribution: %+v", status.Entries)
			}
			// A fresh original baseline must not revive expired validation when
			// the same IP is considered as a replacement for another original.
			s.cdn.mu.Lock()
			now = now.Add(2 * time.Minute) // The preserved cooldown has ended.
			s.cdn.decisions[steamFailureKey(k)] = 7
			s.cdn.mu.Unlock()
			if ip, _ := s.cdn.useTrial(k.adapter, k.domain, k.port, "5.6.7.8"); ip != "" {
				t.Fatal("baseline renewal admitted an unvalidated candidate", ip)
			}
			if ip, _ := s.cdn.choose(k.adapter, k.domain, k.port); ip != "" {
				t.Fatal("legacy selection admitted an unvalidated candidate", ip)
			}
		})
	}
}

func TestSteamOriginalConnectionDoesNotExtendFreshCandidateValidation(t *testing.T) {
	s, err := New(Config{SteamCDNEnabled: true, Adapters: []Adapter{{Name: "a", SourceIP: "127.0.0.1"}}})
	if err != nil {
		t.Fatal(err)
	}
	s.ctx = context.Background()
	s.cdn = newSteamCDN(s.ctx, true)
	defer s.cdn.cancel()
	now := time.Now()
	s.cdn.mu.Lock()
	s.cdn.now = func() time.Time { return now }
	k := cdnKey{"a", testSteamHost, "80", "1.2.3.4"}
	expires := now.Add(30 * time.Second)
	s.cdn.entries[k] = &SteamCDNEntry{
		Adapter: k.adapter, Domain: k.domain, Port: k.port, IP: k.ip,
		ExpiresAt: expires, Validated: true, Samples: 9, DownloadBPS: 1 << 20,
		ProbeBPS: 2 << 20, ProbedAt: now, lastSample: now,
	}
	s.cdn.mu.Unlock()
	client, peer := net.Pipe()
	defer client.Close()
	defer peer.Close()
	upstream, remote := net.Pipe()
	defer upstream.Close()
	defer remote.Close()
	original := cdnRemoteConn{upstream, k.ip + ":80"}
	session := s.registry.Begin("socks5", ChannelAggregation, client)
	defer s.registry.Finish(session)
	s.registry.Attach(session, original, k.ip+":80", s.config.Adapters[0])
	s.prepareSteamCDN(session, original, s.config.Adapters[0], k.domain, k.port, "")
	entry := s.cdn.entries[k]
	if !entry.Validated || !entry.ExpiresAt.Equal(expires) || entry.Samples != 9 || entry.DownloadBPS != 1<<20 || entry.ProbeBPS != 2<<20 || !entry.ProbedAt.Equal(now) {
		t.Fatal("new original changed fresh validation or performance evidence")
	}
}
