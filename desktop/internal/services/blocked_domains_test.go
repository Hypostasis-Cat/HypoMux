package services

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestBlockedDomainServiceReadsLegacyListAndPersistsRemovals(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("HYPOMUX_DATA_DIR", dataDir)
	settings := NewSettingsService()
	current := settings.Get()
	current.BlockedDomainBypass = true
	current.BlockedDomainExpiry = true
	if _, err := settings.Update(current); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(dataDir, "blocked_domains.json"),
		[]byte(`{"WLAN":["Example.COM.","cdn.example.com"]}`),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	service := NewBlockedDomainService(settings)
	service.now = func() time.Time { return time.Now() }
	snapshot, err := service.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Entries) != 2 || snapshot.Entries[0].Adapter != "WLAN" {
		t.Fatalf("legacy entries not loaded: %#v", snapshot)
	}
	if err := service.Remove("WLAN", "EXAMPLE.COM."); err != nil {
		t.Fatal(err)
	}
	reloaded := NewBlockedDomainService(settings)
	after, err := reloaded.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Entries) != 1 || after.Entries[0].Domain != "cdn.example.com" {
		t.Fatalf("removal did not persist: %#v", after)
	}
}

func newBlockedDomainTestService(t *testing.T, useExpiry bool) *BlockedDomainService {
	t.Helper()
	t.Setenv("HYPOMUX_DATA_DIR", t.TempDir())
	settings := NewSettingsService()
	if _, err := settings.UpdateFields(AppSettings{
		BlockedDomainBypass: true, BlockedDomainExpiry: useExpiry,
	}, []string{"blocked_domain_bypass", "blocked_domain_expiry"}); err != nil {
		t.Fatal(err)
	}
	// Seed the test's own file so load never falls back to real legacy data.
	path := filepath.Join(settingsDirectory(), "blocked_domains.json")
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := NewBlockedDomainService(settings)
	service.now = func() time.Time { return time.Unix(1700000000, 0) }
	return service
}

func reloadBlockedDomainTestService(t *testing.T, service *BlockedDomainService) *BlockedDomainService {
	t.Helper()
	// Inject the same clock before load, which may purge expired entries.
	reloaded := &BlockedDomainService{
		path: service.path, settings: NewSettingsService(), now: service.now,
		entries: map[string]map[string]float64{},
	}
	if err := reloaded.load(); err != nil {
		t.Fatal(err)
	}
	return reloaded
}

func setBlockedDomainExpiry(t *testing.T, settings *SettingsService, useExpiry bool) {
	t.Helper()
	if _, err := settings.UpdateFields(AppSettings{BlockedDomainExpiry: useExpiry}, []string{"blocked_domain_expiry"}); err != nil {
		t.Fatal(err)
	}
}

func syncBlockedDomains(t *testing.T, service *BlockedDomainService, entries ...BlockedDomainEntry) {
	t.Helper()
	if err := service.ReplaceRuntime(entries); err != nil {
		t.Fatal(err)
	}
}

func requireBlockedDomainRecords(t *testing.T, service *BlockedDomainService, want map[string]time.Time) {
	t.Helper()
	snapshot, err := service.List()
	if err != nil {
		t.Fatal(err)
	}
	useExpiry := service.settings.Get().BlockedDomainExpiry
	if !snapshot.Enabled || snapshot.UseExpiry != useExpiry || len(snapshot.Entries) != len(want) {
		t.Fatalf("snapshot = %#v, want use_expiry=%t, records=%v", snapshot, useExpiry, want)
	}
	for _, entry := range snapshot.Entries {
		expiry, ok := want[entry.Adapter+"/"+entry.Domain]
		if !ok || !entry.ExpiresAt.Equal(expiry) || entry.Permanent == useExpiry {
			t.Fatalf("unexpected record %#v, want use_expiry=%t, records=%v", entry, useExpiry, want)
		}
	}
}

// Regression for issue #106: a running engine can expire entries under its
// old start-time setting even after the user disables automatic expiry.
func TestBlockedDomainReplaceRuntimePreservesRecordsWhenExpiryDisabled(t *testing.T) {
	service := newBlockedDomainTestService(t, true)
	now := service.now()
	expiresAt := now.Add(30 * time.Minute)
	syncBlockedDomains(t, service,
		BlockedDomainEntry{Adapter: "WLAN", Domain: "example.com", ExpiresAt: expiresAt},
		BlockedDomainEntry{Adapter: "以太网", Domain: "cdn.example.com", ExpiresAt: expiresAt},
	)
	setBlockedDomainExpiry(t, service.settings, false)
	service.now = func() time.Time { return now.Add(time.Hour) }
	want := map[string]time.Time{"WLAN/example.com": expiresAt, "以太网/cdn.example.com": expiresAt}
	for _, entries := range [][]BlockedDomainEntry{nil, {}} {
		syncBlockedDomains(t, service, entries...)
		requireBlockedDomainRecords(t, service, want)
		requireBlockedDomainRecords(t, reloadBlockedDomainTestService(t, service), want)
	}
}

