package proxy

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

func TestSteamExpandedPoolFiltersBeforeLimiting(t *testing.T) {
	now := time.Now()
	var v4, v6 []cdnCandidate
	for i := 1; i <= 24; i++ {
		v4 = append(v4, cdnCandidate{fmt.Sprintf("1.1.1.%d", i), now.Add(time.Minute)})
		v6 = append(v6, cdnCandidate{fmt.Sprintf("2606:4700::%x", i), now.Add(time.Minute)})
	}
	pool := mergeSteamCandidatePool([][]cdnCandidate{v6, v4}, 32)
	if len(pool) != 32 {
		t.Fatalf("pool size: %d", len(pool))
	}
	pool = append([]cdnCandidate{{"10.0.0.1", now.Add(time.Minute)}, {"8.8.8.8", now}}, pool...)
	got := steamCandidatesForAdapter(pool, Adapter{SourceIP: "192.0.2.1"}, "1.1.1.1", now)
	if len(got) != 8 || got[0].ip != "1.1.1.2" || got[7].ip != "1.1.1.9" {
		t.Fatalf("unusable hints consumed validation slots: %+v", got)
	}
	got6 := steamCandidatesForAdapter(pool, Adapter{SourceIPv6: "2001:db8::1"}, "", now)
	if len(got6) != 8 || got6[0].ip != "2606:4700::1" {
		t.Fatalf("IPv6 pool: %+v", got6)
	}
}

type steamFailingWriter struct{ short bool }

func (w steamFailingWriter) Write(p []byte) (int, error) {
	if w.short {
		return len(p) - 1, nil
	}
	return 0, errors.New("client closed")
}

func TestSteamClientWriteFailureDoesNotBlameCandidate(t *testing.T) {
	for _, short := range []bool{false, true} {
		var blocked time.Duration
		var failed bool
		writer := steamTimedWriter{Writer: steamFailingWriter{short}, blocked: &blocked, failed: &failed}
		_, err := io.Copy(writer, strings.NewReader("download bytes"))
		if err == nil || !failed {
			t.Fatal("client failure not detected")
		}
		if steamUpstreamFailed(err, failed, false) {
			t.Fatal("client failure blamed on CDN")
		}
		if !steamUpstreamFailed(err, failed, true) {
			t.Fatal("invalid upstream response was hidden")
		}
	}
	if !steamUpstreamFailed(errors.New("upstream reset"), false, false) {
		t.Fatal("upstream failure ignored")
	}
	if steamUpstreamFailed(nil, false, false) {
		t.Fatal("normal EOF counted as failure")
	}
}