func TestBlockedDomainReplaceRuntimeMergesWhenExpiryDisabled(t *testing.T) {
	service := newBlockedDomainTestService(t, false)
	expiresAt := service.now().Add(30 * time.Minute)
	renewed := service.now().AddDate(100, 0, 0)
	syncBlockedDomains(t, service,
		BlockedDomainEntry{Adapter: "WLAN", Domain: "a.example.com", ExpiresAt: expiresAt},
		BlockedDomainEntry{Adapter: "以太网", Domain: "offline.example.com", ExpiresAt: expiresAt},
	)
	// The second run omits an adapter, updates one record and adds another.
	syncBlockedDomains(t, service,
		BlockedDomainEntry{Adapter: "WLAN", Domain: "a.example.com", ExpiresAt: renewed},
		BlockedDomainEntry{Adapter: "WLAN", Domain: "b.example.com", ExpiresAt: renewed},
	)
	want := map[string]time.Time{
		"WLAN/a.example.com": renewed, "WLAN/b.example.com": renewed,
		"以太网/offline.example.com": expiresAt,
	}
	requireBlockedDomainRecords(t, service, want)
	requireBlockedDomainRecords(t, reloadBlockedDomainTestService(t, service), want)
}

func TestBlockedDomainReplaceRuntimeValidatesEntriesAndHonorsExpiry(t *testing.T) {
	for _, useExpiry := range []bool{false, true} {
		t.Run(fmt.Sprintf("expiry=%t", useExpiry), func(t *testing.T) {
			service := newBlockedDomainTestService(t, useExpiry)
			now := service.now()
			future := now.Add(30 * time.Minute)
			syncBlockedDomains(t, service,
				BlockedDomainEntry{Adapter: " WLAN ", Domain: " EXAMPLE.COM. ", ExpiresAt: future},
				BlockedDomainEntry{Adapter: "WLAN", Domain: "expired.example.com", ExpiresAt: now.Add(-time.Second)},
				BlockedDomainEntry{Adapter: "WLAN", Domain: "boundary.example.com", ExpiresAt: now},
				BlockedDomainEntry{Adapter: " ", Domain: "missing-adapter.example.com", ExpiresAt: future},
				BlockedDomainEntry{Adapter: "WLAN", Domain: " . ", ExpiresAt: future},
				BlockedDomainEntry{Adapter: "WLAN", Domain: "missing-expiry.example.com"},
			)
			want := map[string]time.Time{"WLAN/example.com": future}
			if !useExpiry {
				// A confirmed record first arriving after its old deadline is
				// still permanent under the current desktop preference.
				want["WLAN/expired.example.com"] = now.Add(-time.Second)
				want["WLAN/boundary.example.com"] = now
			}
			requireBlockedDomainRecords(t, service, want)
			requireBlockedDomainRecords(t, reloadBlockedDomainTestService(t, service), want)
		})
	}
}

func TestBlockedDomainReplaceRuntimeMirrorsWhenExpiryEnabled(t *testing.T) {
	service := newBlockedDomainTestService(t, true)
	expiresAt := service.now().Add(30 * time.Minute)
	syncBlockedDomains(t, service,
		BlockedDomainEntry{Adapter: "WLAN", Domain: "a.example.com", ExpiresAt: expiresAt},
		BlockedDomainEntry{Adapter: "WLAN", Domain: "b.example.com", ExpiresAt: expiresAt},
	)
	syncBlockedDomains(t, service, BlockedDomainEntry{Adapter: "WLAN", Domain: "b.example.com", ExpiresAt: expiresAt})
	want := map[string]time.Time{"WLAN/b.example.com": expiresAt}
	requireBlockedDomainRecords(t, service, want)
	requireBlockedDomainRecords(t, reloadBlockedDomainTestService(t, service), want)
	syncBlockedDomains(t, service)
	requireBlockedDomainRecords(t, service, nil)
	requireBlockedDomainRecords(t, reloadBlockedDomainTestService(t, service), nil)
}

func TestBlockedDomainReenablingExpiryRestoresPurgeAndMirror(t *testing.T) {
	service := newBlockedDomainTestService(t, false)
	now := service.now()
	future := now.Add(30 * time.Minute)
	syncBlockedDomains(t, service,
		BlockedDomainEntry{Adapter: "WLAN", Domain: "expired.example.com", ExpiresAt: now.Add(-time.Minute)},
		BlockedDomainEntry{Adapter: "WLAN", Domain: "future.example.com", ExpiresAt: future},
	)
	requireBlockedDomainRecords(t, service, map[string]time.Time{
		"WLAN/expired.example.com": now.Add(-time.Minute), "WLAN/future.example.com": future,
	})
	setBlockedDomainExpiry(t, service.settings, true)
	requireBlockedDomainRecords(t, service, map[string]time.Time{"WLAN/future.example.com": future})
	syncBlockedDomains(t, service)
	requireBlockedDomainRecords(t, reloadBlockedDomainTestService(t, service), nil)
}

func TestBlockedDomainManualRemovalPersistsWhenExpiryDisabled(t *testing.T) {
	service := newBlockedDomainTestService(t, false)
	expiresAt := service.now().AddDate(100, 0, 0)
	syncBlockedDomains(t, service,
		BlockedDomainEntry{Adapter: "WLAN", Domain: "a.example.com", ExpiresAt: expiresAt},
		BlockedDomainEntry{Adapter: "WLAN", Domain: "b.example.com", ExpiresAt: expiresAt},
	)
	if err := service.Remove(" WLAN ", "A.EXAMPLE.COM."); err != nil {
		t.Fatal(err)
	}
	// Verify local removal and reload, not deletion from a running engine:
	// old runtime samples can still report removed entries (a separate issue).
	syncBlockedDomains(t, service)
	want := map[string]time.Time{"WLAN/b.example.com": expiresAt}
	requireBlockedDomainRecords(t, service, want)
	requireBlockedDomainRecords(t, reloadBlockedDomainTestService(t, service), want)
	if err := service.Clear(); err != nil {
		t.Fatal(err)
	}
	syncBlockedDomains(t, service)
	requireBlockedDomainRecords(t, service, nil)
	requireBlockedDomainRecords(t, reloadBlockedDomainTestService(t, service), nil)
}

func TestBlockedDomainReplaceRuntimeRetriesFailedPersistence(t *testing.T) {
	for _, useExpiry := range []bool{false, true} {
		t.Run(fmt.Sprintf("expiry=%t", useExpiry), func(t *testing.T) {
			service := newBlockedDomainTestService(t, useExpiry)
			expiresAt := service.now().Add(30 * time.Minute)
			renewed := expiresAt.Add(time.Minute)
			syncBlockedDomains(t, service,
				BlockedDomainEntry{Adapter: "WLAN", Domain: "a.example.com", ExpiresAt: expiresAt},
				BlockedDomainEntry{Adapter: "以太网", Domain: "b.example.com", ExpiresAt: expiresAt},
			)
			before, err := os.ReadFile(service.path)
			if err != nil {
				t.Fatal(err)
			}
			// A directory at the temporary-file path forces a deterministic
			// write failure without changing permissions or the real record file.
			if err := os.Mkdir(service.path+".tmp", 0o700); err != nil {
				t.Fatal(err)
			}
			incoming := []BlockedDomainEntry{
				{Adapter: "WLAN", Domain: "a.example.com", ExpiresAt: renewed},
				{Adapter: "WLAN", Domain: "c.example.com", ExpiresAt: renewed},
			}
			if err := service.ReplaceRuntime(incoming); err == nil {
				t.Fatal("expected persistence failure")
			}
			requireBlockedDomainRecords(t, service, map[string]time.Time{
				"WLAN/a.example.com": expiresAt, "以太网/b.example.com": expiresAt,
			})
			after, err := os.ReadFile(service.path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("failed write changed persisted data: %q, error: %v", after, err)
			}
			if err := os.Remove(service.path + ".tmp"); err != nil {
				t.Fatal(err)
			}
			// The exact same sample must retry, not be considered unchanged.
			syncBlockedDomains(t, service, incoming...)
			want := map[string]time.Time{"WLAN/a.example.com": renewed, "WLAN/c.example.com": renewed}
			if !useExpiry {
				want["以太网/b.example.com"] = expiresAt
			}
			requireBlockedDomainRecords(t, service, want)
			requireBlockedDomainRecords(t, reloadBlockedDomainTestService(t, service), want)
			// Once persisted, unchanged telemetry must avoid rewriting the file.
			if err := os.Mkdir(service.path+".tmp", 0o700); err != nil {
				t.Fatal(err)
			}
			syncBlockedDomains(t, service, incoming...)
			if !useExpiry {
				syncBlockedDomains(t, service)
			}
		})
	}
}

func TestBlockedDomainConcurrentPermanentSyncAndList(t *testing.T) {
	service := newBlockedDomainTestService(t, false)
	expiresAt := service.now().Add(30 * time.Minute)
	const count = 12
	var workers sync.WaitGroup
	start := make(chan struct{})
	want := make(map[string]time.Time, count)
	for i := range count {
		domain := fmt.Sprintf("%d.example.com", i)
		want["WLAN/"+domain] = expiresAt
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			if err := service.ReplaceRuntime([]BlockedDomainEntry{{Adapter: "WLAN", Domain: domain, ExpiresAt: expiresAt}}); err != nil {
				t.Errorf("sync: %v", err)
			}
			if _, err := service.List(); err != nil {
				t.Errorf("list: %v", err)
			}
		}()
	}
	close(start)
	workers.Wait()
	requireBlockedDomainRecords(t, service, want)
	requireBlockedDomainRecords(t, reloadBlockedDomainTestService(t, service), want)
}
